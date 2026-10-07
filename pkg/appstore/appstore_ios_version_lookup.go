package appstore

import (
	"errors"
	"fmt"
	gohttp "net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/majd/ipatool/v2/pkg/http"
)

type catalogVersionLookupItem struct {
	ID         string `json:"id,omitempty"`
	Type       string `json:"type,omitempty"`
	Attributes struct {
		DeviceFamilies     []string                                    `json:"deviceFamilies,omitempty"`
		PlatformAttributes map[string]catalogVersionPlatformAttributes `json:"platformAttributes,omitempty"`
	} `json:"attributes,omitempty"`
}

type catalogVersionPlatformAttributes struct {
	BundleID          string                    `json:"bundleId,omitempty"`
	ExternalVersionID platformVersionExternalID `json:"externalVersionId,omitempty"`
}

func (t *appstore) lookupLatestIOSExternalVersionID(app App, countryCode string, platform Platform) (string, error) {
	// This is the JSON catalog API used by the public App Store client.
	res, err := t.platformClient.Send(http.Request{
		URL:            fmt.Sprintf("https://apps.apple.com/api/apps/v1/catalog/%s/apps/%d?platform=%s", strings.ToLower(countryCode), app.ID, platform),
		Method:         http.MethodGET,
		ResponseFormat: http.ResponseFormatJSON,
	})
	if err != nil {
		return "", fmt.Errorf("iOS catalog lookup request failed: %w", err)
	}

	if res.StatusCode != gohttp.StatusOK {
		return "", fmt.Errorf("iOS catalog lookup returned HTTP %d", res.StatusCode)
	}

	if len(res.Data.Data) != 1 {
		return "", errors.New("iOS catalog lookup must return exactly one app")
	}

	item := res.Data.Data[0]
	if item.ID != strconv.FormatInt(app.ID, 10) || item.Type != "apps" {
		return "", errors.New("iOS catalog lookup returned a different app")
	}

	attributes, ok := item.Attributes.PlatformAttributes["ios"]
	if !ok || !slices.Contains(item.Attributes.DeviceFamilies, string(platform)) {
		return "", errors.New("iOS catalog lookup returned no metadata for the requested platform")
	}

	if app.BundleID != "" && attributes.BundleID != app.BundleID {
		return "", errors.New("iOS catalog lookup returned a different bundle identifier")
	}

	version := string(attributes.ExternalVersionID)
	if id, err := strconv.ParseUint(version, 10, 64); err != nil || id == 0 {
		return "", errors.New("iOS catalog lookup returned no valid external version id")
	}

	return version, nil
}
