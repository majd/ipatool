package assets

import (
	"bytes"
	"encoding/binary"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("native remote image reader", func() {
	DescribeTable("parses complete Content-Range values",
		func(value string, expectedStart, expectedEnd, expectedSize int64) {
			start, end, size, err := parseContentRange(value)
			Expect(err).NotTo(HaveOccurred())
			Expect(start).To(Equal(expectedStart))
			Expect(end).To(Equal(expectedEnd))
			Expect(size).To(Equal(expectedSize))
		},
		Entry("single byte", "bytes 0-0/26637307067", int64(0), int64(0), int64(26637307067)),
		Entry("middle range", "bytes 100-199/1000", int64(100), int64(199), int64(1000)),
	)

	DescribeTable("rejects incomplete Content-Range values",
		func(value string) {
			_, _, _, err := parseContentRange(value)
			Expect(err).To(HaveOccurred())
		},
		Entry("missing total", "bytes 0-0/*"),
		Entry("missing separator", "bytes 0-0"),
		Entry("zero length", "bytes 0-0/0"),
		Entry("wrong unit", "items 0-0/1"),
		Entry("range beyond total", "bytes 0-1/1"),
		Entry("backwards range", "bytes 2-1/3"),
	)

	It("parses bounded AEA metadata records", func() {
		var data bytes.Buffer
		for key, value := range map[string]string{"first": "value", "second": "another"} {
			record := append(append([]byte(key), 0), value...)
			Expect(binary.Write(&data, binary.LittleEndian, uint32(len(record)+4))).To(Succeed())
			_, err := data.Write(record)
			Expect(err).NotTo(HaveOccurred())
		}

		metadata, err := parseAEAMetadata(data.Bytes())
		Expect(err).NotTo(HaveOccurred())
		Expect(metadata).To(HaveKeyWithValue("first", []byte("value")))
		Expect(metadata).To(HaveKeyWithValue("second", []byte("another")))
	})

	It("rejects an AEA metadata record outside the authentication block", func() {
		data := make([]byte, 8)
		binary.LittleEndian.PutUint32(data, 32)
		_, err := parseAEAMetadata(data)
		Expect(err).To(MatchError(ContainSubstring("invalid AEA metadata record length")))
	})
})
