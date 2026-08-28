package unicorn

import (
	"bytes"
	"errors"
	"fmt"
)

const windowsARM64TCGPatternCount = 16

var (
	tcgMaskFirstPattern  = []byte{0xe8, 0x21, 0xcc, 0x1a, 0xe8, 0x03, 0x28, 0x2a}
	tcgMaskSecondPattern = []byte{0xef, 0x21, 0xc8, 0x1a, 0xef, 0x03, 0x2f, 0x2a}
)

func patchWindowsARM64TCGMasks(image []byte) error {
	// Unicorn's AArch64 TCG backend was compiled with Windows' 32-bit unsigned
	// long, truncating two 64-bit masks. Flip only the register-width bit in the
	// checksum-pinned DLL and reject any layout other than the known build.
	firstOffsets := patternOffsets(image, tcgMaskFirstPattern)
	secondOffsets := patternOffsets(image, tcgMaskSecondPattern)

	if len(firstOffsets) != windowsARM64TCGPatternCount || len(secondOffsets) != len(firstOffsets) {
		return fmt.Errorf("unexpected AArch64 TCG mask layout (%d/%d matches)", len(firstOffsets), len(secondOffsets))
	}

	for index, first := range firstOffsets {
		second := secondOffsets[index]
		if second != first+24 {
			return errors.New("unexpected AArch64 TCG mask instruction spacing")
		}

		image[first+3] |= 0x80
		image[first+7] |= 0x80
		image[second+3] |= 0x80
		image[second+7] |= 0x80
	}

	return nil
}

func patternOffsets(data, pattern []byte) []int {
	var offsets []int

	for start := 0; start < len(data); {
		offset := bytes.Index(data[start:], pattern)
		if offset < 0 {
			break
		}

		offset += start
		offsets = append(offsets, offset)
		start = offset + len(pattern)
	}

	return offsets
}
