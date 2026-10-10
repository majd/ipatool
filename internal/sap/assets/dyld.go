//nolint:wsl // Binary format parsing is clearer when related bounds checks remain adjacent.
package assets

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/blacktop/go-macho"
	"github.com/blacktop/go-macho/pkg/fixupchains"
	machotypes "github.com/blacktop/go-macho/types"
)

const (
	dyldHeaderSize                = 568
	dyldMappingSize               = 56
	dyldImageInfoSize             = 32
	dyldSubcacheEntrySize         = 56
	dyldMaximumMappings           = 64
	dyldMaximumImages             = 100_000
	dyldMaximumSubcaches          = 128
	dyldMaximumPathLength         = 4096
	dyldSlideInfo5Size            = 24
	dyldMachHeader64Size          = 32
	dyldMachSegment64Command      = 0x19
	dyldMaximumLoadCommandsSize   = 16 << 20
	dyldMaximumSlideChainPointers = 1 << 16
)

type dyldCache struct {
	members []*dyldCacheMember
	primary *dyldCacheMember
	slide5  *dyldSlideInfo5
}

type dyldCacheMember struct {
	file     io.ReaderAt
	close    func() error
	size     int64
	uuid     [16]byte
	mappings []dyldCacheMapping
}

type dyldCacheMapping struct {
	member          *dyldCacheMember
	address         uint64
	size            uint64
	fileOffset      uint64
	slideInfoOffset uint64
	slideInfoSize   uint64
}

type dyldSlideInfo5 struct {
	pageSize        uint32
	pageStartsCount uint32
	valueAdd        uint64
}

type dyldCacheFile struct {
	reader io.ReaderAt
	size   int64
	close  func() error
}

type dyldCacheOpener func(suffix string) (*dyldCacheFile, error)

type dyldImage struct {
	member  *dyldCacheMember
	address uint64
	offset  uint64
}

type dyldRebase struct {
	address uint64
	target  uint64
}

func extractDyldImageFromFiles(opener dyldCacheOpener, imagePath, outputPath string) error {
	cache, err := openDyldCache(opener)
	if err != nil {
		return fmt.Errorf("open shared cache: %w", err)
	}
	defer cache.Close()

	image, err := cache.image(imagePath)
	if err != nil {
		return fmt.Errorf("find %s in shared cache: %w", imagePath, err)
	}
	imageMachO, err := cache.openImage(image)
	if err != nil {
		return fmt.Errorf("read %s from shared cache: %w", imagePath, err)
	}
	defer imageMachO.Close()

	var chainedFixups *fixupchains.DyldChainedFixups
	if imageMachO.HasFixups() {
		chainedFixups, err = imageMachO.DyldChainedFixups()
		if err != nil {
			return fmt.Errorf("read %s chained fixups: %w", imagePath, err)
		}
	}
	if err := imageMachO.Export(outputPath, chainedFixups, imageMachO.GetBaseAddress(), nil); err != nil {
		return fmt.Errorf("export %s from shared cache: %w", imagePath, err)
	}

	if err := cache.applySlide(outputPath); err != nil {
		return fmt.Errorf("apply %s shared-cache pointers: %w", imagePath, err)
	}

	return nil
}

