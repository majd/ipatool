package machine

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/majd/ipatool/v2/pkg/util/operatingsystem"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

func TestMachine(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Machine Suite")
}

var _ = Describe("Machine", func() {
	var (
		ctrl    *gomock.Controller
		machine Machine
		mockOS  *operatingsystem.MockOperatingSystem
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockOS = operatingsystem.NewMockOperatingSystem(ctrl)
		machine = New(Args{
			OS: mockOS,
		})
	})

	When("reading the home directory", func() {
		var expected string

		BeforeEach(func() {
			if runtime.GOOS == "windows" {
				mockOS.EXPECT().Getenv("HOMEDRIVE").Return("C:")
				mockOS.EXPECT().Getenv("HOMEPATH").Return(`\Users\test`)
				expected = filepath.Join("C:", `\Users\test`)
			} else {
				mockOS.EXPECT().Getenv("HOME").Return("/home/test")
				expected = "/home/test"
			}
		})

		It("returns the platform home directory", func() {
			dir := machine.HomeDirectory()
			Expect(dir).To(Equal(expected))
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
			_, err := machine.ReadPassword(int(os.Stdout.Fd()))
			Expect(err).To(HaveOccurred())
		})
	})
})
