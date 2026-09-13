//go:build !ios

package unicorn

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

var artifactHTTPClient = &http.Client{Timeout: 2 * time.Minute}

type runtimePaths struct {
	library      string
	dependencies []string
}

func cachedRuntimePaths(ctx context.Context) (runtimePaths, error) {
	goos := runtime.GOOS
	if goos == "linux" && linuxUsesMusl() {
		goos = "linux-musl"
	}

	selected, err := artifactFor(goos, runtime.GOARCH)
	if err != nil {
		return runtimePaths{}, err
	}

	cache, err := os.UserCacheDir()
	if err != nil {
		return runtimePaths{}, fmt.Errorf("locate user cache: %w", err)
	}

	root := filepath.Join(cache, "ipatool", "unicorn", unicornVersion)
	paths := runtimePaths{dependencies: make([]string, 0, len(selected.dependencies))}

	for _, dependency := range selected.dependencies {
		path, err := ensureLibrary(ctx, root, dependency, artifactHTTPClient)
		if err != nil {
			return runtimePaths{}, err
		}

		paths.dependencies = append(paths.dependencies, path)
	}

	paths.library, err = ensureLibrary(ctx, root, selected, artifactHTTPClient)
	if err != nil {
		return runtimePaths{}, err
	}

	paths.library, err = prepareRuntimeLibrary(paths.library)
	if err != nil {
		return runtimePaths{}, err
	}

	return paths, nil
}
