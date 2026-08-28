//go:build !windows || !arm64

package unicorn

func prepareRuntimeLibrary(path string) (string, error) {
	return path, nil
}