func (c *dyldCache) openImage(image *dyldImage) (*macho.File, error) {
	linkeditAddress, err := image.linkeditAddress()
	if err != nil {
		return nil, err
	}
	linkedit, err := c.mappingForAddress(linkeditAddress)
	if err != nil {
		return nil, err
	}

	reader := &dyldCacheReader{cache: c, member: linkedit.member, offset: 0}
	converter := machotypes.VMAddrConverter{
		Converter: c.slidePointer,
		VMAddr2Offet: func(address uint64) (uint64, error) {
			mapping, err := c.mappingForAddress(address)
			if err != nil {
				return 0, err
			}

			return mapping.fileOffset + address - mapping.address, nil
		},
		Offet2VMAddr: func(offset uint64) (uint64, error) {
			return c.addressForOffset(image.member, offset)
		},
	}
	sectionReader := machotypes.NewCustomSectionReader(image.member.file, &converter, 0, image.member.size)

	file, err := macho.NewFile(
		io.NewSectionReader(image.member.file, int64(image.offset), image.member.size-int64(image.offset)),
		macho.FileConfig{
			Offset:          int64(image.offset),
			SectionReader:   sectionReader,
			CacheReader:     reader,
			VMAddrConverter: converter,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("parse shared-cache image: %w", err)
	}

	return file, nil
}

func openDyldCache(opener dyldCacheOpener) (*dyldCache, error) {
	primary, header, err := openDyldCacheMember(opener, "")
	if err != nil {
		return nil, err
	}
	cache := &dyldCache{members: []*dyldCacheMember{primary}, primary: primary}
	complete := false

	defer func() {
		if !complete {
			_ = cache.Close()
		}
	}()

	subcacheOffset := uint64(binary.LittleEndian.Uint32(header[392:]))
	subcacheCount := uint64(binary.LittleEndian.Uint32(header[396:]))
	if subcacheCount > dyldMaximumSubcaches || subcacheCount > 0 && !validDyldRange(primary.size, subcacheOffset, subcacheCount*dyldSubcacheEntrySize) {
		return nil, fmt.Errorf("shared cache has an invalid subcache table")
	}
	entries := make([]byte, subcacheCount*dyldSubcacheEntrySize)
	if len(entries) > 0 {
		if _, err := primary.file.ReadAt(entries, int64(subcacheOffset)); err != nil {
			return nil, fmt.Errorf("read shared-cache subcache table: %w", err)
		}
	}
	for index := uint64(0); index < subcacheCount; index++ {
		entry := entries[index*dyldSubcacheEntrySize : (index+1)*dyldSubcacheEntrySize]
		suffix := strings.TrimRight(string(entry[24:56]), "\x00")
		if suffix == "" || strings.ContainsAny(suffix, `/\\`) {
			return nil, fmt.Errorf("shared cache has an invalid subcache suffix")
		}
		member, _, err := openDyldCacheMember(opener, suffix)
		if err != nil {
			return nil, fmt.Errorf("open shared-cache member %s: %w", suffix, err)
		}
		if !bytes.Equal(member.uuid[:], entry[:16]) {
			_ = member.close()

			return nil, fmt.Errorf("shared-cache member %s has an unexpected UUID", suffix)
		}
		cache.members = append(cache.members, member)
	}

	for _, member := range cache.members {
		for _, mapping := range member.mappings {
			if mapping.slideInfoSize == 0 {
				continue
			}
			version, err := readDyldSlideVersion(mapping)
			if err != nil {
				return nil, err
			}
			if version != 5 {
				return nil, fmt.Errorf("unsupported shared-cache slide version %d", version)
			}
			slide, err := readDyldSlideInfo5(mapping)
			if err != nil {
				return nil, err
			}
			if cache.slide5 == nil {
				cache.slide5 = slide
			} else if cache.slide5.pageSize != slide.pageSize || cache.slide5.valueAdd != slide.valueAdd {
				return nil, fmt.Errorf("shared-cache members use incompatible slide information")
			}
		}
	}
	if cache.slide5 == nil {
		return nil, fmt.Errorf("shared cache has no slide information")
	}
	complete = true

	return cache, nil
}

func openDyldCacheMember(opener dyldCacheOpener, suffix string) (*dyldCacheMember, []byte, error) {
	opened, err := opener(suffix)
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (*dyldCacheMember, []byte, error) {
		_ = opened.close()

		return nil, nil, err
	}
	if opened.size < dyldHeaderSize {
		return fail(fmt.Errorf("shared-cache member is too small"))
	}
	header := make([]byte, dyldHeaderSize)
	if _, err := opened.reader.ReadAt(header, 0); err != nil {
		return fail(err)
	}
	magic := strings.TrimRight(string(header[:16]), "\x00")
	if magic != "dyld_v1  arm64e" {
		return fail(fmt.Errorf("unsupported shared-cache format %q", magic))
	}

	mappingOffset := uint64(binary.LittleEndian.Uint32(header[312:]))
	mappingCount := uint64(binary.LittleEndian.Uint32(header[316:]))
	if mappingCount == 0 || mappingCount > dyldMaximumMappings || !validDyldRange(opened.size, mappingOffset, mappingCount*dyldMappingSize) {
		return fail(fmt.Errorf("shared-cache member has an invalid mapping table"))
	}
	mappingData := make([]byte, mappingCount*dyldMappingSize)
	if _, err := opened.reader.ReadAt(mappingData, int64(mappingOffset)); err != nil {
		return fail(err)
	}

	member := &dyldCacheMember{file: opened.reader, close: opened.close, size: opened.size}
	copy(member.uuid[:], header[88:104])

	for index := uint64(0); index < mappingCount; index++ {
		data := mappingData[index*dyldMappingSize : (index+1)*dyldMappingSize]
		mapping := dyldCacheMapping{
			member:          member,
			address:         binary.LittleEndian.Uint64(data[0:]),
			size:            binary.LittleEndian.Uint64(data[8:]),
			fileOffset:      binary.LittleEndian.Uint64(data[16:]),
			slideInfoOffset: binary.LittleEndian.Uint64(data[24:]),
			slideInfoSize:   binary.LittleEndian.Uint64(data[32:]),
		}
		if mapping.size == 0 || mapping.address+mapping.size < mapping.address || !validDyldRange(opened.size, mapping.fileOffset, mapping.size) || mapping.slideInfoSize != 0 && !validDyldRange(opened.size, mapping.slideInfoOffset, mapping.slideInfoSize) {
			return fail(fmt.Errorf("shared-cache member has an invalid mapping"))
		}

		member.mappings = append(member.mappings, mapping)
	}

	return member, header, nil
}

func validDyldRange(size int64, offset, length uint64) bool {
	return size >= 0 && offset <= uint64(size) && length <= uint64(size)-offset
}

func (c *dyldCache) Close() error {
	var first error
	for _, member := range c.members {
		if err := member.close(); err != nil && first == nil {
			first = err
		}
	}

	return first
}

func (c *dyldCache) image(path string) (*dyldImage, error) {
	header := make([]byte, dyldHeaderSize)
	if _, err := c.primary.file.ReadAt(header, 0); err != nil {
		return nil, fmt.Errorf("read shared-cache header: %w", err)
	}
	imagesOffset := uint64(binary.LittleEndian.Uint32(header[448:]))
	imagesCount := uint64(binary.LittleEndian.Uint32(header[452:]))
	if imagesCount == 0 || imagesCount > dyldMaximumImages || !validDyldRange(c.primary.size, imagesOffset, imagesCount*dyldImageInfoSize) {
		return nil, fmt.Errorf("shared cache has an invalid image table")
	}
	images := make([]byte, imagesCount*dyldImageInfoSize)
	if _, err := c.primary.file.ReadAt(images, int64(imagesOffset)); err != nil {
		return nil, fmt.Errorf("read shared-cache image table: %w", err)
	}
	for index := uint64(0); index < imagesCount; index++ {
		data := images[index*dyldImageInfoSize : (index+1)*dyldImageInfoSize]
		address := binary.LittleEndian.Uint64(data)
		pathOffset := uint64(binary.LittleEndian.Uint32(data[24:]))
		name, err := readDyldCString(c.primary.file, c.primary.size, pathOffset)
		if err != nil {
			return nil, err
		}
		if name != path {
			continue
		}
		mapping, err := c.mappingForAddress(address)
		if err != nil {
			return nil, err
		}

		return &dyldImage{
			member:  mapping.member,
			address: address,
			offset:  mapping.fileOffset + address - mapping.address,
		}, nil
	}

	return nil, fmt.Errorf("image was not found")
}

func readDyldCString(file io.ReaderAt, size int64, offset uint64) (string, error) {
	if !validDyldRange(size, offset, 1) {
		return "", fmt.Errorf("shared cache contains an invalid string offset")
	}
	limit := min(uint64(dyldMaximumPathLength), uint64(size)-offset)
	data := make([]byte, limit)
	n, err := file.ReadAt(data, int64(offset))
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("read shared-cache string: %w", err)
	}
	data = data[:n]
	if end := bytes.IndexByte(data, 0); end >= 0 {
		return string(data[:end]), nil
	}

	return "", fmt.Errorf("shared-cache string is not terminated")
}

