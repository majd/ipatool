package machimage

import (
	"math"
	"strings"
	"testing"

	"github.com/blacktop/go-macho/types"
)

func TestValidateSegments(t *testing.T) {
	tests := []struct {
		name    string
		dataLen int
		segment segment
		wantErr string
	}{
		{
			name:    "valid",
			dataLen: 32,
			segment: segment{name: "__DATA", address: 0x1000, size: 16, fileOff: 8, fileSize: 16},
		},
		{
			name:    "file data exceeds memory",
			dataLen: 32,
			segment: segment{name: "__DATA", address: 0x1000, size: 8, fileOff: 8, fileSize: 16},
			wantErr: "file data exceeds its memory size",
		},
		{
			name:    "file data exceeds image",
			dataLen: 16,
			segment: segment{name: "__DATA", address: 0x1000, size: 16, fileOff: 8, fileSize: 16},
			wantErr: "data exceeds",
		},
		{
			name:    "file range overflows",
			dataLen: 32,
			segment: segment{name: "__DATA", address: 0x1000, size: 16, fileOff: math.MaxUint64 - 7, fileSize: 16},
			wantErr: "data exceeds",
		},
		{
			name:    "address range overflows",
			dataLen: 32,
			segment: segment{name: "__DATA", address: math.MaxUint64 - 7, size: 16},
			wantErr: "address range overflows",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			image := Image{name: "test", data: make([]byte, test.dataLen), segments: []segment{test.segment}}

			err := image.validateSegments()
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("validateSegments() error = %v", err)
				}

				return
			}

			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("validateSegments() error = %v, want error containing %q", err, test.wantErr)
			}
		})
	}
}

func TestSegmentFileOffsetBounds(t *testing.T) {
	image := Image{
		name: "test",
		data: make([]byte, 64),
		segments: []segment{
			{name: "__DATA", address: 0x1000, size: 24, fileOff: 16, fileSize: 16},
			{name: "__SHORT", address: 0x2000, size: 16, fileOff: 56, fileSize: 16},
		},
	}

	tests := []struct {
		name    string
		segment string
		offset  uint64
		want    uint64
		wantErr string
	}{
		{name: "valid at end", segment: "__DATA", offset: 8, want: 24},
		{name: "past file data", segment: "__DATA", offset: 9, wantErr: "exceeds file data"},
		{name: "past memory", segment: "__DATA", offset: 17, wantErr: "exceeds segment"},
		{name: "past image", segment: "__SHORT", offset: 8, wantErr: "exceeds test"},
		{name: "offset overflow", segment: "__DATA", offset: math.MaxUint64 - 3, wantErr: "exceeds segment"},
		{name: "unknown segment", segment: "__MISSING", wantErr: "unknown segment"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := image.segmentFileOffset(test.segment, test.offset, pointerSize)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("segmentFileOffset() error = %v", err)
				}

				if got != test.want {
					t.Fatalf("segmentFileOffset() = %#x, want %#x", got, test.want)
				}

				return
			}

			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("segmentFileOffset() error = %v, want error containing %q", err, test.wantErr)
			}
		})
	}
}

func TestRelocateRejectsFixupsOutsideTheirSegments(t *testing.T) {
	tests := []struct {
		name    string
		binds   []types.Bind
		rebases []types.Rebase
	}{
		{
			name:  "bind",
			binds: []types.Bind{{Name: "symbol", Segment: "__DATA", SegOffset: 9}},
		},
		{
			name:    "rebase",
			rebases: []types.Rebase{{Type: types.REBASE_TYPE_POINTER, Segment: "__DATA", Offset: 9, Value: 0x1000}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			image := Image{
				name:     "test",
				data:     make([]byte, 64),
				base:     0x1000,
				segments: []segment{{name: "__DATA", address: 0x1000, size: 32, fileOff: 16, fileSize: 16}},
				binds:    test.binds,
				rebases:  test.rebases,
			}

			err := image.Relocate(0x2000, func(string) (uint64, error) { return 0x3000, nil })
			if err == nil || !strings.Contains(err.Error(), "exceeds file data") {
				t.Fatalf("Relocate() error = %v, want file-data bounds error", err)
			}
		})
	}
}
