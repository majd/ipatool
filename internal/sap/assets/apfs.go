//nolint:wsl // Binary tree traversal is clearer when related bounds checks remain adjacent.
package assets

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/blacktop/go-apfs"
	"github.com/blacktop/go-apfs/pkg/disk"
	"github.com/blacktop/go-apfs/pkg/disk/dmg"
	"github.com/blacktop/go-apfs/types"
	"github.com/deploymenttheory/go-macos-pkg/pkg/lzbitmap"
	"github.com/go-compressions/lzfse"
)

const (
	apfsObjectHeaderSize = 32
	apfsObjectTypeBTree  = 2
	apfsObjectTypeNode   = 3
	apfsObjectTypeFSTree = 14
	apfsObjectNoHeader   = 0x20000000
	apfsNodeRoot         = 0x0001
	apfsNodeNoHeader     = 0x0010
	apfsBlockSize        = 4096
	apfsMaximumTreeDepth = 32
	decmpfsLZVN          = 8
	decmpfsLZFSE         = 12
	decmpfsLZBitmap      = 14
	decmpfsBlockSize     = 64 << 10
)

type apfsExtractor struct {
	device        disk.Device
	filesystem    *apfs.APFS
	volumeOMap    types.BTreeNodePhys
	rootTreeBase  uint64
	transactionID types.XidT
}

type apfsExtent struct {
	logical  uint64
	physical uint64
	length   uint64
}

type apfsFile struct {
	io.ReaderAt
	size int64
}

func extractAPFSFiles(ctx context.Context, source io.ReaderAt, size int64, paths []string, destination string) error {
	extractor, closeExtractor, err := newAPFSExtractor(source, size)
	if err != nil {
		return err
	}
	defer func() { _ = closeExtractor() }()

	if err := os.MkdirAll(destination, 0o700); err != nil {
		return fmt.Errorf("create Apple filesystem extraction directory: %w", err)
	}

	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("extract Apple filesystem files: %w", err)
		}
		if err := extractor.extractFile(ctx, path, destination); err != nil {
			return fmt.Errorf("extract %s from Apple filesystem: %w", path, err)
		}
	}

	return nil
}

func newAPFSExtractor(source io.ReaderAt, size int64) (*apfsExtractor, func() error, error) {
	device, closeDevice, err := openAPFSDevice(source, size)
	if err != nil {
		return nil, nil, err
	}

	filesystem, err := apfs.NewAPFS(device)
	if err != nil {
		_ = closeDevice()

		return nil, nil, fmt.Errorf("open Apple filesystem: %w", err)
	}
	fail := func(err error) (*apfsExtractor, func() error, error) {
		filesystem.Close()
		_ = closeDevice()

		return nil, nil, err
	}

	volumeOMap, ok := filesystem.Volume.OMap.Body.(types.OMap)
	if !ok {
		return fail(fmt.Errorf("apple filesystem volume has an invalid object map"))
	}
	if volumeOMap.Tree == nil {
		return fail(fmt.Errorf("apple filesystem volume has no object-map tree"))
	}
	volumeOMapRoot, ok := volumeOMap.Tree.Body.(types.BTreeNodePhys)
	if !ok {
		return fail(fmt.Errorf("apple filesystem volume has an invalid object-map tree"))
	}

	extractor := &apfsExtractor{
		device:        device,
		filesystem:    filesystem,
		volumeOMap:    volumeOMapRoot,
		rootTreeBase:  uint64(filesystem.Volume.RootTreeOid),
		transactionID: filesystem.Volume.OMap.Hdr.Xid,
	}
	closeExtractor := func() error {
		filesystem.Close()

		return closeDevice()
	}

	return extractor, closeExtractor, nil
}