func (c *dyldCache) mappingForAddress(address uint64) (*dyldCacheMapping, error) {
	for _, member := range c.members {
		for index := range member.mappings {
			mapping := &member.mappings[index]
			if mapping.address <= address && address < mapping.address+mapping.size {
				return mapping, nil
			}
		}
	}

	return nil, fmt.Errorf("address %#x is outside the shared cache", address)
}

func (c *dyldCache) addressForOffset(member *dyldCacheMember, offset uint64) (uint64, error) {
	for _, mapping := range member.mappings {
		if mapping.fileOffset <= offset && offset < mapping.fileOffset+mapping.size {
			return mapping.address + offset - mapping.fileOffset, nil
		}
	}

	return 0, fmt.Errorf("offset %#x is outside the shared-cache member", offset)
}

func (c *dyldCache) slidePointer(pointer uint64) uint64 {
	if pointer == 0 {
		return 0
	}
	if _, err := c.mappingForAddress(pointer); err == nil {
		return pointer
	}

	return c.slide5.decode(pointer)
}

func (i *dyldImage) linkeditAddress() (uint64, error) {
	header := make([]byte, dyldMachHeader64Size)
	if _, err := i.member.file.ReadAt(header, int64(i.offset)); err != nil {
		return 0, fmt.Errorf("read Mach-O header: %w", err)
	}
	if binary.LittleEndian.Uint32(header) != uint32(machotypes.Magic64) {
		return 0, fmt.Errorf("image has an invalid Mach-O magic")
	}
	commandCount := binary.LittleEndian.Uint32(header[16:])
	commandsSize := uint64(binary.LittleEndian.Uint32(header[20:]))
	if commandsSize > dyldMaximumLoadCommandsSize || !validDyldRange(i.member.size, i.offset+dyldMachHeader64Size, commandsSize) {
		return 0, fmt.Errorf("image has invalid load commands")
	}
	commands := make([]byte, commandsSize)
	if _, err := i.member.file.ReadAt(commands, int64(i.offset+dyldMachHeader64Size)); err != nil {
		return 0, fmt.Errorf("read Mach-O load commands: %w", err)
	}
	for index, offset := uint32(0), uint64(0); index < commandCount; index++ {
		if offset+8 > uint64(len(commands)) {
			return 0, fmt.Errorf("image load commands are truncated")
		}
		commandSize := uint64(binary.LittleEndian.Uint32(commands[offset+4:]))
		if commandSize < 8 || offset+commandSize > uint64(len(commands)) {
			return 0, fmt.Errorf("image has an invalid load command")
		}
		if binary.LittleEndian.Uint32(commands[offset:]) == dyldMachSegment64Command && commandSize >= 72 {
			name := strings.TrimRight(string(commands[offset+8:offset+24]), "\x00")
			if name == "__LINKEDIT" {
				return binary.LittleEndian.Uint64(commands[offset+24:]), nil
			}
		}
		offset += commandSize
	}

	return 0, fmt.Errorf("image has no __LINKEDIT segment")
}

