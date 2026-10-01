package machine

import (
	"net"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Linux hardware address", func() {
	It("prefers a sysfs backing device and preserves virtual adapters as fallbacks", func() {
		root := GinkgoT().TempDir()
		Expect(os.MkdirAll(filepath.Join(root, "eth0", "device"), 0o755)).To(Succeed())
		Expect(os.MkdirAll(filepath.Join(root, "veth0"), 0o755)).To(Succeed())
		for name, priority := range map[string]int{"eth0": macPriorityPhysical, "veth0": macPriorityFallback, "missing": macPriorityFallback} {
			networkInterface := net.Interface{Name: name, HardwareAddr: net.HardwareAddr{2, 1, 2, 3, 4, 5}}
			candidate := macAddressWithSysfs(networkInterface, root)
			Expect(candidate.priority).To(Equal(priority))
			Expect(candidate.address).To(Equal(networkInterface.HardwareAddr))
		}
	})
})
