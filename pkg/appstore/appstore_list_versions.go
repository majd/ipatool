package appstore

import (
	"fmt"
	"strings"
)

type ListVersionsInput struct {
	Account            Account
	App                App
	RedownloadEndpoint string
}

type ListVersionsOutput struct {
	ExternalVersionIdentifiers []string
	LatestExternalVersionID    string
}

func (t *appstore) ListVersions(input ListVersionsInput) (ListVersionsOutput, error) {
	macAddr, err := t.machine.MacAddress()
	if err != nil {
		return ListVersionsOutput{}, fmt.Errorf("failed to get mac address: %w", err)
	}

	guid := strings.ReplaceAll(strings.ToUpper(macAddr), ":", "")

	res, err := t.sendDownloadProduct(input.Account, input.App, guid, "", input.RedownloadEndpoint)
	if err != nil {
		return ListVersionsOutput{}, err
	}

	item, err := downloadProductItem(res)
	if err != nil {
		return ListVersionsOutput{}, err
	}

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