type dyldCacheReader struct {
	cache  *dyldCache
	member *dyldCacheMember
	offset int64
}

func (r *dyldCacheReader) Read(data []byte) (int, error) {
	n, err := r.member.file.ReadAt(data, r.offset)
	r.offset += int64(n)

	return n, err //nolint:wrapcheck // Reader adapters must preserve io.EOF.
}

func (r *dyldCacheReader) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		offset += r.offset
	case io.SeekEnd:
		offset += r.member.size
	default:
		return 0, fmt.Errorf("invalid seek mode")
	}
	if offset < 0 {
		return 0, fmt.Errorf("negative shared-cache offset")
	}
	r.offset = offset

	return offset, nil
}

func (r *dyldCacheReader) ReadAt(data []byte, offset int64) (int, error) {
	return r.member.file.ReadAt(data, offset) //nolint:wrapcheck // ReaderAt implementations must preserve io.EOF.
}

func (r *dyldCacheReader) SeekToAddr(address uint64) error {
	mapping, err := r.cache.mappingForAddress(address)
	if err != nil {
		return err
	}
	r.member = mapping.member
	r.offset = int64(mapping.fileOffset + address - mapping.address)

	return nil
}

func (r *dyldCacheReader) ReadAtAddr(data []byte, address uint64) (int, error) {
	written := 0
	for written < len(data) {
		mapping, err := r.cache.mappingForAddress(address)
		if err != nil {
			return written, err
		}
		available := mapping.address + mapping.size - address
		count := min(uint64(len(data)-written), available)
		offset := mapping.fileOffset + address - mapping.address
		n, err := mapping.member.file.ReadAt(data[written:written+int(count)], int64(offset))
		written += n
		address += uint64(n)
		if err != nil {
			return written, err //nolint:wrapcheck // ReaderAt implementations must preserve io.EOF.
		}
		if n != int(count) {
			return written, io.ErrUnexpectedEOF
		}
	}

	return written, nil
}