func (e *apfsExtractor) extractFile(ctx context.Context, path, destination string) (err error) {
	file, err := e.openFile(ctx, path)
	if err != nil {
		return err
	}

	outputPath := filepath.Join(destination, filepath.Base(path))
	output, err := os.OpenFile(outputPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create output: %w", err)
	}
	defer func() {
		if closeErr := output.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close output: %w", closeErr)
		}

		if err != nil {
			_ = os.Remove(outputPath)
		}
	}()

	reader := &contextReader{ctx: ctx, reader: io.NewSectionReader(file, 0, file.size)}
	if _, err := io.CopyN(output, reader, file.size); err != nil {
		return fmt.Errorf("copy file data: %w", err)
	}

	if err := output.Sync(); err != nil {
		return fmt.Errorf("sync output: %w", err)
	}

	return nil
}

func (e *apfsExtractor) openFile(ctx context.Context, path string) (*apfsFile, error) {
	records, err := e.resolvePath(path)
	if err != nil {
		return nil, err
	}

	var inode *types.JInodeVal
	var compression *types.DecmpfsDiskHeader
	var resource *types.JXattrDstreamT
	var extents []apfsExtent
	for _, record := range records {
		switch record.Hdr.GetType() {
		case types.APFS_TYPE_INODE:
			value, ok := record.Val.(types.JInodeVal)
			if ok {
				inode = &value
			}
		case types.APFS_TYPE_FILE_EXTENT:
			key, keyOK := record.Key.(types.JFileExtentKeyT)
			value, valueOK := record.Val.(types.JFileExtentValT)
			if keyOK && valueOK {
				extents = append(extents, apfsExtent{
					logical:  key.LogicalAddr,
					physical: value.PhysBlockNum,
					length:   value.Length(),
				})
			}
		case types.APFS_TYPE_XATTR:
			key, keyOK := record.Key.(types.JXattrKeyT)
			value, valueOK := record.Val.(types.JXattrValT)
			if !keyOK || !valueOK {
				continue
			}

			switch key.Name {
			case types.DECMPFS_XATTR_NAME:
				compression, err = types.GetDecmpfsHeader(record)
				if err != nil {
					return nil, fmt.Errorf("read compression metadata: %w", err)
				}
			case "com.apple.ResourceFork":
				stream, ok := value.Data.(types.JXattrDstreamT)
				if ok {
					resource = &stream
				}
			}
		}
	}

	if inode == nil {
		return nil, fmt.Errorf("file has no inode record")
	}

	if compression != nil {
		method := uint32(compression.CompressionType)
		if method != decmpfsLZVN && method != decmpfsLZFSE && method != decmpfsLZBitmap {
			return nil, fmt.Errorf("unsupported compression type %d", compression.CompressionType)
		}
		if resource == nil {
			return nil, fmt.Errorf("compressed file has no resource fork")
		}

		extents, err = e.dataExtents(resource.XattrObjID)
		if err != nil {
			return nil, fmt.Errorf("read resource-fork extents: %w", err)
		}
		resourceReader, err := newAPFSExtentReader(e.device, extents, resource.DStream.Size)
		if err != nil {
			return nil, err
		}
		reader, err := newDecmpfsReader(ctx, resourceReader, resource.DStream.Size, compression.UncompressedSize, method)
		if err != nil {
			return nil, err
		}

		return &apfsFile{ReaderAt: reader, size: int64(compression.UncompressedSize)}, nil
	}

	size := inode.UncompressedSize
	for _, field := range inode.Xfields {
		if stream, ok := field.Field.(types.JDstreamT); ok {
			size = stream.Size

			break
		}
	}
	if len(extents) == 0 && size != 0 {
		extents, err = e.dataExtents(inode.PrivateID)
		if err != nil {
			return nil, fmt.Errorf("read file extents: %w", err)
		}
	}
	reader, err := newAPFSExtentReader(e.device, extents, size)
	if err != nil {
		return nil, err
	}

	return &apfsFile{ReaderAt: reader, size: int64(size)}, nil
}

func (e *apfsExtractor) resolvePath(path string) ([]types.NodeEntry, error) {
	objectID := uint64(types.FSROOT_OID)
	for _, name := range strings.Split(strings.Trim(path, "/"), "/") {
		records, err := e.recordsForObject(objectID)
		if err != nil {
			return nil, err
		}

		found := false
		for _, record := range records {
			if record.Hdr.GetType() != types.APFS_TYPE_DIR_REC {
				continue
			}
			key, keyOK := record.Key.(types.JDrecHashedKeyT)
			value, valueOK := record.Val.(types.JDrecVal)
			if keyOK && valueOK && key.Name == name {
				objectID = value.FileID
				found = true

				break
			}
		}
		if !found {
			return nil, fmt.Errorf("path component %q was not found", name)
		}
	}

	return e.recordsForObject(objectID)
}

