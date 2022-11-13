package util

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"os"
)

var _ = Describe("Machine", func() {
	var (
		machine Machine
	)

	BeforeEach(func() {
		machine = NewMachine()
	})

	When("os is darwin", func() {
		var originalHome string

		BeforeEach(func() {
			originalHome = os.Getenv("HOME")

			err := os.Setenv("HOME", "/testval")
			Expect(err).ToNot(HaveOccurred())
		})

		AfterEach(func() {
			err := os.Setenv("HOME", originalHome)
			Expect(err).ToNot(HaveOccurred())
		})

		It("returns home directory from HOME", func() {
			dir := machine.HomeDirectory()
			Expect(dir).To(Equal("/testval"))
		})
	})

	When("machine has network interfaces", func() {
		It("returns MAC address of the first interface", func() {
			res, err := machine.MacAddress()
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(ContainSubstring(":"))
		})
	})
})