func readDyldSlideVersion(mapping dyldCacheMapping) (uint32, error) {
	if mapping.slideInfoSize < 4 {
		return 0, fmt.Errorf("shared cache has truncated slide information")
	}
	var data [4]byte
	if _, err := mapping.member.file.ReadAt(data[:], int64(mapping.slideInfoOffset)); err != nil {
		return 0, fmt.Errorf("read shared-cache slide version: %w", err)
	}

	return binary.LittleEndian.Uint32(data[:]), nil
}

func readDyldSlideInfo5(mapping dyldCacheMapping) (*dyldSlideInfo5, error) {
	if mapping.slideInfoSize < dyldSlideInfo5Size {
		return nil, fmt.Errorf("shared cache has truncated slide information")
	}
	data := make([]byte, dyldSlideInfo5Size)
	if _, err := mapping.member.file.ReadAt(data, int64(mapping.slideInfoOffset)); err != nil {
		return nil, fmt.Errorf("read shared-cache slide information: %w", err)
	}
	if binary.LittleEndian.Uint32(data) != 5 {
		return nil, fmt.Errorf("unsupported shared-cache slide version %d", binary.LittleEndian.Uint32(data))
	}
	slide := &dyldSlideInfo5{
		pageSize:        binary.LittleEndian.Uint32(data[4:]),
		pageStartsCount: binary.LittleEndian.Uint32(data[8:]),
		valueAdd:        binary.LittleEndian.Uint64(data[16:]),
	}
	if slide.pageSize == 0 || uint64(dyldSlideInfo5Size)+uint64(slide.pageStartsCount)*2 > mapping.slideInfoSize {
		return nil, fmt.Errorf("shared cache has invalid slide information")
	}

	return slide, nil
}

func (s *dyldSlideInfo5) decode(pointer uint64) uint64 {
	if pointer == 0 {
		return 0
	}
	target := pointer & ((uint64(1) << 34) - 1)
	if pointer>>63 == 0 {
		target |= (pointer >> 34 & 0xff) << 56
	}

	return s.valueAdd + target
}

