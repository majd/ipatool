package appstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type GetVersionMetadataInput struct {
	Context   context.Context
	Account   Account
	App       App
	VersionID string
	Platform  Platform
}

type GetVersionMetadataOutput struct {
	DisplayVersion string
	ReleaseDate    time.Time
}

func (t *appstore) GetVersionMetadata(input GetVersionMetadataInput) (GetVersionMetadataOutput, error) {
	platform := input.Platform
	if platform == "" {
		platform = PlatformIPhone
	}

	switch platform {
	case PlatformIPhone, PlatformIPad, PlatformAppleTV, PlatformVisionOS, PlatformMacOS:
	default:
		return GetVersionMetadataOutput{}, fmt.Errorf("invalid platform %q", platform)
	}

	macAddr, err := t.machine.MacAddress()
	if err != nil {
		return GetVersionMetadataOutput{}, fmt.Errorf("failed to get mac address: %w", err)
	}

	guid := strings.ReplaceAll(strings.ToUpper(macAddr), ":", "")

	res, _, err := t.sendDownloadProduct(input.Account, input.App, guid, input.VersionID, platform)
	if err != nil {
		return GetVersionMetadataOutput{}, err
	}

	if res.Data.FailureType == FailureTypePasswordTokenExpired || res.Data.FailureType == FailureTypeSignInRequired {
		return GetVersionMetadataOutput{}, ErrPasswordTokenExpired
	}

	if res.Data.FailureType == FailureTypeLicenseNotFound {
		return GetVersionMetadataOutput{}, ErrLicenseRequired
	}

	if res.Data.CustomerMessage != "" && (res.Data.FailureType != "" || len(res.Data.Items) == 0) {
		return GetVersionMetadataOutput{}, NewErrorWithMetadata(fmt.Errorf("received error: %s", res.Data.CustomerMessage), res)
	}

	if res.Data.FailureType != "" {
		return GetVersionMetadataOutput{}, NewErrorWithMetadata(fmt.Errorf("received error: %s", res.Data.FailureType), res)
	}

	if len(res.Data.Items) == 0 {
		return GetVersionMetadataOutput{}, NewErrorWithMetadata(errors.New("invalid response"), res)
	}

	item := res.Data.Items[0]
	if platform == PlatformMacOS {
		packagePlatform, err := downloadPackagePlatform(platform, item)
		if err != nil {
			return GetVersionMetadataOutput{}, err
		}

		if packagePlatform == PlatformMacOS {
			_, hardwareID, err := machineIdentity(macAddr)
			if err != nil {
				return GetVersionMetadataOutput{}, err
			}

			metadata, err := t.readVersionMetadataFromMacPackage(input.Context, item, hardwareID, input.App.BundleID)
			if err != nil {
				return GetVersionMetadataOutput{}, fmt.Errorf("failed to read macOS version metadata: %w", err)
			}

			return GetVersionMetadataOutput(metadata), nil
		}
	}

	// Do not fall back to item.Metadata here. The App Store download API can
	// return stale version and release date values, so the IPA Info.plist is the
	// source of truth and failures should be visible to callers.
	metadata, err := t.readVersionMetadataFromIPA(item.URL)
	if err != nil {
		return GetVersionMetadataOutput{}, fmt.Errorf("failed to read version metadata: %w", err)
	}

	return GetVersionMetadataOutput(metadata), nil
}