func (e *apfsExtractor) recordsForObject(objectID uint64) ([]types.NodeEntry, error) {
	visited := make(map[uint64]struct{})

	return e.collectRecords(e.filesystem.FSRootBtree, objectID, visited, 0)
}

func (e *apfsExtractor) collectRecords(node types.BTreeNodePhys, objectID uint64, visited map[uint64]struct{}, depth int) ([]types.NodeEntry, error) {
	if depth > apfsMaximumTreeDepth {
		return nil, fmt.Errorf("filesystem tree exceeds maximum depth")
	}
	if node.IsLeaf() {
		var records []types.NodeEntry
		for _, raw := range node.Entries {
			record, ok := raw.(types.NodeEntry)
			if ok && record.Hdr.GetID() == objectID {
				records = append(records, record)
			}
		}

		return records, nil
	}

	var records []types.NodeEntry
	for index, raw := range node.Entries {
		entry, ok := raw.(types.NodeEntry)
		if !ok {
			return nil, fmt.Errorf("filesystem tree contains an invalid index entry")
		}
		lower := entry.Hdr.GetID()
		upper := ^uint64(0)
		if index+1 < len(node.Entries) {
			next, ok := node.Entries[index+1].(types.NodeEntry)
			if !ok {
				return nil, fmt.Errorf("filesystem tree contains an invalid index entry")
			}
			upper = next.Hdr.GetID()
		}
		if objectID < lower || objectID > upper {
			continue
		}

		child, address, err := e.filesystemChild(entry)
		if err != nil {
			return nil, err
		}
		if _, seen := visited[address]; seen {
			continue
		}
		visited[address] = struct{}{}
		childRecords, err := e.collectRecords(child, objectID, visited, depth+1)
		if err != nil {
			return nil, err
		}
		records = append(records, childRecords...)
	}

	return records, nil
}

func (e *apfsExtractor) filesystemChild(entry types.NodeEntry) (types.BTreeNodePhys, uint64, error) {
	var objectID types.OidT
	var expectedHash *[sha256.Size]byte
	switch value := entry.Val.(type) {
	case types.BTreeNodeIndexNodeValT:
		objectID = types.OidT(e.rootTreeBase + uint64(value.ChildOid))
		expectedHash = &value.ChildHash
	case uint64:
		objectID = types.OidT(value)
	default:
		return types.BTreeNodePhys{}, 0, fmt.Errorf("sealed filesystem tree contains an invalid child reference")
	}

	mapping, err := e.volumeOMap.GetOMapEntry(e.device, objectID, e.transactionID)
	if err != nil {
		return types.BTreeNodePhys{}, 0, fmt.Errorf("resolve filesystem child %#x: %w", objectID, err)
	}

	if expectedHash != nil {
		raw := make([]byte, apfsBlockSize)
		if _, err := readAPFSBlock(e.device, raw, mapping.Val.Paddr); err != nil {
			return types.BTreeNodePhys{}, 0, fmt.Errorf("read filesystem child %#x: %w", objectID, err)
		}
		if digest := sha256.Sum256(raw); digest != *expectedHash {
			return types.BTreeNodePhys{}, 0, fmt.Errorf("filesystem child %#x failed SHA-256 verification", objectID)
		}
	}

	object, err := types.ReadObj(e.device, mapping.Val.Paddr)
	if err != nil {
		return types.BTreeNodePhys{}, 0, fmt.Errorf("decode filesystem child %#x: %w", objectID, err)
	}
	child, ok := object.Body.(types.BTreeNodePhys)
	if !ok {
		return types.BTreeNodePhys{}, 0, fmt.Errorf("filesystem child %#x is not a tree node", objectID)
	}

	return child, mapping.Val.Paddr, nil
}