func (c *dyldCache) applySlide(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open exported image: %w", err)
	}
	defer file.Close()

	exported, err := macho.NewFile(file)
	if err != nil {
		return fmt.Errorf("parse exported image: %w", err)
	}
	defer exported.Close()

	for _, segment := range exported.Segments() {
		mapping, err := c.mappingForAddress(segment.Addr)
		if err != nil {
			return err
		}
		if mapping.slideInfoOffset == 0 {
			continue
		}
		pageSize, err := c.slidePageSize(*mapping)
		if err != nil {
			return err
		}
		start := (segment.Addr - mapping.address) / pageSize
		end := (segment.Addr + segment.Memsz - mapping.address + pageSize) / pageSize
		rebases, err := c.rebases(*mapping, start, end)
		if err != nil {
			return err
		}
		for _, rebase := range rebases {
			offset, err := exported.GetOffset(rebase.address)
			if err != nil {
				continue
			}
			if _, err := file.Seek(int64(offset), io.SeekStart); err != nil {
				return fmt.Errorf("seek to exported pointer %#x: %w", offset, err)
			}
			if err := binary.Write(file, binary.LittleEndian, rebase.target); err != nil {
				return fmt.Errorf("write exported pointer %#x: %w", rebase.target, err)
			}
		}
	}

	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync exported image: %w", err)
	}

	return nil
}

func (c *dyldCache) rebases(mapping dyldCacheMapping, startPage, endPage uint64) ([]dyldRebase, error) {
	version, err := readDyldSlideVersion(mapping)
	if err != nil {
		return nil, err
	}
	if version != 5 {
		return nil, fmt.Errorf("unsupported shared-cache slide version %d", version)
	}

	slide, err := readDyldSlideInfo5(mapping)
	if err != nil {
		return nil, err
	}
	starts, err := readDyldSlideArray(mapping, dyldSlideInfo5Size, slide.pageStartsCount)
	if err != nil {
		return nil, err
	}
	if endPage == 0 || endPage > uint64(len(starts)) {
		endPage = uint64(len(starts))
	}
	if startPage > endPage {
		return nil, fmt.Errorf("invalid shared-cache slide page range")
	}

	var rebases []dyldRebase

	for page := startPage; page < endPage; page++ {
		start := starts[page]
		if start == 0xffff {
			continue
		}
		pageFileOffset := mapping.fileOffset + page*uint64(slide.pageSize)
		pageAddress := mapping.address + page*uint64(slide.pageSize)
		offset := uint64(start)
		for count := 0; count < dyldMaximumSlideChainPointers; count++ {
			if offset+8 > uint64(slide.pageSize) {
				return nil, fmt.Errorf("shared-cache slide chain leaves its page")
			}
			var data [8]byte
			if _, err := mapping.member.file.ReadAt(data[:], int64(pageFileOffset+offset)); err != nil {
				return nil, fmt.Errorf("read shared-cache slide pointer: %w", err)
			}
			pointer := binary.LittleEndian.Uint64(data[:])
			rebases = append(rebases, dyldRebase{
				address: pageAddress + offset,
				target:  slide.decode(pointer),
			})
			next := pointer >> 52 & 0x7ff
			if next == 0 {
				break
			}
			offset += next * 8
			if count == dyldMaximumSlideChainPointers-1 {
				return nil, fmt.Errorf("shared-cache slide chain is too long")
			}
		}
	}

	return rebases, nil
}

func (c *dyldCache) slidePageSize(mapping dyldCacheMapping) (uint64, error) {
	slide, err := readDyldSlideInfo5(mapping)
	if err != nil {
		return 0, err
	}

	return uint64(slide.pageSize), nil
}

func readDyldSlideArray(mapping dyldCacheMapping, offset, count uint32) ([]uint16, error) {
	if count == 0 {
		return nil, nil
	}
	data := make([]byte, uint64(count)*2)
	if _, err := mapping.member.file.ReadAt(data, int64(mapping.slideInfoOffset+uint64(offset))); err != nil {
		return nil, fmt.Errorf("read shared-cache slide table: %w", err)
	}
	values := make([]uint16, count)
	for index := range values {
		values[index] = binary.LittleEndian.Uint16(data[index*2:])
	}

	return values, nil
}
