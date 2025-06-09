package appstore

import (
	"errors"
	"fmt"
	"strings"

	"github.com/majd/ipatool/v2/pkg/http"
)

type ListVersionsInput struct {
	Account Account
	App     App
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

	req := t.listVersionsRequest(input.Account, input.App, guid)
	res, err := t.downloadClient.Send(req)

	if err != nil {
		return ListVersionsOutput{}, fmt.Errorf("failed to send http request: %w", err)
	}

	if res.Data.FailureType == FailureTypePasswordTokenExpired {
		return ListVersionsOutput{}, ErrPasswordTokenExpired
	}

	if res.Data.FailureType == FailureTypeLicenseNotFound {
		return ListVersionsOutput{}, ErrLicenseRequired
	}

	if res.Data.FailureType != "" && res.Data.CustomerMessage != "" {
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

func (t *appstore) listVersionsRequest(acc Account, app App, guid string) http.Request {
	host := fmt.Sprintf("%s-%s", PrivateAppStoreAPIDomainPrefixWithoutAuthCode, PrivateAppStoreAPIDomain)

	payload := map[string]interface{}{
		"creditDisplay": "",
		"guid":          guid,
		"salableAdamId": app.ID,
	}

	return http.Request{
		URL:            fmt.Sprintf("https://%s%s?guid=%s", host, PrivateAppStoreAPIPathDownload, guid),
		Method:         http.MethodPOST,
		ResponseFormat: http.ResponseFormatXML,
		Headers: map[string]string{
			"Content-Type": "application/x-apple-plist",
			"iCloud-DSID":  acc.DirectoryServicesID,
			"X-Dsid":       acc.DirectoryServicesID,
		},
		Payload: &http.XMLPayload{
			Content: payload,
		},
	}
}
