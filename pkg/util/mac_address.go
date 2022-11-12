package util

import (
	"github.com/pkg/errors"
	"net"
)

func MacAddress() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", errors.Wrap(err, "failed to get network interfaces")
	}

	if len(ifaces) == 0 {
		return "", errors.New("no network interfaces were found")
	}

	for _, iface := range ifaces {
		addr := iface.HardwareAddr.String()
		if addr != "" {
			return addr, nil
		}
	}

	return "", errors.New("could not find a network interface with a MAC address")
}
