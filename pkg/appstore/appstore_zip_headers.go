package appstore

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

const (
	zipLocalHeaderSignature    = 0x04034b50
	zipCentralHeaderSignature  = 0x02014b50
	zipDirectoryEndSignature   = 0x06054b50
	zip64DirectoryEndSignature = 0x06064b50
	zip64LocatorSignature      = 0x07064b50
	zip64ExtraID               = 0x0001

	zipLocalHeaderLen    = 30
	zipCentralHeaderLen  = 46
	zipDirectoryEndLen   = 22
	zip64DirectoryEndLen = 56
	zip64LocatorLen      = 20
)

// zipLocalHeaders reads the local metadata that archive/zip's FileHeader omits.
// The central directory provides offsets rather than relying on entry order or
// searching compressed data for local-header signatures.
type zipLocalHeaders struct {
	source    io.ReaderAt
	directory *io.SectionReader
	base      int64
}

func newZIPLocalHeaders(source io.ReaderAt, size int64) (*zipLocalHeaders, error) {
	directory, err := readZIPDirectoryEnd(source, size)
	if err != nil {
		return nil, err
	}

	if directory.size > uint64(directory.endOffset) || directory.offset > math.MaxInt64 {
		return nil, zip.ErrFormat
	}

	base := directory.endOffset - int64(directory.size) - int64(directory.offset)
	// Match archive/zip's handling of prefixed archives and inaccurate directory
	// sizes: prefer the declared offset if it already points to a central header.
	var signature [4]byte
	if base > 0 {
		if _, err := source.ReadAt(signature[:], int64(directory.offset)); err == nil && binary.LittleEndian.Uint32(signature[:]) == zipCentralHeaderSignature {
			base = 0
		}
	}

	if base < 0 || base > size-int64(directory.offset) {
		return nil, zip.ErrFormat
	}

	start := base + int64(directory.offset)

	return &zipLocalHeaders{
		source:    source,
		directory: io.NewSectionReader(source, start, size-start),
		base:      base,
	}, nil
}

type zipDirectoryEnd struct {
	offset    uint64
	size      uint64
	endOffset int64
}

func readZIPDirectoryEnd(source io.ReaderAt, size int64) (zipDirectoryEnd, error) {
	// The end record can be followed by a comment of up to 65535 bytes.
	tail := make([]byte, min(size, zipDirectoryEndLen+math.MaxUint16))
	if _, err := source.ReadAt(tail, size-int64(len(tail))); err != nil {
		return zipDirectoryEnd{}, fmt.Errorf("failed to read zip trailer: %w", err)
	}

	for i := len(tail) - zipDirectoryEndLen; i >= 0; i-- {
		if binary.LittleEndian.Uint32(tail[i:]) != zipDirectoryEndSignature {
			continue
		}

		commentLen := int(binary.LittleEndian.Uint16(tail[i+20:]))
		if i+zipDirectoryEndLen+commentLen > len(tail) {
			continue
		}

		directory := zipDirectoryEnd{
			size:      uint64(binary.LittleEndian.Uint32(tail[i+12:])),
			offset:    uint64(binary.LittleEndian.Uint32(tail[i+16:])),
			endOffset: size - int64(len(tail)) + int64(i),
		}

		return readZIP64DirectoryEnd(source, size, directory)
	}

	return zipDirectoryEnd{}, zip.ErrFormat
}

func readZIP64DirectoryEnd(source io.ReaderAt, size int64, directory zipDirectoryEnd) (zipDirectoryEnd, error) {
	// A ZIP64 locator, when present, immediately precedes the ordinary end record.
	if directory.endOffset < zip64LocatorLen {
		return directory, nil
	}

	var locator [zip64LocatorLen]byte
	if _, err := source.ReadAt(locator[:], directory.endOffset-zip64LocatorLen); err != nil {
		return zipDirectoryEnd{}, fmt.Errorf("failed to read zip64 locator: %w", err)
	}

	if binary.LittleEndian.Uint32(locator[:]) != zip64LocatorSignature {
		return directory, nil
	}

	offset := binary.LittleEndian.Uint64(locator[8:])
	disk := binary.LittleEndian.Uint32(locator[4:])
	diskCount := binary.LittleEndian.Uint32(locator[16:])

	if offset > uint64(size) || disk != 0 || diskCount != 1 {
		return zipDirectoryEnd{}, zip.ErrFormat
	}

	var end [zip64DirectoryEndLen]byte
	if _, err := source.ReadAt(end[:], int64(offset)); err != nil {
		return zipDirectoryEnd{}, fmt.Errorf("failed to read zip64 trailer: %w", err)
	}

	if binary.LittleEndian.Uint32(end[:]) != zip64DirectoryEndSignature {
		return zipDirectoryEnd{}, zip.ErrFormat
	}

	return zipDirectoryEnd{
		size:      binary.LittleEndian.Uint64(end[40:]),
		offset:    binary.LittleEndian.Uint64(end[48:]),
		endOffset: int64(offset),
	}, nil
}

