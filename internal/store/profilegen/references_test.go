package profilegen

import (
	"encoding/binary"
	"sort"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// referencePages collects page addresses from synthetic instructions so boundary
// and fallback behavior is checked independently of the pinned framework build.
func referencePages(words ...uint32) []uint64 {
	code := make([]byte, len(words)*4)
	for i, word := range words {
		binary.LittleEndian.PutUint32(code[i*4:], word)
	}

	pages := map[uint64]bool{}

	dataReferences(code, 0x100000, func(address uint64) { pages[address&^4095] = true })

	result := make([]uint64, 0, len(pages))
	for page := range pages {
		result = append(result, page)
	}

	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })

	return result
}

var _ = Describe("Store data references", func() {
	It("retains the addressed page for an ADRP and ADD pair", func() {
		Expect(referencePages(0x90000000, 0x91040000)).To(Equal([]uint64{0x100000})) // add x0, x0, #256
	})
	It("tracks addresses across independent instructions", func() {
		Expect(referencePages(0x90000000, 0xd2800021, 0xd503201f, 0x91040000)).To(Equal([]uint64{0x100000})) // mov x1, #1; nop; add x0, x0, #256
	})
	It("includes scaled load offsets beyond the ADRP page", func() {
		Expect(referencePages(0x90000000, 0xf95ffc00)).To(Equal([]uint64{0x100000, 0x103000})) // ldr x0, [x0, #16376]
	})
	It("includes shifted immediate address arithmetic", func() {
		Expect(referencePages(0x90000000, 0x91400800)).To(Equal([]uint64{0x100000, 0x102000})) // add x0, x0, #2, lsl #12
	})
	It("includes both pages of an unaligned load", func() {
		Expect(referencePages(0x90000000, 0x913ff000, 0xf9400000)).To(Equal([]uint64{0x100000, 0x101000})) // add x0, x0, #4092; ldr x0, [x0]
	})
	It("sign extends unscaled negative load offsets", func() {
		Expect(referencePages(0x90000000, 0xf85f8000)).To(Equal([]uint64{0xff000, 0x100000})) // ldur x0, [x0, #-8]
	})
	It("retains the full conservative range for an unknown consumer", func() {
		pages := referencePages(0x90000000, 0x8b010000)
		Expect(pages).To(HaveLen(16))
		Expect(pages[0]).To(Equal(uint64(0x100000)))
		Expect(pages[15]).To(Equal(uint64(0x10f000)))
	})
	It("keeps a conservative range for addresses still live at a branch after a known access", func() {
		Expect(referencePages(0x90000000, 0xf9400001, 0x14000001)).To(HaveLen(16)) // ldr x1, [x0]; b next
	})
	It("does not propagate register values across branches", func() {
		Expect(referencePages(0x90000000, 0x14000001, 0xf97ffc00)).To(HaveLen(16))
	})
})