func (e *apfsExtractor) physicalExtents(objectID uint64) ([]apfsExtent, error) {
	object, err := types.ReadObj(e.device, uint64(e.filesystem.Volume.FextTreeOid))
	if err != nil {
		return nil, fmt.Errorf("read physical extent tree: %w", err)
	}
	root, ok := object.Body.(types.BTreeNodePhys)
	if !ok {
		return nil, fmt.Errorf("physical extent tree root is not a tree node")
	}

	extents, err := e.collectPhysicalExtents(root, objectID, make(map[uint64]struct{}), 0)
	if err != nil {
		return nil, err
	}
	sort.Slice(extents, func(i, j int) bool { return extents[i].logical < extents[j].logical })

	return extents, nil
}

func (e *apfsExtractor) dataExtents(objectID uint64) ([]apfsExtent, error) {
	if e.filesystem.Volume.FextTreeOid != 0 {
		return e.physicalExtents(objectID)
	}

	records, err := e.recordsForObject(objectID)
	if err != nil {
		return nil, err
	}
	var extents []apfsExtent
	for _, record := range records {
		if record.Hdr.GetType() != types.APFS_TYPE_FILE_EXTENT {
			continue
		}
		key, keyOK := record.Key.(types.JFileExtentKeyT)
		value, valueOK := record.Val.(types.JFileExtentValT)
		if keyOK && valueOK {
			extents = append(extents, apfsExtent{
				logical:  key.LogicalAddr,
				physical: value.PhysBlockNum,
				length:   value.Length(),
			})
		}
	}
	sort.Slice(extents, func(i, j int) bool { return extents[i].logical < extents[j].logical })

	return extents, nil
}

func (e *apfsExtractor) collectPhysicalExtents(node types.BTreeNodePhys, objectID uint64, visited map[uint64]struct{}, depth int) ([]apfsExtent, error) {
	if depth > apfsMaximumTreeDepth {
		return nil, fmt.Errorf("physical extent tree exceeds maximum depth")
	}
	if node.IsLeaf() {
		var extents []apfsExtent
		for _, raw := range node.Entries {
			entry, ok := raw.(types.FextNodeEntry)
			if ok && entry.Key.PrivateID == objectID {
				extents = append(extents, apfsExtent{
					logical:  entry.Key.LogicalAddr,
					physical: entry.Val.PhysBlockNum,
					length:   entry.Val.Length(),
				})
			}
		}

		return extents, nil
	}

	var extents []apfsExtent
	for index, raw := range node.Entries {
		entry, ok := raw.(types.OMapNodeEntry)
		if !ok {
			return nil, fmt.Errorf("physical extent tree contains an invalid index entry")
		}
		lower := uint64(entry.Key.Oid)
		upper := ^uint64(0)
		if index+1 < len(node.Entries) {
			next, ok := node.Entries[index+1].(types.OMapNodeEntry)
			if !ok {
				return nil, fmt.Errorf("physical extent tree contains an invalid index entry")
			}
			upper = uint64(next.Key.Oid)
		}
		if objectID < lower || objectID > upper {
			continue
		}
		if _, seen := visited[entry.PAddr]; seen {
			continue
		}
		visited[entry.PAddr] = struct{}{}

		object, err := types.ReadObj(e.device, entry.PAddr)
		if err != nil {
			return nil, fmt.Errorf("read physical extent child %#x: %w", entry.PAddr, err)
		}
		child, ok := object.Body.(types.BTreeNodePhys)
		if !ok {
			return nil, fmt.Errorf("physical extent child %#x is not a tree node", entry.PAddr)
		}
		childExtents, err := e.collectPhysicalExtents(child, objectID, visited, depth+1)
		if err != nil {
			return nil, err
		}
		extents = append(extents, childExtents...)
	}

	return extents, nil
}