func (r *zipLocalHeaders) extra(file *zip.File) ([]byte, error) {
	var central [zipCentralHeaderLen]byte
	if _, err := io.ReadFull(r.directory, central[:]); err != nil {
		return nil, fmt.Errorf("failed to read central header: %w", err)
	}

	if binary.LittleEndian.Uint32(central[:]) != zipCentralHeaderSignature {
		return nil, zip.ErrFormat
	}

	nameLen := int(binary.LittleEndian.Uint16(central[28:]))
	extraLen := int(binary.LittleEndian.Uint16(central[30:]))
	commentLen := int(binary.LittleEndian.Uint16(central[32:]))

	metadata := make([]byte, nameLen+extraLen+commentLen)
	if _, err := io.ReadFull(r.directory, metadata); err != nil {
		return nil, fmt.Errorf("failed to read central metadata: %w", err)
	}

	if string(metadata[:nameLen]) != file.Name {
		return nil, zip.ErrFormat
	}

	offset, err := zipLocalHeaderOffset(central[:], metadata[nameLen:nameLen+extraLen])
	if err != nil {
		return nil, err
	}

	if offset > uint64(math.MaxInt64-r.base) {
		return nil, zip.ErrFormat
	}

	localOffset := r.base + int64(offset)

	var local [zipLocalHeaderLen]byte

	if _, err := r.source.ReadAt(local[:], localOffset); err != nil {
		return nil, fmt.Errorf("failed to read local header: %w", err)
	}

	if binary.LittleEndian.Uint32(local[:]) != zipLocalHeaderSignature {
		return nil, zip.ErrFormat
	}

	localNameLen := int(binary.LittleEndian.Uint16(local[26:]))
	localExtraLen := int(binary.LittleEndian.Uint16(local[28:]))

	dataOffset, err := file.DataOffset()
	if err != nil {
		return nil, fmt.Errorf("failed to locate entry data: %w", err)
	}

	if dataOffset-localOffset != int64(zipLocalHeaderLen+localNameLen+localExtraLen) {
		return nil, zip.ErrFormat
	}

	localMetadata := make([]byte, localNameLen+localExtraLen)
	if _, err := r.source.ReadAt(localMetadata, localOffset+zipLocalHeaderLen); err != nil {
		return nil, fmt.Errorf("failed to read local metadata: %w", err)
	}

	if !bytes.Equal(localMetadata[:localNameLen], metadata[:nameLen]) {
		return nil, zip.ErrFormat
	}

	return localMetadata[localNameLen:], nil
}

// ZIP64 central-directory offsets belong to the source archive. Let zip.Writer
// regenerate structural ZIP64 values for the output instead of leaving an old
// block ahead of the one it appends. Preserve all other extra bytes verbatim.
func zipExtraWithoutZIP64(extra []byte) []byte {
	result := make([]byte, 0, len(extra))

	for len(extra) >= 4 {
		length := int(binary.LittleEndian.Uint16(extra[2:])) + 4
		if length > len(extra) {
			break
		}

		if binary.LittleEndian.Uint16(extra) != zip64ExtraID {
			result = append(result, extra[:length]...)
		}

		extra = extra[length:]
	}

	return append(result, extra...)
}

// Stored ZIP64 entries have no descriptor after framing normalization, so their
// local ZIP64 sizes must be authoritative even if the source used placeholders.
func zipStoredExtra(header *zip.FileHeader, extra []byte) []byte {
	if header.Method != zip.Store || (header.CompressedSize64 < math.MaxUint32 && header.UncompressedSize64 < math.MaxUint32) {
		return extra
	}

	result := zipExtraWithoutZIP64(extra)

	var sizes [20]byte

	binary.LittleEndian.PutUint16(sizes[:], zip64ExtraID)
	binary.LittleEndian.PutUint16(sizes[2:], 16)
	binary.LittleEndian.PutUint64(sizes[4:], header.UncompressedSize64)
	binary.LittleEndian.PutUint64(sizes[12:], header.CompressedSize64)

	return append(result, sizes[:]...)
}

// ZIP64 values appear only for saturated fields, in this order:
// uncompressed size, compressed size, local-header offset, disk number.
func zipLocalHeaderOffset(central, extra []byte) (uint64, error) {
	offset := binary.LittleEndian.Uint32(central[42:])
	if offset != math.MaxUint32 {
		return uint64(offset), nil
	}

	skip := 0
	if binary.LittleEndian.Uint32(central[24:]) == math.MaxUint32 {
		skip += 8
	}

	if binary.LittleEndian.Uint32(central[20:]) == math.MaxUint32 {
		skip += 8
	}

	for len(extra) >= 4 {
		tag := binary.LittleEndian.Uint16(extra)

		length := int(binary.LittleEndian.Uint16(extra[2:]))
		if length > len(extra)-4 {
			return 0, zip.ErrFormat
		}

		if tag == zip64ExtraID {
			if length < skip+8 {
				return 0, zip.ErrFormat
			}

			return binary.LittleEndian.Uint64(extra[4+skip:]), nil
		}

		extra = extra[4+length:]
	}

	return 0, zip.ErrFormat
}
