package profilegen

import (
	"context"
	"crypto/sha256"
	"fmt"

	"github.com/majd/ipatool/v2/internal/sap/assets"
	profileformat "github.com/majd/ipatool/v2/internal/store/profile/format"
)

// Result contains a self-contained metadata profile and its reconstructed pages.
// No native reference data is accepted during generation.
type Result struct {
	Profile *profileformat.Profile
	Pages   []byte
}

// Generate executes the pinned Swift runtime under Unicorn and inspects the
// downloaded Mach-O metadata. The host never loads an Apple framework.
//
//nolint:nonamedreturns // Convert binary decoding panics to a contextual error.
func Generate(ctx context.Context, cache *assets.StoreCache) (result *Result, err error) {
	defer func() {
		if p := recover(); p != nil {
			result = nil
			err = fmt.Errorf("generate Store profile: %v", p)
		}
	}()

	s, err := newSource(ctx, cache)
	if err != nil {
		return nil, fmt.Errorf("generate Store assets: %w", err)
	}

	defer s.close()

	req, err := s.discoverSwiftRequests()
	if err != nil {
		return nil, fmt.Errorf("discover Swift metadata: %w", err)
	}

	classes := s.objcClasses()

	v, err := newBootstrapVM(ctx, s)
	if err != nil {
		return nil, fmt.Errorf("generate Store assets: %w", err)
	}

	defer v.engine.Close()

	if err = v.realizeStoreClasses(classes); err != nil {
		return nil, fmt.Errorf("generate Store assets: %w", err)
	}

	p, err := v.swiftMetadata(req)
	if err != nil {
		return nil, fmt.Errorf("generate Store assets: %w", err)
	}

	p.Features, err = s.features()
	if err != nil {
		return nil, fmt.Errorf("generate Store assets: %w", err)
	}

	pages := v.collect(p, classes)

	return &Result{Profile: p, Pages: pages}, nil
}
func digest(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }
