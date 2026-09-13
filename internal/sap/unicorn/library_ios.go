package unicorn

import (
	"context"
	"fmt"

	"github.com/ebitengine/purego"
)

// iOS builds statically link Unicorn because the desktop runtime artifacts
// cannot be loaded on iOS. tools/build-ios.sh exports its symbols for purego.
func openLibrary(ctx context.Context) (library, error) {
	if err := ctx.Err(); err != nil {
		return library{}, fmt.Errorf("load Unicorn library: %w", err)
	}

	if _, err := purego.Dlsym(purego.RTLD_DEFAULT, "uc_version"); err != nil {
		return library{}, fmt.Errorf("unicorn is not linked; build with tools/build-ios.sh: %w", err)
	}

	return library{handle: purego.RTLD_DEFAULT, close: func() error { return nil }}, nil
}
