package emulator

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Store memory grouping", func() {
	It("groups nearby unsorted pages and tolerates duplicates", func() {
		regions, err := groupPages([]uint64{0x3000, 0x1000, 0x1000, 0x800000})
		Expect(err).NotTo(HaveOccurred())
		Expect(regions).To(Equal([]memoryRegion{{0x1000, 0x3000}, {0x800000, 4096}}))
	})
	It("bounds groups even when many nearby pages form a long chain", func() {
		var pages []uint64
		for address := uint64(0); address < 3*maxRegionSize; address += maxRegionGap {
			pages = append(pages, address)
		}
		regions, err := groupPages(pages)
		Expect(err).NotTo(HaveOccurred())
		for _, region := range regions {
			Expect(region.size).To(BeNumerically("<=", maxRegionSize))
		}
		for _, address := range pages {
			covered := false
			for _, region := range regions {
				covered = covered || address >= region.address && address+profilePageSize <= region.address+region.size
			}
			Expect(covered).To(BeTrue())
		}
	})
	It("rejects excessive total mapped memory", func() {
		var pages []uint64
		for address := uint64(0); address < 2*maxProfileSpan; address += maxRegionGap {
			pages = append(pages, address)
		}
		_, err := groupPages(pages)
		Expect(err).To(MatchError(ContainSubstring("size limit")))
	})
	It("rejects unaligned and overflowing pages", func() {
		for _, address := range []uint64{1, ^uint64(0) - 4095} {
			_, err := groupPages([]uint64{address})
			Expect(err).To(MatchError(ContainSubstring("page address")))
		}
	})
	It("still rejects execution in padding outside the pinned code", func() {
		guest := &Runtime{codeRanges: [][2]uint64{{0x1000, 0x2000}}}
		guest.step(0x3000, 4)
		Expect(guest.fault).To(MatchError(ContainSubstring("execution outside the pinned image")))
	})
})

var _ = Describe("Profile dependency checks", func() {
	It("checks the complete range rather than just the first or last page", func() {
		allowed := map[uint64]bool{0x1000: true, 0x3000: true}
		Expect(checkProfileAccess(allowed, 0x1ffc, 8)).To(MatchError(ContainSubstring("missing Store profile page 0x2000")))
		Expect(checkProfileAccess(allowed, 0x1000, 0x2001)).To(MatchError(ContainSubstring("missing Store profile page 0x2000")))
		Expect(checkProfileAccess(allowed, 0x1000, 4096)).To(Succeed())
		Expect(checkProfileAccess(allowed, 0x3000, 4096)).To(Succeed())
	})
	It("handles empty accesses and rejects wrapping ranges", func() {
		Expect(checkProfileAccess(nil, ^uint64(0), 0)).To(Succeed())
		Expect(checkProfileAccess(nil, ^uint64(0), 2)).To(MatchError(ContainSubstring("overflows")))
		Expect(checkProfileAccess(map[uint64]bool{^uint64(4095): true}, ^uint64(0), 1)).To(Succeed())
	})
})
