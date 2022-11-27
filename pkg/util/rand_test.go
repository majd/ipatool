package util

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Rand", func() {
	It("returns random integer in range", func() {
		res := RandInt(1, 3)
		Expect(res).To(BeNumerically(">=", 1))
		Expect(res).To(BeNumerically("<=", 3))
	})
})
