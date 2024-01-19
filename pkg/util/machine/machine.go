package machine

import (
	"fmt"
	"net"
	"path/filepath"
	"runtime"

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
	os operatingsystem.OperatingSystem
}

type Args struct {
	OS operatingsystem.OperatingSystem
}

func New(args Args) Machine {
	return &machine{
		os: args.OS,
	}
}

func (*machine) MacAddress() (string, error) {
	interfaces, err := net.Interfaces()
	if err != nil {
		return "", fmt.Errorf("failed to get network interfaces: %w", err)
	}

	if len(interfaces) == 0 {
		return "", fmt.Errorf("could not find network interfaces: %w", err)
	}

	for _, netInterface := range interfaces {
		addr := netInterface.HardwareAddr.String()
		if addr != "" {
			return addr, nil
		}
	}

	return "", fmt.Errorf("could not find network interfaces with a valid mac address: %w", err)
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
