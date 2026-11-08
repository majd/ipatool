package storeauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/majd/ipatool/v2/internal/sap/assets"
	"github.com/majd/ipatool/v2/internal/sap/machine"
	"github.com/majd/ipatool/v2/internal/store/emulator"
	"github.com/majd/ipatool/v2/internal/store/profile"
)

// StoreAMDHeaders executes AppleMediaServices' type-2 anisette flow and its
// com.apple.adid dependency entirely inside the pinned Unicorn runtimes.
func StoreAMDHeaders(ctx context.Context, encoded []byte, endpoint string, hardwareID []byte) (map[string]string, []byte, error) {
	if ctx == nil {
		return nil, nil, errors.New("store AMD context is nil")
	}

	state, err := decodeAnisetteState(encoded)
	if err != nil {
		return nil, nil, err
	}

	if len(state.Provisioning) == 0 {
		return nil, nil, errors.New("store AMD state is not provisioned")
	}

	bundle, err := assets.Load(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("load Store AMD assets: %w", err)
	}

	adi, err := machine.OpenModernADI(ctx, bundle, state.Identifier, state.Provisioning)
	if err != nil {
		return nil, nil, fmt.Errorf("open Store AMD service: %w", err)
	}

	defer func() { _ = adi.Close() }()

	prepared, err := profile.Load(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("load Store AMD framework profile: %w", err)
	}

	framework, err := emulator.Open(ctx, bundle.AppleMediaServices, prepared)
	if err != nil {
		return nil, nil, fmt.Errorf("open Store AMD framework: %w", err)
	}

	defer func() { _ = framework.Close() }()

	headers, err := framework.StoreAMDHeaders(endpoint, hardwareID, func(service string, request []byte) ([]byte, error) {
		if service != "com.apple.adid" {
			return nil, fmt.Errorf("unsupported Store AMD XPC service %s", service)
		}

		return adi.ExchangeXPC(request)
	})
	if err != nil {
		return nil, nil, fmt.Errorf("run Store AMD framework: %w", err)
	}

	state.Provisioning, err = adi.State()
	if err != nil {
		return nil, nil, fmt.Errorf("persist Store AMD state: %w", err)
	}

	updated, err := json.Marshal(state)
	if err != nil {
		return nil, nil, errors.New("encode Store AMD state")
	}

	return headers, updated, nil
}
