package util

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("MAC Address", func() {
	It("returns MAC address", func() {
		res, err := MacAddress()
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(ContainSubstring(":"))
	})
})
