package util

import (
	"os"
	"path/filepath"
	"runtime"
)

func HomeDirectory() string {
	if runtime.GOOS == "windows" {
		return filepath.Join(os.Getenv("HOMEDRIVE"), os.Getenv("HOMEPATH"))
	}
	
	return os.Getenv("HOME")
}
