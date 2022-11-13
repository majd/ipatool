package util

import (
	"github.com/pkg/errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
)

//go:generate mockgen -source=machine.go -destination=machine_mock.go -package util
type Machine interface {
	MacAddress() (string, error)
	HomeDirectory() string
}

type machine struct{}

func NewMachine() Machine {
	return &machine{}
}

func (*machine) MacAddress() (string, error) {
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

func (*machine) HomeDirectory() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("HOMEDRIVE"), os.Getenv("HOMEPATH"))
	}

	return os.Getenv("HOME")
}
