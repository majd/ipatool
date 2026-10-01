//go:build (!darwin && !linux && !windows) || ios

package machine

import "net"

func interfaceMacAddress(networkInterface net.Interface) (macAddressCandidate, error) {
	return macAddressCandidate{address: networkInterface.HardwareAddr, priority: macPriorityFallback}, nil
}
