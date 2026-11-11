package machimage

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/blacktop/go-macho/types"
)

// ReadARM64 consumes exactly size bytes, retaining only the arm64e image of a
// universal binary. A caller can hash the entire input with an io.TeeReader.
func ReadARM64(input io.Reader, size int) ([]byte, error) {
	if size < 0 {
		return nil, errors.New("invalid image size")
	}

	var header [8]byte

	prefix := min(size, 4)
	if _, err := io.ReadFull(input, header[:prefix]); err != nil {
		return nil, fmt.Errorf("read image magic: %w", err)
	}

	if prefix < 4 || binary.BigEndian.Uint32(header[:]) != types.MagicFat.Int() {
		data := make([]byte, size)
		copy(data, header[:prefix])

		if _, err := io.ReadFull(input, data[prefix:]); err != nil {
			return nil, fmt.Errorf("read thin image: %w", err)
		}

		return data, nil
	}

	if size < 8 {
		return nil, errors.New("truncated universal binary header")
	}

	if _, err := io.ReadFull(input, header[4:]); err != nil {
		return nil, fmt.Errorf("read universal binary header: %w", err)
	}

	count := uint64(binary.BigEndian.Uint32(header[4:]))
	if count == 0 || count > uint64(size-8)/20 {
		return nil, errors.New("invalid universal binary architecture count")
	}

	var offset, length uint64

	var arch [20]byte
	for range count {
		if _, err := io.ReadFull(input, arch[:]); err != nil {
			return nil, fmt.Errorf("read universal binary architecture: %w", err)
		}

		cpu := types.CPU(binary.BigEndian.Uint32(arch[:]))
		subtype := types.CPUSubtype(binary.BigEndian.Uint32(arch[4:]))

		if length == 0 && cpu == types.CPUArm64 && subtype&types.CpuSubtypeMask == types.CPUSubtypeArm64E {
			offset = uint64(binary.BigEndian.Uint32(arch[8:]))
			length = uint64(binary.BigEndian.Uint32(arch[12:]))

			if length == 0 || offset < 8+count*20 || offset+length > uint64(size) {
				return nil, errors.New("arm64e slice exceeds input size or overlaps universal binary header")
			}
		}
	}

	if length == 0 {
		return nil, errors.New("universal binary has no arm64e slice")
	}

	buffer := make([]byte, 256<<10)
	if err := discardImageBytes(input, offset-(8+count*20), buffer); err != nil {
		return nil, fmt.Errorf("read universal binary prefix: %w", err)
	}

	data := make([]byte, int(length))
	if _, err := io.ReadFull(input, data); err != nil {
		return nil, fmt.Errorf("read arm64e image: %w", err)
	}

	if err := discardImageBytes(input, uint64(size)-offset-length, buffer); err != nil {
		return nil, fmt.Errorf("read universal binary suffix: %w", err)
	}

	return data, nil
}

func discardImageBytes(input io.Reader, count uint64, buffer []byte) error {
	for count > 0 {
		size := min(count, uint64(len(buffer)))
		if _, err := io.ReadFull(input, buffer[:size]); err != nil {
			return fmt.Errorf("consume image bytes: %w", err)
		}

		count -= size
	}

	return nil
}
