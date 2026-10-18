package profilegen

import (
	"context"
	"testing"

	"github.com/majd/ipatool/v2/internal/sap/unicorn"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestProfileGeneration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Store Profile Generation Suite")
}

var _ = Describe("Bootstrap binary decoding", func() {
	It("decodes negative ADRP page displacements", func() { Expect(adrp(0xf0ffffe0, 0x2001234)).To(Equal(uint64(0x2000000))) })
	It("preserves ordinary instructions and lowers RCpc loads", func() {
		Expect(bootstrapInstruction(0xf8bfc115)).To(Equal(uint32(0xf9400115)))
		Expect(bootstrapInstruction(0xd503201f)).To(Equal(uint32(0xd503201f)))
	})
	It("rejects overflowing guest heap allocations", func() {
		v := &bootstrapVM{allocations: map[uint64]uint64{}}
		Expect(func() { v.alloc(^uint64(0)) }).To(Panic())
		v.cursor = bootHeapSize + 1
		Expect(func() { v.alloc(16) }).To(Panic())
	})
	It("looks up sorted noncontiguous image ranges", func() {
		ranges := [][2]uint64{{8, 16}, {24, 40}}
		Expect(inside(7, ranges)).To(BeFalse())
		Expect(inside(8, ranges)).To(BeTrue())
		Expect(inside(16, ranges)).To(BeFalse())
		Expect(inside(39, ranges)).To(BeTrue())
		Expect(inside(40, ranges)).To(BeFalse())
	})
})

var _ = Describe("Single-threaded bootstrap atomics", func() {
	var v *bootstrapVM
	BeforeEach(func() {
		engine, err := unicorn.New(context.Background())
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(engine.Close)
		Expect(engine.MemMap(bootHeap, 4096)).To(Succeed())
		v = &bootstrapVM{engine: engine}
	})
	It("performs compare-and-swap with the observed value on failure", func() {
		v.put(bootHeap, 41)
		v.setReg(3, bootHeap)
		v.setReg(1, 41)
		v.setReg(2, 99)
		v.atomic(0xc8e1fc62) // casal x1, x2, [x3]
		Expect(v.u64(bootHeap)).To(Equal(uint64(99)))
		Expect(v.reg(1)).To(Equal(uint64(41)))
		v.setReg(1, 42)
		v.setReg(2, 100)
		v.atomic(0xc8e1fc62)
		Expect(v.u64(bootHeap)).To(Equal(uint64(99)))
		Expect(v.reg(1)).To(Equal(uint64(99)))
	})
	It("updates both words of an ARM64 CASP atomically", func() {
		v.put(bootHeap, 11)
		v.put(bootHeap+8, 12)
		v.setReg(4, bootHeap)
		v.setReg(0, 11)
		v.setReg(1, 12)
		v.setReg(2, 21)
		v.setReg(3, 22)
		v.atomic(0x4860fc82) // caspal x0, x1, x2, x3, [x4]
		Expect(v.u64(bootHeap)).To(Equal(uint64(21)))
		Expect(v.u64(bootHeap + 8)).To(Equal(uint64(22)))
		Expect(v.reg(0)).To(Equal(uint64(11)))
		Expect(v.reg(1)).To(Equal(uint64(12)))
	})
	It("wraps a 32-bit atomic add without overwriting adjacent memory", func() {
		v.put(bootHeap, 0xabcdef01ffffffff)
		v.setReg(19, bootHeap)
		v.setReg(8, 2)
		v.atomic(0xb8a80268) // ldadda w8, w8, [x19]
		Expect(v.u64(bootHeap)).To(Equal(uint64(0xabcdef0100000001)))
		Expect(v.reg(8)).To(Equal(uint64(0xffffffff)))
	})
})
