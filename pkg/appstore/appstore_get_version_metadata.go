package appstore

import (
	"fmt"
	"strings"
	"time"
)

type GetVersionMetadataInput struct {
	Account            Account
	App                App
	VersionID          string
	RedownloadEndpoint string
}

type GetVersionMetadataOutput struct {
	DisplayVersion string
	ReleaseDate    time.Time
}

func (t *appstore) GetVersionMetadata(input GetVersionMetadataInput) (GetVersionMetadataOutput, error) {
	macAddr, err := t.machine.MacAddress()
	if err != nil {
		return GetVersionMetadataOutput{}, fmt.Errorf("failed to get mac address: %w", err)
	}

	guid := strings.ReplaceAll(strings.ToUpper(macAddr), ":", "")

	res, err := t.sendDownloadProduct(input.Account, input.App, guid, input.VersionID, input.RedownloadEndpoint)
	if err != nil {
		return GetVersionMetadataOutput{}, err
	}

	item, err := downloadProductItem(res)
	if err != nil {
		return GetVersionMetadataOutput{}, err
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
