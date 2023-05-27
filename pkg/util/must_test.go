package util

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Must", func() {
	It("returns current value", func() {
		res := Must("value", nil)
		Expect(res).To(Equal("value"))
	})

	It("panics", func() {
		defer func() {
			r := recover()
			Expect(r).To(Equal("test"))
		}()

		_ = Must("value", errors.New("test"))
	})
})
