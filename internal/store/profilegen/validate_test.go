package profilegen

import (
	"encoding/binary"

	profileformat "github.com/majd/ipatool/v2/internal/store/profile/format"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Native profile comparison", func() {
	It("compares witness methods independently of their table addresses", func() {
		data := make([]byte, 4096)
		put := func(a, value uint64) { binary.LittleEndian.PutUint64(data[a-4096:], value) }
		put(0x1100, 0x100) // Conformance points to the protocol at 0x1200.
		put(0x1210, 1)     // One protocol requirement.
		put(0x1218, 1)     // Method requirement.
		put(0x1300, 0x1100)
		put(0x1308, 0x5000)
		put(0x1400, 0x1100)
		put(0x1408, 0x5000)
		p := &profileformat.Profile{Pages: []profileformat.Page{{Address: 4096}}}
		a, b := memory(p, data, nil), memory(p, append([]byte(nil), data...), nil)
		Expect(equalWitness(a, b, 0x1300, 0x1400, map[[2]uint64]bool{})).To(BeTrue())
		binary.LittleEndian.PutUint64(b.pages[0x408:], 0x5008)
		Expect(equalWitness(a, b, 0x1300, 0x1400, map[[2]uint64]bool{})).To(BeFalse())
	})
	It("distinguishes missing dictionary keys from zero-valued entries", func() {
		a := profileformat.Constant{Kind: "NSDictionary", Entries: map[string]uint64{"a": 0}}
		b := profileformat.Constant{Kind: "NSDictionary", Entries: map[string]uint64{"b": 0}}
		Expect(equalConstant(a, b)).To(BeFalse())
	})
})
