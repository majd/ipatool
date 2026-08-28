//go:build windows

package unicorn

import (
	"context"
	"errors"
	"fmt"
	"syscall"
)

func openLibrary(ctx context.Context) (library, error) {
	paths, err := cachedRuntimePaths(ctx)
	if err != nil {
		return library{}, err
	}
	if err := ctx.Err(); err != nil {
		return library{}, err
	}

	handles := make([]syscall.Handle, 0, len(paths.dependencies)+1)
	for _, dependency := range paths.dependencies {
		handle, err := syscall.LoadLibrary(dependency)
		if err != nil {
			return library{}, errors.Join(fmt.Errorf("%s: %w", dependency, err), unloadLibraries(handles))
		}
		handles = append(handles, handle)
	}
	if err := ctx.Err(); err != nil {
		return library{}, errors.Join(err, unloadLibraries(handles))
	}

	handle, err := syscall.LoadLibrary(paths.library)
	if err != nil {
		return library{}, errors.Join(fmt.Errorf("%s: %w", paths.library, err), unloadLibraries(handles))
	}
	handles = append(handles, handle)
	if err := prepareLibrary(handle); err != nil {
		return library{}, errors.Join(fmt.Errorf("prepare %s: %w", paths.library, err), unloadLibraries(handles))
	}

	return library{
		handle: uintptr(handle),
		close:  func() error { return unloadLibraries(handles) },
	}, nil
}

func unloadLibraries(handles []syscall.Handle) error {
	var err error
	for index := len(handles) - 1; index >= 0; index-- {
		err = errors.Join(err, syscall.FreeLibrary(handles[index]))
	}
	return err
}
