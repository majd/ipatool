package profilegen

import (
	"context"
	"encoding/binary"

	"github.com/majd/ipatool/v2/internal/sap/unicorn"
	profileformat "github.com/majd/ipatool/v2/internal/store/profile/format"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Swift bootstrap discovery", func() {
	DescribeTable("measures symbolic mangled names without treating payload zeros as terminators", func(data []byte, length int) {
		n, err := mangledNameLength(data)
		Expect(err).NotTo(HaveOccurred())
		Expect(n).To(Equal(length))
	}, Entry("plain", []byte{'S', 'S', 0}, 2), Entry("relative", []byte{2, 0, 0, 0, 0, 'y', 'S', 'S', 'G', 0}, 9), Entry("absolute", []byte{0x18, 0, 0, 0, 0, 0, 0, 0, 0, 'G', 0}, 10))
	DescribeTable("rejects malformed symbolic names", func(data []byte) { _, err := mangledNameLength(data); Expect(err).To(HaveOccurred()) }, Entry("missing terminator", []byte{'S', 'S'}), Entry("short relative", []byte{1, 0, 0}), Entry("short absolute", []byte{0x18, 0, 0, 0, 0}))
	It("discovers relocated relative records and excludes invalid lengths and interior references", func() {
		for _, base := range []uint64{0x2000, 0x140000000} {
			data := make([]byte, 32)
			binary.LittleEndian.PutUint32(data, 0xfffffff0)
			binary.LittleEndian.PutUint32(data[4:], 9)
			binary.LittleEndian.PutUint32(data[8:], 0x100)
			binary.LittleEndian.PutUint32(data[12:], 3)
			binary.LittleEndian.PutUint32(data[16:], 0xffffffe0)
			binary.LittleEndian.PutUint32(data[20:], 8)
			binary.LittleEndian.PutUint32(data[24:], 0xffffffda)
			binary.LittleEndian.PutUint32(data[28:], 7)
			names := map[uint64]uint64{base - 16: 9, base + 0x108: 3}
			Expect(concreteTypeRecords(base, data, names)).To(Equal([]typeRequest{{base - 16, 9}, {base + 0x108, 3}}))
		}
	})
	It("finds coalesced GOT references only when the address register matches", func() {
		a, ok := referencedPointer(0x90000001, 0xf9402420, 0x2000)
		Expect(ok).To(BeTrue())
		Expect(a).To(Equal(uint64(0x2048)))
		_, ok = referencedPointer(0x90000002, 0xf9402420, 0x2000)
		Expect(ok).To(BeFalse())
		_, ok = referencedPointer(0x90000001, 0xf9002420, 0x2000)
		Expect(ok).To(BeFalse())
	})
})

var _ = Describe("Observed Swift metadata", func() {
	It("keeps generic witness instantiations separate and records only complete foreign metadata", func() {
		engine, err := unicorn.New(context.Background())
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(engine.Close)
		Expect(engine.MemMap(bootHeap, 4096)).To(Succeed())
		v := &bootstrapVM{engine: engine, metadata: newMetadataProfile(), allocations: map[uint64]uint64{}, callbacks: map[uint64]func(){}}
		for _, m := range []uint64{100, 200} {
			v.set(0, 10)
			v.set(1, m)
			v.writeRegister(unicorn.RegLR, bootReturn)
			v.observeMetadataCall("swift_getWitnessTable")
			link, err := engine.RegRead(unicorn.RegLR)
			Expect(err).NotTo(HaveOccurred())
			Expect(link).To(Equal(bootReturn))
			v.set(0, m+1000)
			v.observeMetadataReturn(bootReturn)
		}
		Expect(v.metadata.SwiftWitnesses[profileformat.WitnessKey(10, 100)]).To(Equal(uint64(1100)))
		Expect(v.metadata.SwiftWitnesses[profileformat.WitnessKey(10, 200)]).To(Equal(uint64(1200)))
		for _, state := range []uint64{1, 0} {
			v.set(1, 42)
			v.observeMetadataCall("swift_getForeignTypeMetadata")
			link, err := engine.RegRead(unicorn.RegLR)
			Expect(err).NotTo(HaveOccurred())
			Expect(link).To(Equal(bootReturn))
			v.set(0, 900)
			v.set(1, state)
			v.observeMetadataReturn(bootReturn)
			if state != 0 {
				Expect(v.metadata.SwiftForeign).To(BeEmpty())
			}
		}
		Expect(v.metadata.SwiftForeign).To(HaveKeyWithValue("42", uint64(900)))
	})
})

var _ = Describe("Swift metadata allocation", func() {
	DescribeTable("uses the size argument after alignment for zone allocations with options", func(name string) {
		engine, err := unicorn.New(context.Background())
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(engine.Close)
		v := &bootstrapVM{engine: engine, allocations: map[uint64]uint64{}, cursor: 16}
		v.set(0, 0)
		v.set(1, 4096)
		v.set(2, 16384)
		v.set(3, 2)
		v.set(4, 2)
		Expect(v.host(name)).To(BeTrue())
		address := v.arg(0)
		Expect(address % 4096).To(BeZero())
		Expect(v.allocations[address]).To(Equal(uint64(16384)))
		Expect(v.alloc(16)).To(BeNumerically(">=", address+16384))
	}, Entry("typed", "malloc_type_zone_malloc_with_options"), Entry("untyped", "malloc_zone_malloc_with_options"))
})
