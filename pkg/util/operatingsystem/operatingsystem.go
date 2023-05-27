package operatingsystem

import (
	"fmt"
	"os"
)

//go:generate go run github.com/golang/mock/mockgen -source=operatingsystem.go -destination=operatingsystem_mock.go -package operatingsystem
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

func (operatingSystem) Stat(name string) (os.FileInfo, error) {
	info, err := os.Stat(name)
	if err != nil {
		return nil, fmt.Errorf("failed to describe file '%s': %w", name, err)
	}

	return info, nil
}

func (operatingSystem) Getwd() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	return wd, nil
}

func (operatingSystem) OpenFile(name string, flag int, perm os.FileMode) (*os.File, error) {
	file, err := os.OpenFile(name, flag, perm)
	if err != nil {
		return nil, fmt.Errorf("failed to open file '%s': %w", name, err)
	}

	return file, nil
}

func (operatingSystem) Remove(name string) error {
	err := os.Remove(name)
	if err != nil {
		return fmt.Errorf("failed to remove file '%s': %w", name, err)
	}

	return nil
}

func (operatingSystem) IsNotExist(err error) bool {
	return os.IsNotExist(err)
}

func (operatingSystem) MkdirAll(path string, perm os.FileMode) error {
	err := os.MkdirAll(path, perm)
	if err != nil {
		return fmt.Errorf("failed to create directory '%s': %w", path, err)
	}

	return nil
}

func (operatingSystem) Rename(oldPath, newPath string) error {
	err := os.Rename(oldPath, newPath)
	if err != nil {
		return fmt.Errorf("failed to rename '%s' to '%s': %w", oldPath, newPath, err)
	}

	return nil
}
