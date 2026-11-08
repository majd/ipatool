//nolint:nlreturn,wsl // Authentication steps are intentionally kept in protocol order.
package appstore

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/majd/ipatool/v2/internal/storeauth"
)

const anisetteKeychainKey = "anisette-v4"

func (t *appstore) nativeLogin(input LoginInput) (LoginOutput, error) {
	t.authStateMu.Lock()
	defer t.authStateMu.Unlock()
	if len(t.anisetteState) == 0 && t.keychain != nil {
		t.anisetteState, _ = t.keychain.Get(anisetteKeychainKey)
	}
	ctx := context.Background()
	macAddress, err := t.machine.MacAddress()
	if err != nil {
		return LoginOutput{}, fmt.Errorf("get Store machine identity: %w", err)
	}
	guid, hardwareID, err := machineIdentity(macAddress)
	if err != nil {
		return LoginOutput{}, err
	}
	bag, err := t.bag(guid)
	if err != nil {
		return LoginOutput{}, fmt.Errorf("get Store authentication bag: %w", err)
	}
	if len(t.anisetteState) != 0 {
		proof, state, proofErr := storeauth.StoreAMDHeaders(ctx, t.anisetteState, bag.AuthEndpoint, hardwareID)
		if proofErr == nil {
			if err = t.keychain.Set(anisetteKeychainKey, state); err != nil {
				return LoginOutput{}, fmt.Errorf("save Apple authentication state: %w", err)
			}
			t.anisetteState = state
			t.authenticationHeaders = make(http.Header)
			for key, value := range proof {
				t.authenticationHeaders.Set(key, value)
			}
			defer func() { t.authenticationHeaders = nil }()

			return t.legacyLogin(input)
		}
	}
	headers, state, err := storeauth.AnisetteHeaders(ctx, t.authHTTP, t.anisetteState)
	if err != nil {
		return LoginOutput{}, fmt.Errorf("prepare Apple authentication: %w", err)
	}
	if t.keychain == nil {
		return LoginOutput{}, errors.New("keychain is not configured")
	}
	if err = t.keychain.Set(anisetteKeychainKey, state); err != nil {
		return LoginOutput{}, fmt.Errorf("save Apple authentication state: %w", err)
	}
	t.anisetteState = state
	t.authenticationHeaders = headers.Clone()
	defer func() { t.authenticationHeaders = nil }()

	output, err := t.legacyLogin(input)
	var action *authenticationMachineActionError
	if !errors.As(err, &action) || action.action != "RP" {
		return output, err
	}

	for _, key := range []string{"X-Apple-I-MD", "X-Apple-I-MD-M", "X-Apple-I-MD-RINFO"} {
		t.authenticationHeaders.Del(key)
	}

	output, err = t.legacyLogin(input)
	if !errors.As(err, &action) || action.action != "SP" {
		return output, err
	}

	kbsync, err := t.generateKBSync(ctx, hardwareID, ^uint64(1))
	if err != nil {
		return LoginOutput{}, fmt.Errorf("generate Apple Store provisioning kbsync: %w", err)
	}
	state, err = storeauth.ProvisionStoreAnisette(ctx, t.authHTTP, state, action.data, privateAppStoreProvisioningURL, storeAuthenticationUserAgent, kbsync, hardwareID, headers)
	if err != nil {
		return LoginOutput{}, fmt.Errorf("complete Apple Store provisioning: %w", err)
	}
	if err = t.keychain.Set(anisetteKeychainKey, state); err != nil {
		return LoginOutput{}, fmt.Errorf("save Apple authentication state: %w", err)
	}
	t.anisetteState = state
	proof, nextState, proofErr := storeauth.StoreAMDHeaders(ctx, state, bag.AuthEndpoint, hardwareID)
	if proofErr != nil {
		return LoginOutput{}, fmt.Errorf("prepare Store AMD proof: %w", proofErr)
	}
	state = nextState
	headers = make(http.Header)
	for key, value := range proof {
		headers.Set(key, value)
	}
	if err = t.keychain.Set(anisetteKeychainKey, state); err != nil {
		return LoginOutput{}, fmt.Errorf("save Store AMD state: %w", err)
	}
	t.anisetteState = state
	t.authenticationHeaders = headers.Clone()

	return t.legacyLogin(input)
}
