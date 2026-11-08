package appstore

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/majd/ipatool/v2/internal/storeauth"
)

func (t *appstore) storeAMDProof(ctx context.Context, endpoint string, hardwareID []byte) (map[string]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	if len(hardwareID) != 6 {
		return nil, errors.New("store AMD proof requires a six-byte device GUID")
	}

	if !isStoreAMDCommerceURL(endpoint) {
		return nil, errors.New("store AMD proof endpoint is not allowlisted")
	}

	t.authStateMu.Lock()
	defer t.authStateMu.Unlock()

	if len(t.anisetteState) == 0 && t.keychain != nil {
		t.anisetteState, _ = t.keychain.Get(anisetteKeychainKey)
	}

	proof, state, err := storeauth.StoreAMDHeaders(ctx, t.anisetteState, endpoint, hardwareID)
	if err != nil {
		return nil, fmt.Errorf("generate Store AMD proof: %w", err)
	}

	if t.keychain == nil {
		return nil, errors.New("keychain is not configured")
	}

	if err = t.keychain.Set(anisetteKeychainKey, state); err != nil {
		return nil, fmt.Errorf("save Store AMD state: %w", err)
	}

	t.anisetteState = state

	return proof, nil
}

func isStoreAMDCommerceURL(endpoint string) bool {
	if strings.Contains(endpoint, "#") {
		return false
	}

	parsed, err := url.ParseRequestURI(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Fragment != "" ||
		parsed.RawPath != "" || parsed.Opaque != "" || (parsed.Port() != "" && parsed.Port() != "443") {
		return false
	}

	query, err := url.ParseQuery(parsed.RawQuery)
	if err != nil || parsed.ForceQuery || len(query) != 1 {
		return false
	}

	values, ok := query["guid"]
	if !ok || len(values) != 1 || values[0] == "" {
		return false
	}

	host := strings.ToLower(parsed.Hostname())
	switch host {
	case downloadDispatchDomain:
		switch parsed.Path {
		case entDownloadPath, redownloadProductPath, updateProductPath:
			return true
		}
	default:
		if !isStoreBuyHost(host) {
			return false
		}

		switch parsed.Path {
		case PrivateAppStoreAPIPathDownload:
			return true
		}
	}

	return false
}

func isStoreBuyHost(host string) bool {
	if host == PrivateAppStoreAPIDomain {
		return true
	}

	const suffix = "-" + PrivateAppStoreAPIDomain

	pod := strings.TrimSuffix(host, suffix)
	if pod == host || len(pod) < 2 || pod[0] != 'p' {
		return false
	}

	for _, digit := range pod[1:] {
		if digit < '0' || digit > '9' {
			return false
		}
	}

	return true
}
