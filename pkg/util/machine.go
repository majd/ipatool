package util

import (
	"github.com/pkg/errors"
	"golang.org/x/term"
	"net"
	"path/filepath"
	"runtime"
)

//go:generate mockgen -source=machine.go -destination=machine_mock.go -package util
type Machine interface {
	MacAddress() (string, error)
	HomeDirectory() string
	ReadPassword(fd int) ([]byte, error)
}

type machine struct {
	os OperatingSystem
}

type MachineArgs struct {
	OperatingSystem OperatingSystem
}

func NewMachine(args MachineArgs) Machine {
	return &machine{
		os: args.OperatingSystem,
	}
}

func (*machine) MacAddress() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", errors.Wrap(err, ErrGetNetworkInterfaces.Error())
	}

	if len(ifaces) == 0 {
		return "", errors.New(ErrEmptyNetworkInterfaces.Error())
	}

	for _, iface := range ifaces {
		addr := iface.HardwareAddr.String()
		if addr != "" {
			return addr, nil
		}
	}

	return "", errors.New(ErrInvalidNetworkInterface.Error())
}

func (m *machine) HomeDirectory() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(m.os.Getenv("HOMEDRIVE"), m.os.Getenv("HOMEPATH"))
	}

	return m.os.Getenv("HOME")
}

func (*machine) ReadPassword(fd int) ([]byte, error) {
	return term.ReadPassword(fd)
}
