package operatingsystem

import (
	"os"
)

//go:generate go run go.uber.org/mock/mockgen -source=operatingsystem.go -destination=operatingsystem_mock.go -package operatingsystem
type OperatingSystem interface {
	Getenv(key string) string
	Stat(name string) (os.FileInfo, error)
	Getwd() (string, error)
	OpenFile(name string, flag int, perm os.FileMode) (*os.File, error)
	Remove(name string) error
	IsNotExist(err error) bool
	MkdirAll(path string, perm os.FileMode) error
	Rename(oldPath, newPath string) error
}

type operatingSystem struct{}

func New() OperatingSystem {
	return &operatingSystem{}
}

func (operatingSystem) Getenv(key string) string {
	return os.Getenv(key)
}

// nolint:wrapcheck
func (operatingSystem) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

// nolint:wrapcheck
func (operatingSystem) Getwd() (string, error) {
	return os.Getwd()
}

// nolint:wrapcheck
func (operatingSystem) OpenFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	return os.OpenFile(name, flag, perm)
}

// nolint:wrapcheck
func (operatingSystem) Remove(name string) error {
	return os.Remove(name)
}

// nolint:wrapcheck
func (operatingSystem) IsNotExist(err error) bool {
	return os.IsNotExist(err)
}

// nolint:wrapcheck
func (operatingSystem) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

// nolint:wrapcheck
func (operatingSystem) Rename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}
