package util

import "os"

//go:generate mockgen -source=operating_system.go -destination=operating_system_mock.go -package util
type OperatingSystem interface {
	Getenv(key string) string
	Stat(name string) (os.FileInfo, error)
	Executable() (string, error)
	OpenFile(name string, flag int, perm os.FileMode) (*os.File, error)
	Remove(name string) error
}

type operatingSystem struct{}

func NewOperatingSystem() OperatingSystem {
	return &operatingSystem{}
}

func (*operatingSystem) Getenv(key string) string {
	return os.Getenv(key)
}

func (*operatingSystem) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

func (*operatingSystem) Executable() (string, error) {
	return os.Executable()
}

func (*operatingSystem) OpenFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(name, flag, perm)
}

func (*operatingSystem) Remove(name string) error {
	return os.Remove(name)
}