func readAPFSBlock(device disk.Device, data []byte, physicalAddress uint64) (int, error) {
	var count int
	var err error
	if raw, ok := device.(*readerDevice); ok {
		count, err = raw.source.ReadAt(data, int64(physicalAddress*apfsBlockSize))
	} else {
		count, err = device.ReadAt(data, int64(physicalAddress*apfsBlockSize))
	}
	if err != nil {
		return count, fmt.Errorf("read Apple filesystem block: %w", err)
	}

	return count, nil
}

type apfsExtentReader struct {
	device  disk.Device
	extents []apfsExtent
	size    uint64
}

func newAPFSExtentReader(device disk.Device, extents []apfsExtent, size uint64) (*apfsExtentReader, error) {
	if size > math.MaxInt64 {
		return nil, fmt.Errorf("file size %d exceeds the supported maximum", size)
	}

	sort.Slice(extents, func(i, j int) bool { return extents[i].logical < extents[j].logical })
	position := uint64(0)
	for _, extent := range extents {
		if extent.length == 0 || extent.logical != position || extent.physical > ^uint64(0)/apfsBlockSize {
			return nil, fmt.Errorf("file has invalid or sparse physical extents")
		}
		physicalOffset := extent.physical * apfsBlockSize
		if physicalOffset > device.GetSize() || extent.length > device.GetSize()-physicalOffset {
			return nil, fmt.Errorf("file extent exceeds the Apple filesystem image")
		}
		position += extent.length
		if position < extent.logical {
			return nil, fmt.Errorf("file extent length overflows")
		}
	}
	if position < size {
		return nil, fmt.Errorf("file extents cover %d bytes, expected %d", position, size)
	}

	return &apfsExtentReader{device: device, extents: extents, size: size}, nil
}

func (r *apfsExtentReader) ReadAt(data []byte, offset int64) (int, error) {
	if offset < 0 {
		return 0, fmt.Errorf("negative file offset")
	}
	if len(data) == 0 {
		return 0, nil
	}
	position := uint64(offset)
	if position >= r.size {
		return 0, io.EOF
	}

	written := 0
	for written < len(data) && position < r.size {
		index := sort.Search(len(r.extents), func(index int) bool {
			extent := r.extents[index]

			return extent.logical+extent.length > position
		})
		if index == len(r.extents) || position < r.extents[index].logical {
			return written, io.ErrUnexpectedEOF
		}
		extent := r.extents[index]
		available := min(extent.logical+extent.length-position, r.size-position)
		count := min(uint64(len(data)-written), available)
		physicalOffset := extent.physical*apfsBlockSize + position - extent.logical
		n, err := r.device.ReadAt(data[written:written+int(count)], int64(physicalOffset))
		written += n
		position += uint64(n)
		if err != nil {
			return written, err //nolint:wrapcheck // ReaderAt implementations must preserve io.EOF.
		}
		if n != int(count) {
			return written, io.ErrUnexpectedEOF
		}
	}
	if written != len(data) {
		return written, io.EOF
	}

	return written, nil
}

type decmpfsReader struct {
	ctx        context.Context
	source     io.ReaderAt
	outputSize uint64
	method     uint32
	offsets    []uint32
	mu         sync.Mutex
	cache      map[uint64][]byte
	cacheOrder []uint64
}

