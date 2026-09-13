package unicorn

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const maxArtifactSize = 64 << 20

func linuxUsesMusl() bool {
	return linuxUsesMuslFor("/proc/self/exe", muslLoaderInstalled)
}

func linuxUsesMuslFor(executable string, loaderInstalled func() bool) bool {
	if interpreter, ok := executableInterpreter(executable); ok {
		return interpreterUsesMusl(interpreter)
	}

	return loaderInstalled()
}

// executableInterpreter reads the loader recorded in the executable. Missing PT_INTERP or
// an unreadable executable leaves the caller to fall back to the host filesystem heuristic.
func executableInterpreter(path string) (string, bool) {
	file, err := elf.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()

	for _, program := range file.Progs {
		if program.Type != elf.PT_INTERP {
			continue
		}

		interpreter, err := io.ReadAll(program.Open())
		if err != nil || uint64(len(interpreter)) != program.Filesz {
			return "", false
		}

		return string(bytes.TrimRight(interpreter, "\x00")), true
	}

	return "", false
}

func interpreterUsesMusl(interpreter string) bool {
	return strings.HasPrefix(filepath.Base(interpreter), "ld-musl-")
}

// muslLoaderInstalled reports whether a musl loader exists on the host. That only says musl is
// available, not that this process uses it, so it is a last resort when the interpreter is
// unavailable. glibc distributions package musl for cross compilation, and selecting the
// musllinux artifact there loads a second libc into the process.
func muslLoaderInstalled() bool {
	loaders, _ := filepath.Glob("/lib/ld-musl-*.so.1")
	if len(loaders) != 0 {
		return true
	}

	loaders, _ = filepath.Glob("/usr/lib/ld-musl-*.so.1")
	if len(loaders) != 0 {
		return true
	}

	_, err := os.Stat("/etc/alpine-release")

	return err == nil
}

func ensureLibrary(ctx context.Context, root string, selected artifact, client *http.Client) (string, error) {
	if filepath.Base(selected.filename) != selected.filename {
		return "", fmt.Errorf("invalid library filename %q", selected.filename)
	}

	directory := filepath.Join(root, selected.librarySHA256)
	destination := filepath.Join(directory, selected.filename)

	valid, err := fileMatchesSHA256(destination, selected.librarySHA256)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("verify cached Unicorn library: %w", err)
	}

	if valid {
		return destination, nil
	}

	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", fmt.Errorf("create Unicorn cache: %w", err)
	}

	archive, err := downloadArtifact(ctx, directory, selected, client)
	if err != nil {
		return "", err
	}
	defer os.Remove(archive)

	temporary, err := extractLibrary(archive, directory, selected)
	if err != nil {
		return "", err
	}
	defer os.Remove(temporary)

	if err := installLibrary(temporary, destination, selected.librarySHA256); err != nil {
		return "", err
	}

	return destination, nil
}

//nolint:nonamedreturns // Deferred cleanup must update the returned error.
func downloadArtifact(ctx context.Context, directory string, selected artifact, client *http.Client) (path string, err error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, selected.url, nil)
	if err != nil {
		return "", fmt.Errorf("create Unicorn download request: %w", err)
	}

	request.Header.Set("User-Agent", "ipatool Unicorn runtime downloader")

	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("download Unicorn %s: %w", unicornVersion, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download Unicorn %s: unexpected HTTP status %s", unicornVersion, response.Status)
	}

	if response.ContentLength > maxArtifactSize {
		return "", fmt.Errorf("download Unicorn %s: artifact exceeds %d bytes", unicornVersion, maxArtifactSize)
	}

	file, err := os.CreateTemp(directory, ".unicorn-artifact-*")
	if err != nil {
		return "", fmt.Errorf("create Unicorn download: %w", err)
	}

	path = file.Name()

	defer func() {
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close Unicorn download: %w", closeErr)
		}

		if err != nil {
			_ = os.Remove(path)
		}
	}()

	hash := sha256.New()

	written, err := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, maxArtifactSize+1))
	if err != nil {
		return "", fmt.Errorf("save Unicorn download: %w", err)
	}

	if written > maxArtifactSize {
		return "", fmt.Errorf("download Unicorn %s: artifact exceeds %d bytes", unicornVersion, maxArtifactSize)
	}

	if actual := hex.EncodeToString(hash.Sum(nil)); actual != selected.archiveSHA256 {
		return "", fmt.Errorf("download Unicorn %s: archive SHA-256 is %s, want %s", unicornVersion, actual, selected.archiveSHA256)
	}

	if err := file.Sync(); err != nil {
		return "", fmt.Errorf("sync Unicorn download: %w", err)
	}

	return path, nil
}

//nolint:nonamedreturns // Deferred cleanup must update the returned error.
func extractLibrary(archive, directory string, selected artifact) (path string, err error) {
	entry, err := openArchiveEntry(archive, selected.member, selected.format)
	if err != nil {
		return "", err
	}

	defer func() {
		if closeErr := entry.close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close Unicorn archive entry: %w", closeErr)
		}
	}()

	if entry.size > maxArtifactSize {
		return "", fmt.Errorf("invalid Unicorn library entry %q", selected.member)
	}

	destination, err := os.CreateTemp(directory, ".unicorn-library-*")
	if err != nil {
		return "", fmt.Errorf("create cached Unicorn library: %w", err)
	}

	path = destination.Name()

	defer func() {
		if closeErr := destination.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close cached Unicorn library: %w", closeErr)
		}

		if err != nil {
			_ = os.Remove(path)
		}
	}()

	hash := sha256.New()

	written, err := io.Copy(io.MultiWriter(destination, hash), io.LimitReader(entry.reader, maxArtifactSize+1))
	if err != nil {
		return "", fmt.Errorf("extract Unicorn library: %w", err)
	}

	if written > maxArtifactSize || uint64(written) != entry.size {
		return "", fmt.Errorf("invalid size for Unicorn library entry %q", selected.member)
	}

	if actual := hex.EncodeToString(hash.Sum(nil)); actual != selected.librarySHA256 {
		return "", fmt.Errorf("extracted Unicorn library SHA-256 is %s, want %s", actual, selected.librarySHA256)
	}

	if err := destination.Sync(); err != nil {
		return "", fmt.Errorf("sync cached Unicorn library: %w", err)
	}

	return path, nil
}

func installLibrary(source, destination, checksum string) error {
	if err := os.Rename(source, destination); err == nil {
		return nil
	}

	// Another process may have populated the same content-addressed cache entry.
	valid, verifyErr := fileMatchesSHA256(destination, checksum)
	if verifyErr == nil && valid {
		return nil
	}

	if verifyErr != nil && !errors.Is(verifyErr, os.ErrNotExist) {
		return fmt.Errorf("verify concurrently cached Unicorn library: %w", verifyErr)
	}

	if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("replace cached Unicorn library: %w", err)
	}

	if err := os.Rename(source, destination); err != nil {
		return fmt.Errorf("install cached Unicorn library: %w", err)
	}

	return nil
}

func fileMatchesSHA256(path, expected string) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("open checksum target: %w", err)
	}

	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return false, fmt.Errorf("inspect checksum target: %w", err)
	}

	if info.Size() < 0 || info.Size() > maxArtifactSize {
		return false, nil
	}

	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return false, fmt.Errorf("checksum target: %w", err)
	}

	return hex.EncodeToString(hash.Sum(nil)) == expected, nil
}
