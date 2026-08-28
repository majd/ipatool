package unicorn

import (
	"bytes"
	"testing"
)

func TestPatchWindowsARM64TCGMasks(t *testing.T) {
	image := make([]byte, 0, windowsARM64TCGPatternCount*33)
	for range windowsARM64TCGPatternCount {
		image = append(image, tcgMaskFirstPattern...)
		image = append(image, make([]byte, 16)...)
		image = append(image, tcgMaskSecondPattern...)
		image = append(image, 0xff)
	}

	if err := patchWindowsARM64TCGMasks(image); err != nil {
		t.Fatalf("patchWindowsARM64TCGMasks: %v", err)
	}

	if matches := patternOffsets(image, tcgMaskFirstPattern); len(matches) != 0 {
		t.Fatalf("first unpatched pattern count = %d, want 0", len(matches))
	}

	if matches := patternOffsets(image, tcgMaskSecondPattern); len(matches) != 0 {
		t.Fatalf("second unpatched pattern count = %d, want 0", len(matches))
	}
}

func TestPatchWindowsARM64TCGMasksRejectsUnexpectedImage(t *testing.T) {
	image := bytes.Join([][]byte{tcgMaskFirstPattern, tcgMaskSecondPattern}, make([]byte, 16))
	if err := patchWindowsARM64TCGMasks(image); err == nil {
		t.Fatal("patchWindowsARM64TCGMasks unexpectedly succeeded")
	}
}
