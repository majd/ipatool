package machimage

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/blacktop/go-macho/types"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestImageGinkgo(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Mach-O Image Suite")
}

func universalImage() []byte {
	data := make([]byte, 56)
	for index, value := range []uint32{
		types.MagicFat.Int(), 2,
		uint32(types.CPUAmd64), 3, 48, 4, 0,
		uint32(types.CPUArm64), uint32(types.CPUSubtypeArm64E), 52, 4, 0,
	} {
		binary.BigEndian.PutUint32(data[index*4:], value)
	}

	copy(data[48:], []byte("x86!arm!"))

	return data
}

var _ = Describe("ARM64 image selection", func() {
	It("selects arm64e without parsing unused architectures", func() {
		input := universalImage()
		data, err := ARM64Slice(input)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(Equal("arm!"))
		Expect(&data[0]).To(BeIdenticalTo(&input[52]))
	})
	It("preserves thin image data", func() {
		input := []byte("thin image")
		data, err := ARM64Slice(input)
		Expect(err).NotTo(HaveOccurred())
		Expect(&data[0]).To(BeIdenticalTo(&input[0]))
	})
	It("isolates mutable relocation data from the source bundle", func() {
		for _, input := range [][]byte{universalImage(), []byte("thin image")} {
			original := bytes.Clone(input)
			data, err := arm64Slice(input)
			Expect(err).NotTo(HaveOccurred())
			data[0] ^= 0xff
			Expect(input).To(Equal(original))
		}
	})
	DescribeTable("rejects malformed universal images", func(mutate func([]byte) []byte, message string) {
		_, err := ARM64Slice(mutate(universalImage()))
		Expect(err).To(MatchError(ContainSubstring(message)))
	},
		Entry("truncated header", func(b []byte) []byte { return b[:4] }, "header"),
		Entry("truncated architecture table", func(b []byte) []byte { return b[:47] }, "architecture count"),
		Entry("no architectures", func(b []byte) []byte {
			binary.BigEndian.PutUint32(b[4:], 0)

			return b
		}, "architecture count"),
		Entry("excessive count", func(b []byte) []byte {
			binary.BigEndian.PutUint32(b[4:], ^uint32(0))

			return b
		}, "architecture count"),
		Entry("no arm64e image", func(b []byte) []byte {
			binary.BigEndian.PutUint32(b[32:], 0)

			return b
		}, "no arm64e slice"),
		Entry("slice past the end", func(b []byte) []byte {
			binary.BigEndian.PutUint32(b[40:], 5)

			return b
		}, "exceeds input size"),
		Entry("empty slice", func(b []byte) []byte {
			binary.BigEndian.PutUint32(b[40:], 0)

			return b
		}, "exceeds input size"),
		Entry("overlapping header", func(b []byte) []byte {
			binary.BigEndian.PutUint32(b[36:], 8)

			return b
		}, "overlaps"),
		Entry("large offset", func(b []byte) []byte {
			binary.BigEndian.PutUint32(b[36:], ^uint32(0))

			return b
		}, "exceeds input size"),
	)
})
