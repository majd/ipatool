// Command profilegen regenerates the committed Store profile from pinned Apple
// binaries under Unicorn. Normal builds and library consumers do not run it.
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/majd/ipatool/v2/internal/sap/assets"
	"github.com/majd/ipatool/v2/internal/store/profilegen"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	cachePath := flag.String("cache", "", "optional offline copy of the pinned dyld shared cache")
	output := flag.String("output", "internal/store/profile/data_generated.go", "generated Go source file")
	check := flag.Bool("check", false, "regenerate and compare with the committed output without writing files")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	fmt.Fprintln(os.Stderr, "Generating Store profile from pinned Apple binaries under Unicorn...")

	var cache *assets.StoreCache

	var err error

	if *cachePath == "" {
		cache, err = assets.OpenRemoteStoreCache(ctx)
	} else {
		cache, err = assets.OpenLocalStoreCache(ctx, *cachePath)
	}

	if err != nil {
		return fmt.Errorf("open profile inputs: %w", err)
	}

	defer cache.Close()

	result, err := profilegen.Generate(ctx, cache)
	if err != nil {
		return fmt.Errorf("generate profile: %w", err)
	}

	workspace, err := os.MkdirTemp("", "ipatool-profile-")
	if err != nil {
		return fmt.Errorf("create profile workspace: %w", err)
	}

	defer os.RemoveAll(workspace)

	pluginPath := filepath.Join(workspace, "AMSAccountAuthenticationPlugin")
	if err = cache.ExtractAuthenticationPlugin(pluginPath); err != nil {
		return fmt.Errorf("extract authentication plugin: %w", err)
	}

	plugin, err := os.ReadFile(pluginPath)
	if err != nil {
		return fmt.Errorf("read authentication plugin: %w", err)
	}

	source, err := result.GoSource(ctx, plugin)
	if err != nil {
		return fmt.Errorf("encode profile source: %w", err)
	}

	current, err := os.ReadFile(*output)
	if err == nil && bytes.Equal(current, source) {
		fmt.Fprintln(os.Stderr, "Store profile is up to date.")

		return nil
	}

	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read generated profile: %w", err)
	}

	if *check {
		return fmt.Errorf("generated Store profile differs from %s; regenerate it before committing", *output)
	}

	if err = writeSource(ctx, *output, source); err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Wrote %s (%d bytes).\n", *output, len(source))

	return nil
}

// writeSource replaces the complete Go file atomically after generation succeeds.
func writeSource(ctx context.Context, path string, source []byte) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("write profile source: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create profile output directory: %w", err)
	}

	file, err := os.CreateTemp(filepath.Dir(path), ".profile-source-*")
	if err != nil {
		return fmt.Errorf("create profile output: %w", err)
	}

	defer os.Remove(file.Name())
	defer file.Close()

	if err = file.Chmod(0644); err != nil {
		return fmt.Errorf("set profile source permissions: %w", err)
	}

	if _, err = file.Write(source); err != nil {
		return fmt.Errorf("write profile source: %w", err)
	}

	if err = file.Sync(); err != nil {
		return fmt.Errorf("sync profile source: %w", err)
	}

	if err = file.Close(); err != nil {
		return fmt.Errorf("close profile source: %w", err)
	}

	if err = ctx.Err(); err != nil {
		return fmt.Errorf("write profile source: %w", err)
	}

	if err = os.Rename(file.Name(), path); err != nil {
		return fmt.Errorf("install profile source: %w", err)
	}

	return nil
}
