//go:build !darwin || ios

package machine

import "net"

func interfaceMacAddress(networkInterface net.Interface) (string, error) {
	return networkInterface.HardwareAddr.String(), nil
}
