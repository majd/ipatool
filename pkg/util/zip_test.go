package util

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Zip", func() {
	When("slices have different lengths", func() {
		It("returns error", func() {
			_, err := Zip([]string{}, []string{"test"})
			Expect(err).To(HaveOccurred())
		})
	})

	When("slices have different lengths", func() {
		It("returns zipped slices", func() {
			res, err := Zip([]string{
				"lslice1",
				"lslice2",
			}, []string{
				"rslice1",
				"rslice2",
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(res[0].First).To(Equal("lslice1"))
			Expect(res[0].Second).To(Equal("rslice1"))
			Expect(res[1].First).To(Equal("lslice2"))
			Expect(res[1].Second).To(Equal("rslice2"))
		})
	})
})