func newDecmpfsReader(ctx context.Context, source io.ReaderAt, sourceSize, outputSize uint64, method uint32) (*decmpfsReader, error) {
	if outputSize > math.MaxInt64 {
		return nil, fmt.Errorf("compressed file size %d exceeds the supported maximum", outputSize)
	}
	if sourceSize < 8 {
		return nil, fmt.Errorf("compressed resource fork is too small")
	}

	var first [4]byte
	if _, err := source.ReadAt(first[:], 0); err != nil {
		return nil, fmt.Errorf("read compression index: %w", err)
	}
	tableSize := uint64(binary.LittleEndian.Uint32(first[:]))
	blockCount := (outputSize + decmpfsBlockSize - 1) / decmpfsBlockSize
	expectedTableSize := (blockCount + 1) * 4
	if tableSize != expectedTableSize || tableSize > sourceSize {
		return nil, fmt.Errorf("compression index has size %d, expected %d", tableSize, expectedTableSize)
	}

	table := make([]byte, tableSize)
	if _, err := source.ReadAt(table, 0); err != nil {
		return nil, fmt.Errorf("read compression index: %w", err)
	}
	offsets := make([]uint32, blockCount+1)
	for index := range offsets {
		offsets[index] = binary.LittleEndian.Uint32(table[index*4:])
	}
	for index := uint64(0); index < blockCount; index++ {
		start, end := uint64(offsets[index]), uint64(offsets[index+1])
		if start < tableSize || end <= start || end > sourceSize || end-start > decmpfsBlockSize+1 {
			return nil, fmt.Errorf("compressed block %d has invalid range %d..%d", index, start, end)
		}
	}
	if uint64(offsets[len(offsets)-1]) != sourceSize {
		return nil, fmt.Errorf("compressed blocks end at %d, expected %d", offsets[len(offsets)-1], sourceSize)
	}

	return &decmpfsReader{
		ctx:        ctx,
		source:     source,
		outputSize: outputSize,
		method:     method,
		offsets:    offsets,
		cache:      make(map[uint64][]byte),
	}, nil
}

func (r *decmpfsReader) ReadAt(data []byte, offset int64) (int, error) {
	if offset < 0 {
		return 0, fmt.Errorf("negative compressed file offset")
	}
	if len(data) == 0 {
		return 0, nil
	}
	if err := r.ctx.Err(); err != nil {
		return 0, err //nolint:wrapcheck // ReaderAt must preserve context cancellation.
	}
	position := uint64(offset)
	if position >= r.outputSize {
		return 0, io.EOF
	}

	wanted := len(data)
	if remaining := r.outputSize - position; uint64(wanted) > remaining {
		wanted = int(remaining)
	}
	written := 0
	for written < wanted {
		blockIndex := position / decmpfsBlockSize
		block, err := r.block(blockIndex)
		if err != nil {
			return written, err
		}
		blockOffset := position % decmpfsBlockSize
		count := min(wanted-written, len(block)-int(blockOffset))
		copy(data[written:written+count], block[blockOffset:uint64(count)+blockOffset])
		written += count
		position += uint64(count)
	}
	if written != len(data) {
		return written, io.EOF
	}

	return written, nil
}

func (r *decmpfsReader) block(index uint64) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if block, ok := r.cache[index]; ok {
		return block, nil
	}
	if index+1 >= uint64(len(r.offsets)) {
		return nil, io.EOF
	}
	if err := r.ctx.Err(); err != nil {
		return nil, fmt.Errorf("read compressed file: %w", err)
	}

	start, end := uint64(r.offsets[index]), uint64(r.offsets[index+1])
	compressed := make([]byte, end-start)
	if _, err := r.source.ReadAt(compressed, int64(start)); err != nil {
		return nil, fmt.Errorf("read compressed block %d: %w", index, err)
	}
	expected := min(uint64(decmpfsBlockSize), r.outputSize-index*decmpfsBlockSize)
	plain, err := decompressBlock(compressed, r.method, int(expected))
	if err != nil {
		return nil, fmt.Errorf("decode compressed block %d: %w", index, err)
	}
	if uint64(len(plain)) != expected {
		return nil, fmt.Errorf("decompressed block %d has size %d, expected %d", index, len(plain), expected)
	}

	const maximumCachedBlocks = 128
	if len(r.cacheOrder) == maximumCachedBlocks {
		delete(r.cache, r.cacheOrder[0])
		r.cacheOrder = r.cacheOrder[1:]
	}
	r.cache[index] = plain
	r.cacheOrder = append(r.cacheOrder, index)

	return plain, nil
}

