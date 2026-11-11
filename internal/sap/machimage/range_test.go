package machimage

import (
	"bytes"
	"encoding/binary"
	"io"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type headerReader struct {
	input []byte
	read  int
	end   int64
}

func (r *headerReader) ReadAt(data []byte, offset int64) (int, error) {
	r.read += len(data)
	r.end = max(r.end, offset+int64(len(data)))

	return bytes.NewReader(r.input).ReadAt(data, offset) //nolint:wrapcheck // ReaderAt adapters preserve io.EOF.
}

var _ = Describe("Remote architecture selection", func() {
	It("reads only the architecture headers", func() {
		input := universalImage()
		reader := &headerReader{input: input}
		offset, size, err := ARM64Range(reader, int64(len(input)))
		Expect(err).NotTo(HaveOccurred())
		Expect(offset).To(Equal(int64(52)))
		Expect(size).To(Equal(int64(4)))
		Expect(reader.end).To(BeNumerically("<=", 48))
		Expect(reader.read).To(BeNumerically("<=", 48))
	})
	It("keeps thin images whole", func() {
		data := []byte("thin image")
		reader := &headerReader{input: data}
		offset, size, err := ARM64Range(reader, int64(len(data)))
		Expect(err).NotTo(HaveOccurred())
		Expect(offset).To(BeZero())
		Expect(size).To(Equal(int64(len(data))))
		Expect(reader.read).To(Equal(4))
	})
	It("rejects invalid counts and out-of-bounds slices", func() {
		for _, index := range []int{4, 36, 40} {
			data := universalImage()
			binary.BigEndian.PutUint32(data[index:], ^uint32(0))
			_, _, err := ARM64Range(bytes.NewReader(data), int64(len(data)))
			Expect(err).To(HaveOccurred())
		}
	})
	It("reports truncated headers and missing arm64e slices", func() {
		data := universalImage()
		_, _, err := ARM64Range(bytes.NewReader(data[:10]), int64(len(data)))
		Expect(err).To(HaveOccurred())
		binary.BigEndian.PutUint32(data[32:], 0)
		_, _, err = ARM64Range(bytes.NewReader(data), int64(len(data)))
		Expect(err).To(MatchError(ContainSubstring("no arm64e slice")))
	})
	It("handles tiny files and rejects a negative declared size", func() {
		_, size, err := ARM64Range(bytes.NewReader([]byte{1}), 1)
		Expect(err).NotTo(HaveOccurred())
		Expect(size).To(Equal(int64(1)))
		_, _, err = ARM64Range(bytes.NewReader(nil), -1)
		Expect(err).To(HaveOccurred())
		_, _, err = ARM64Range(io.NewSectionReader(bytes.NewReader(nil), 0, 4), 4)
		Expect(err).To(HaveOccurred())
	})
})
