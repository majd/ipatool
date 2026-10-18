package assets

import (
	"encoding/binary"

	"github.com/go-compressions/lzfse"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Short LZFSE entropy streams", func() {
	It("decodes a valid short literal payload without changing its output", func() {
		original := make([]byte, 65536)
		encoded, err := lzfse.Compress(original)
		Expect(err).NotTo(HaveOccurred())
		Expect(binary.LittleEndian.Uint32(encoded)).To(Equal(uint32(0x32787662)))
		word := binary.LittleEndian.Uint64(encoded[8:])
		Expect(word >> 20 & 0xfffff).To(BeZero())
		header := int(binary.LittleEndian.Uint32(encoded[24:]))
		short := append([]byte(nil), encoded[:header]...)
		short = append(short, make([]byte, 4)...)
		short = append(short, encoded[header:]...)
		binary.LittleEndian.PutUint64(short[8:], word|4<<20)
		_, err = lzfse.Decompress(short)
		Expect(err).To(MatchError(ContainSubstring("stream too short")))
		padded, err := padLZFSEStreams(short)
		Expect(err).NotTo(HaveOccurred())
		decoded, err := lzfse.Decompress(padded)
		Expect(err).NotTo(HaveOccurred())
		Expect(decoded).To(Equal(original))
	})
	It("rejects truncated block payloads", func() { _, err := padLZFSEStreams([]byte{'b', 'v', 'x', '2'}); Expect(err).To(HaveOccurred()) })
})
