package machimage

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Streaming ARM64 extraction", func() {
	It("hashes discarded bytes and stops at the declared size", func() {
		fat := append(universalImage(), []byte("unused suffix")...)
		reader := bytes.NewReader(append(bytes.Clone(fat), []byte("trailer")...))
		digest := sha256.New()
		data, err := ReadARM64(io.TeeReader(reader, digest), len(fat))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(Equal("arm!"))
		expected := sha256.Sum256(fat)
		Expect(digest.Sum(nil)).To(Equal(expected[:]))
		trailing, err := io.ReadAll(reader)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(trailing)).To(Equal("trailer"))
	})
	It("preserves thin and short input", func() {
		for _, input := range [][]byte{nil, {1}, {1, 2, 3}, []byte("thin image")} {
			data, err := ReadARM64(bytes.NewReader(input), len(input))
			Expect(err).NotTo(HaveOccurred())
			Expect(bytes.Equal(data, input)).To(BeTrue())
		}
	})
	It("rejects a negative size", func() {
		_, err := ReadARM64(bytes.NewReader(nil), -1)
		Expect(err).To(MatchError(ContainSubstring("invalid image size")))
	})
	It("rejects truncation before, inside, and after the selected slice", func() {
		input := append(universalImage(), []byte("suffix")...)
		for _, length := range []int{0, 4, 7, 20, 48, 53, 55, 59} {
			_, err := ReadARM64(bytes.NewReader(input[:length]), len(input))
			Expect(err).To(HaveOccurred())
			Expect(errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)).To(BeTrue())
		}
	})
	It("rejects invalid architecture ranges before allocating their data", func() {
		input := universalImage()
		input[40] = 0xff
		_, err := ReadARM64(bytes.NewReader(input), len(input))
		Expect(err).To(MatchError(ContainSubstring("exceeds input size")))
	})
})
