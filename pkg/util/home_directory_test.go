package util

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Home Directory", func() {
	It("returns home directory", func() {
		res := HomeDirectory()
		Expect(res).To(ContainSubstring("/"))
	})
})
