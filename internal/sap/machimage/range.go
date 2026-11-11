package machimage

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/blacktop/go-macho/types"
)

// ARM64Range locates a thin image or the arm64e slice using only its headers.
// It leaves unselected architectures unread for range-backed remote sources.
func ARM64Range(source io.ReaderAt, size int64) (int64, int64, error) {
	if size < 0 {
		return 0, 0, errors.New("invalid image size")
	}

	var header [8]byte

	if size < 4 {
		return 0, size, nil
	}

	if _, err := source.ReadAt(header[:4], 0); err != nil {
		return 0, 0, fmt.Errorf("read image magic: %w", err)
	}

	if binary.BigEndian.Uint32(header[:]) != types.MagicFat.Int() {
		return 0, size, nil
	}

	if size < 8 {
		return 0, 0, errors.New("truncated universal binary header")
	}

	if _, err := source.ReadAt(header[4:], 4); err != nil {
		return 0, 0, fmt.Errorf("read universal binary header: %w", err)
	}

	count := int64(binary.BigEndian.Uint32(header[4:]))
	if count == 0 || count > (size-8)/20 {
		return 0, 0, errors.New("invalid universal binary architecture count")
	}

	var architecture [20]byte
	for index := int64(0); index < count; index++ {
		if _, err := source.ReadAt(architecture[:], 8+index*20); err != nil {
			return 0, 0, fmt.Errorf("read universal binary architecture: %w", err)
		}

		cpu := types.CPU(binary.BigEndian.Uint32(architecture[:]))
		subtype := types.CPUSubtype(binary.BigEndian.Uint32(architecture[4:]))

		if cpu != types.CPUArm64 || subtype&types.CpuSubtypeMask != types.CPUSubtypeArm64E {
			continue
		}

		offset, length := int64(binary.BigEndian.Uint32(architecture[8:])), int64(binary.BigEndian.Uint32(architecture[12:]))
		if length == 0 || offset < 8+count*20 || offset > size || length > size-offset {
			return 0, 0, errors.New("arm64e slice exceeds input size or overlaps universal binary header")
		}

		return offset, length, nil
	}

	return 0, 0, errors.New("universal binary has no arm64e slice")
}
