package appstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"strconv"
	"strings"

	"github.com/byteness/keyring"
	"github.com/majd/ipatool/v2/pkg/http"
)

func purchaseStorefrontCorrection(result purchaseResult, current string) string {
	if result.FailureType != "-128" || result.Metrics.DialogID != "MZCommerce.CountryMismatch" || result.Dialog.OKButtonAction.Kind != "Goto" {
		return ""
	}

	// Only interpret Apple's known store-switch action. Do not navigate to a
	// response-supplied URL or infer a country from localized dialog text.
	u, err := url.Parse(result.Dialog.OKButtonAction.URL)
	if err != nil || u.Scheme != "https" || u.Hostname() != "itunes.apple.com" ||
		(u.Port() != "" && u.Port() != "443") || u.User != nil || u.Fragment != "" || u.RawPath != "" ||
		u.Path != "/WebObjects/MZStore.woa/wa/resetAndRedirect" {
		return ""
	}

	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query["dsf"]) != 1 {
		return ""
	}

	identifier := query.Get("dsf")
	number, err := strconv.ParseUint(identifier, 10, 32)

	if err != nil || number == 0 || strconv.FormatUint(number, 10) != identifier {
		return ""
	}

	current = strings.TrimSpace(current)

	previous := storefrontIdentifier(current)
	if identifier == previous {
		return ""
	}

	// Retain the client's language/platform suffix, changing only the country.
	return identifier + strings.TrimPrefix(current, previous)
}

func (t *appstore) saveAccountStorefront(dsid, storefront string) error {
	// Serialize with native login so a correction cannot overwrite fresh tokens.
	t.authStateMu.Lock()
	defer t.authStateMu.Unlock()

	if t.keychain == nil || dsid == "" {
		return errors.New("saved account is required for storefront recovery")
	}

	info, err := t.AccountInfo()
	if err != nil {
		return err
	}

	if info.Account.DirectoryServicesID != dsid {
		return errors.New("saved account changed during storefront recovery")
	}

	info.Account.StoreFront = storefront

	data, err := json.Marshal(info.Account)
	if err != nil {
		return fmt.Errorf("failed to encode corrected account: %w", err)
	}

	if err := t.keychain.Set("account", data); err != nil {
		return fmt.Errorf("failed to save corrected account: %w", err)
	}

	return nil
}

func (t *appstore) loginStorefront(result http.Result[loginResult], dsid string) (string, error) {
	if storefront, err := result.GetHeader(HTTPHeaderStoreFront); err == nil && strings.TrimSpace(storefront) != "" {
		return storefront, nil
	}

	// The request-storefront header is an echo, not an account-country update.
	// Keep a saved storefront for the same DSID when refreshing credentials.
	if t.keychain != nil {
		info, err := t.AccountInfo()
		if err != nil && !errors.Is(err, keyring.ErrKeyNotFound) && !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}

		if err == nil && dsid != "" && info.Account.DirectoryServicesID == dsid && info.Account.StoreFront != "" {
			return info.Account.StoreFront, nil
		}
	}

	// A first login may supply only this provisional routing hint. A later
	// CountryMismatch response can establish and persist the actual country.
	storefront, err := result.GetHeader(HTTPHeaderRequestStoreFront)
	if err != nil {
		return "", fmt.Errorf("failed to get storefront header: %w", err)
	}

	return storefront, nil
}
