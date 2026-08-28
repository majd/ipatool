package unicorn

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func prepareRuntimeLibrary(path string) (string, error) {
	image, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read Unicorn library: %w", err)
	}
	if err := patchWindowsARM64TCGMasks(image); err != nil {
		return "", fmt.Errorf("prepare Windows ARM64 Unicorn library: %w", err)
	}

	sum := sha256.Sum256(image)
	checksum := hex.EncodeToString(sum[:])
	destination := filepath.Join(filepath.Dir(path), "libunicorn-windows-arm64.dll")
	valid, err := fileMatchesSHA256(destination, checksum)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("verify prepared Unicorn library: %w", err)
	}
	if valid {
		return destination, nil
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), ".libunicorn-prepared-*")
	if err != nil {
		return "", fmt.Errorf("create prepared Unicorn library: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if _, err := temporary.Write(image); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("write prepared Unicorn library: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return "", fmt.Errorf("sync prepared Unicorn library: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close prepared Unicorn library: %w", err)
	}
	if err := installLibrary(temporaryPath, destination, checksum); err != nil {
		return "", fmt.Errorf("install prepared Unicorn library: %w", err)
	}
	return destination, nil
}
