//go:build darwin || linux

package unicorn

import (
	"context"
	"fmt"

	"github.com/ebitengine/purego"
)

func openLibrary(ctx context.Context) (library, error) {
	paths, err := cachedRuntimePaths(ctx)
	if err != nil {
		return library{}, err
	}

	if err := ctx.Err(); err != nil {
		return library{}, fmt.Errorf("load Unicorn library: %w", err)
	}

	handle, err := purego.Dlopen(paths.library, purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return library{}, fmt.Errorf("%s: %w", paths.library, err)
	}

	return library{
		handle: handle,
		close: func() error {
			return purego.Dlclose(handle)
		},
	}, nil
}
