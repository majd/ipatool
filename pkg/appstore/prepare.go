package appstore

import (
	"context"
	"fmt"

	"github.com/majd/ipatool/v2/internal/sap/assets"
	"github.com/majd/ipatool/v2/internal/store/profile"
)

// Prepare loads the compiled profile and caches the required Apple runtime assets.
// Requests prepare these lazily; library hosts can preload them with this function.
func Prepare(ctx context.Context) error {
	if _, err := profile.Load(ctx); err != nil {
		return fmt.Errorf("prepare Store runtime: %w", err)
	}

	if _, err := assets.Load(ctx); err != nil {
		return fmt.Errorf("prepare Store assets: %w", err)
	}

	return nil
}
