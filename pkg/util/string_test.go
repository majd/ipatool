package util

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("String", func() {
	It("returns current value", func() {
		res := IfEmpty("current", "fallback")
		Expect(res).To(Equal("current"))
	})

	It("returns fallback value", func() {
		res := IfEmpty("", "fallback")
		Expect(res).To(Equal("fallback"))
	})
})
