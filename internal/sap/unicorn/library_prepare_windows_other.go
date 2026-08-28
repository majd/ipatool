//go:build windows && !amd64 && !arm64

package unicorn

func prepareArchitectureLibrary(uintptr, uintptr, uintptr) error {
	return nil
}
