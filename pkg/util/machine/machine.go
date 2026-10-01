package machine

import (
	"fmt"
	"net"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/majd/ipatool/v2/pkg/util/operatingsystem"
	"golang.org/x/term"
)

//go:generate go run go.uber.org/mock/mockgen -source=machine.go -destination=machine_mock.go -package machine
type Machine interface {
	MacAddress() (string, error)
	HomeDirectory() string
	ReadPassword(fd int) ([]byte, error)
}

type machine struct {
	os             operatingsystem.OperatingSystem
	interfaces     func() ([]net.Interface, error)
	macAddressOnce sync.Once
	macAddress     string
	macAddressErr  error
}

type Args struct {
	OS operatingsystem.OperatingSystem
}

func New(args Args) Machine {
	return &machine{
		os:         args.OS,
		interfaces: net.Interfaces,
	}
}

// Resolve once so bag requests, authentication and downloads use the same
// identity even if an adapter is added or removed during this invocation.
func (m *machine) MacAddress() (string, error) {
	m.macAddressOnce.Do(func() {
		interfaces, err := m.interfaces()
		if err != nil {
			m.macAddressErr = fmt.Errorf("failed to get network interfaces: %w", err)

			return
		}

		m.macAddress, m.macAddressErr = selectMacAddress(interfaces, interfaceMacAddress)
	})

	return m.macAddress, m.macAddressErr
}

func (m *machine) HomeDirectory() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(m.os.Getenv("HOMEDRIVE"), m.os.Getenv("HOMEPATH"))
	}

	return m.os.Getenv("HOME")
}

func (*machine) ReadPassword(fd int) ([]byte, error) {
	data, err := term.ReadPassword(fd)
	if err != nil {
		return nil, fmt.Errorf("failed to read password: %w", err)
	}

	return data, nil
}
