package machine

import (
	"net"
	"os"
	"path/filepath"
)

func interfaceMacAddress(networkInterface net.Interface) (macAddressCandidate, error) {
	return macAddressWithSysfs(networkInterface, "/sys/class/net"), nil
}

func macAddressWithSysfs(networkInterface net.Interface, root string) macAddressCandidate {
	candidate := macAddressCandidate{address: networkInterface.HardwareAddr, priority: macPriorityFallback}
	// Physical adapters have a backing device in sysfs. Keep a fallback for
	// containers and systems where sysfs is unavailable.
	if _, err := os.Stat(filepath.Join(root, networkInterface.Name, "device")); err == nil {
		candidate.priority = macPriorityPhysical
	}

	return candidate
}
