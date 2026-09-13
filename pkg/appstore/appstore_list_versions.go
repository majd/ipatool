package appstore

import (
	"errors"
	"fmt"
	"strings"
)

type ListVersionsInput struct {
	Account  Account
	App      App
	Platform Platform
}

type ListVersionsOutput struct {
	ExternalVersionIdentifiers []string
	LatestExternalVersionID    string
}

func (t *appstore) ListVersions(input ListVersionsInput) (ListVersionsOutput, error) {
	platform := input.Platform
	if platform == "" {
		platform = PlatformIPhone
	}

	switch platform {
	case PlatformIPhone, PlatformIPad, PlatformAppleTV, PlatformVisionOS, PlatformMacOS:
	default:
		return ListVersionsOutput{}, fmt.Errorf("invalid platform %q", platform)
	}

	macAddr, err := t.machine.MacAddress()
	if err != nil {
		return ListVersionsOutput{}, fmt.Errorf("failed to get mac address: %w", err)
	}

	guid := strings.ReplaceAll(strings.ToUpper(macAddr), ":", "")

	var externalVersionID string

	switch platform {
	case PlatformMacOS:
		externalVersionID, err = t.lookupLatestMacOSExternalVersionID(input.Account, input.App)
	case PlatformAppleTV, PlatformVisionOS:
		externalVersionID, err = t.lookupLatestExternalVersionID(input.Account, input.App, platform)
	}

	if err != nil {
		return ListVersionsOutput{}, fmt.Errorf("failed to resolve platform version: %w", err)
	}

	res, _, err := t.sendDownloadProduct(input.Account, input.App, guid, externalVersionID, platform)
	if err != nil {
		return ListVersionsOutput{}, err
	}

	if res.Data.FailureType == FailureTypePasswordTokenExpired || res.Data.FailureType == FailureTypeSignInRequired {
		return ListVersionsOutput{}, ErrPasswordTokenExpired
	}

	if res.Data.FailureType == FailureTypeLicenseNotFound {
		return ListVersionsOutput{}, ErrLicenseRequired
	}

	if res.Data.CustomerMessage != "" && (res.Data.FailureType != "" || len(res.Data.Items) == 0) {
		return ListVersionsOutput{}, NewErrorWithMetadata(fmt.Errorf("received error: %s", res.Data.CustomerMessage), res)
	}

	if res.Data.FailureType != "" {
		return ListVersionsOutput{}, NewErrorWithMetadata(fmt.Errorf("received error: %s", res.Data.FailureType), res)
	}

	if len(res.Data.Items) == 0 {
		return ListVersionsOutput{}, NewErrorWithMetadata(errors.New("invalid response"), res)
	}

	item := res.Data.Items[0]

	rawIdentifiers, ok := item.Metadata["softwareVersionExternalIdentifiers"].([]interface{})
	if !ok {
		return ListVersionsOutput{}, NewErrorWithMetadata(fmt.Errorf("failed to get version identifiers from item metadata"), item.Metadata)
	}

	externalVersionIdentifiers := make([]string, len(rawIdentifiers))
	for i, val := range rawIdentifiers {
		externalVersionIdentifiers[i] = fmt.Sprintf("%v", val)
	}

	latestExternalVersionID := item.Metadata["softwareVersionExternalIdentifier"]
	if latestExternalVersionID == nil {
		return ListVersionsOutput{}, NewErrorWithMetadata(fmt.Errorf("failed to get latest version from item metadata"), item.Metadata)
	}

	return ListVersionsOutput{
		ExternalVersionIdentifiers: externalVersionIdentifiers,
		LatestExternalVersionID:    fmt.Sprintf("%v", latestExternalVersionID),
	}, nil
}
