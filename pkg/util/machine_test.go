package util

import (
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"syscall"
)

var _ = Describe("Machine", func() {
	var (
		ctrl    *gomock.Controller
		machine Machine
		mockOS  *MockOperatingSystem
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockOS = NewMockOperatingSystem(ctrl)
		machine = NewMachine(MachineArgs{
			OperatingSystem: mockOS,
		})
	})

	When("OperatingSystem is darwin", func() {
		BeforeEach(func() {
			mockOS.EXPECT().
				Getenv("HOME").
				Return("/home/test")
		})

		It("returns home directory from HOME", func() {
			dir := machine.HomeDirectory()
			Expect(dir).To(Equal("/home/test"))
		})
	})

	When("machine has network interfaces", func() {
		It("returns MAC address of the first interface", func() {
			res, err := machine.MacAddress()
			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(ContainSubstring(":"))
		})
	})

	When("reading password from stdout", func() {
		It("returns error", func() {
			_, err := machine.ReadPassword(syscall.Stdout)
			Expect(err).To(MatchError(ContainSubstring("inappropriate ioctl for device")))
		})
	})
})