func decompressBlock(compressed []byte, method uint32, maximumSize int) ([]byte, error) {
	switch method {
	case decmpfsLZVN:
		if compressed[0] == 0x06 {
			return compressed[1:], nil
		}

		decoded, err := lzfse.DecompressLZVN(compressed, maximumSize)
		if err != nil {
			return nil, fmt.Errorf("decompress LZVN block: %w", err)
		}

		return decoded, nil
	case decmpfsLZFSE:
		if compressed[0] != 'b' {
			return compressed[1:], nil
		}

		decoded, err := lzfse.Decompress(compressed)
		if err != nil {
			return nil, fmt.Errorf("decompress LZFSE block: %w", err)
		}

		return decoded, nil
	case decmpfsLZBitmap:
		if compressed[0] == 0xff {
			return compressed[1:], nil
		}

		decoded, err := lzbitmap.Decompress(compressed)
		if err != nil {
			return nil, fmt.Errorf("decompress LZBITMAP block: %w", err)
		}

		return decoded, nil
	default:
		return nil, fmt.Errorf("unsupported compression type %d", method)
	}
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err //nolint:wrapcheck // Preserve context cancellation for io.CopyN.
	}

	return r.reader.Read(data) //nolint:wrapcheck // Reader adapters must preserve io.EOF.
}

func openAPFSDevice(source io.ReaderAt, size int64) (disk.Device, func() error, error) {
	if size < 36 {
		return nil, nil, fmt.Errorf("apple filesystem image is too small: %d", size)
	}

	header := make([]byte, 36)
	if _, err := source.ReadAt(header, 0); err != nil {
		return nil, nil, fmt.Errorf("read Apple filesystem header: %w", err)
	}

	if bytes.Equal(header[32:36], []byte("NXSB")) {
		device := &readerDevice{source: source, size: uint64(size)}

		return device, device.Close, nil
	}

	image, err := dmg.NewDMG(io.NewSectionReader(source, 0, size))
	if err != nil {
		return nil, nil, fmt.Errorf("open Apple disk image: %w", err)
	}

	return image, image.Close, nil
}

type readerDevice struct {
	source io.ReaderAt
	size   uint64
}

func (d *readerDevice) Close() error {
	return nil
}

func (d *readerDevice) ReadFile(writer *bufio.Writer, offset, length int64) error {
	if offset < 0 || length < 0 || uint64(offset) > d.size || uint64(length) > d.size-uint64(offset) {
		return fmt.Errorf("invalid Apple filesystem range %d+%d", offset, length)
	}

	if _, err := io.CopyN(writer, io.NewSectionReader(d.source, offset, length), length); err != nil {
		return fmt.Errorf("copy Apple filesystem range: %w", err)
	}

	if err := writer.Flush(); err != nil {
		return fmt.Errorf("flush Apple filesystem range: %w", err)
	}

	return nil
}

func (d *readerDevice) GetSize() uint64 {
	return d.size
}

func (d *readerDevice) ReadAt(data []byte, offset int64) (int, error) {
	count, err := d.source.ReadAt(data, offset)
	if count != len(data) || len(data) != int(types.BLOCK_SIZE) || offset%int64(types.BLOCK_SIZE) != 0 {
		return count, err //nolint:wrapcheck // ReaderAt implementations must preserve io.EOF.
	}

	if !bytes.Equal(data[:apfsObjectHeaderSize], make([]byte, apfsObjectHeaderSize)) {
		return count, err //nolint:wrapcheck // ReaderAt implementations must preserve io.EOF.
	}

	flags := binary.LittleEndian.Uint16(data[apfsObjectHeaderSize:])
	if flags&apfsNodeNoHeader == 0 {
		return count, err //nolint:wrapcheck // ReaderAt implementations must preserve io.EOF.
	}

	objectType := uint32(apfsObjectTypeNode)
	if flags&apfsNodeRoot != 0 {
		objectType = apfsObjectTypeBTree
	}

	// Sealed APFS tree nodes omit the normal object header. Add a synthetic
	// header after their hash has been verified so go-apfs can decode them.
	binary.LittleEndian.PutUint64(data[8:16], uint64(offset/int64(types.BLOCK_SIZE)))
	binary.LittleEndian.PutUint32(data[24:28], apfsObjectNoHeader|objectType)
	binary.LittleEndian.PutUint32(data[28:32], apfsObjectTypeFSTree)
	binary.LittleEndian.PutUint64(data[:8], types.CreateChecksum(data))

	return count, err //nolint:wrapcheck // ReaderAt implementations must preserve io.EOF.
}
