package emulator

import (
	"errors"
	"fmt"
	"slices"
)

const (
	profilePageSize = uint64(4096)
	maxRegionGap    = uint64(1 << 20)
	maxRegionSize   = uint64(32 << 20)
	maxProfileSpan  = uint64(1 << 30)
)

type memoryRegion struct{ address, size uint64 }

// Group nearby pages to avoid rebuilding Unicorn's memory topology thousands of
// times during both mapping and teardown. Bound individual and total spans so a
// chain of small gaps cannot silently reserve an arbitrarily large address range.
// Executable addresses are still checked against the pinned code ranges by step.
func groupPages(addresses []uint64) ([]memoryRegion, error) {
	slices.Sort(addresses)

	for _, address := range addresses {
		if address%profilePageSize != 0 || address > ^uint64(0)-profilePageSize {
			return nil, errors.New("invalid Store profile page address")
		}
	}

	var regions []memoryRegion

	var total uint64

	for start := 0; start < len(addresses); {
		end := start + 1
		for end < len(addresses) && addresses[end]-addresses[end-1] <= maxRegionGap && addresses[end]-addresses[start]+profilePageSize <= maxRegionSize {
			end++
		}

		size := addresses[end-1] - addresses[start] + profilePageSize
		if size > maxProfileSpan-total {
			return nil, errors.New("store profile mapping exceeds size limit")
		}

		total += size
		regions = append(regions, memoryRegion{address: addresses[start], size: size})
		start = end
	}

	return regions, nil
}

// checkProfileAccess checks every touched page, including accesses that start in
// a valid page and cross a gap. Grouping padding is never a valid dependency.
func checkProfileAccess(allowed map[uint64]bool, address, size uint64) error {
	if size == 0 {
		return nil
	}

	if size-1 > ^uint64(0)-address {
		return errors.New("store memory access overflows address space")
	}

	last := (address + size - 1) &^ (profilePageSize - 1)

	for page := address &^ (profilePageSize - 1); ; page += profilePageSize {
		if !allowed[page] {
			return fmt.Errorf("missing Store profile page %#x for access %#x (%d bytes)", page, address, size)
		}

		if page == last {
			return nil
		}
	}
}
