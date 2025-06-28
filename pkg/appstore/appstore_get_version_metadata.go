package appstore

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/majd/ipatool/v2/pkg/http"
)

type GetVersionMetadataInput struct {
	Account   Account
	App       App
	VersionID string
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

	req := t.getVersionMetadataRequest(input.Account, input.App, guid, input.VersionID)
	res, err := t.downloadClient.Send(req)

	if err != nil {
		return GetVersionMetadataOutput{}, fmt.Errorf("failed to send http request: %w", err)
	}

	if res.Data.FailureType == FailureTypePasswordTokenExpired {
		return GetVersionMetadataOutput{}, ErrPasswordTokenExpired
	}

	if res.Data.FailureType == FailureTypeLicenseNotFound {
		return GetVersionMetadataOutput{}, ErrLicenseRequired
	}

	if res.Data.FailureType != "" && res.Data.CustomerMessage != "" {
		return GetVersionMetadataOutput{}, NewErrorWithMetadata(fmt.Errorf("received error: %s", res.Data.CustomerMessage), res)
	}

	if res.Data.FailureType != "" {
		return GetVersionMetadataOutput{}, NewErrorWithMetadata(fmt.Errorf("received error: %s", res.Data.FailureType), res)
	}

	if len(res.Data.Items) == 0 {
		return GetVersionMetadataOutput{}, NewErrorWithMetadata(errors.New("invalid response"), res)
	}

	item := res.Data.Items[0]

	releaseDate, err := time.Parse(time.RFC3339, fmt.Sprintf("%v", item.Metadata["releaseDate"]))
	if err != nil {
		return GetVersionMetadataOutput{}, fmt.Errorf("failed to parse release date: %w", err)
	}

	return GetVersionMetadataOutput{
		DisplayVersion: fmt.Sprintf("%v", item.Metadata["bundleShortVersionString"]),
		ReleaseDate:    releaseDate,
	}, nil
}

func (t *appstore) getVersionMetadataRequest(acc Account, app App, guid string, version string) http.Request {
	host := fmt.Sprintf("%s-%s", PrivateAppStoreAPIDomainPrefixWithoutAuthCode, PrivateAppStoreAPIDomain)

	payload := map[string]interface{}{
		"creditDisplay":     "",
		"guid":              guid,
		"salableAdamId":     app.ID,
		"externalVersionId": version,
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
