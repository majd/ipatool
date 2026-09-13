package appstore

import (
	"encoding/json"
	"errors"
	"fmt"
	gohttp "net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/majd/ipatool/v2/pkg/http"
)

func (t *appstore) lookupLatestMacOSExternalVersionID(acc Account, app App) (string, error) {
	countryCode, err := countryCodeFromStoreFront(acc.StoreFront)
	if err != nil {
		return "", fmt.Errorf("failed to resolve the country code: %w", err)
	}

	// The legacy MDM lookup can return an iOS offer even with platform=osx.
	// Use the Mac product page to select the native Mac offer instead.
	res, err := t.storefrontClient.Send(http.Request{
		URL:            fmt.Sprintf("https://apps.apple.com/%s/app/id%d?platform=mac", strings.ToLower(countryCode), app.ID),
		Method:         http.MethodGET,
		ResponseFormat: http.ResponseFormatRaw,
	})
	if err != nil {
		return "", fmt.Errorf("macOS version lookup request failed: %w", err)
	}

	if res.StatusCode != gohttp.StatusOK {
		return "", fmt.Errorf("macOS version lookup returned HTTP %d", res.StatusCode)
	}

	return macOSExternalVersionID(res.Data, app)
}

func macOSExternalVersionID(body []byte, app App) (string, error) {
	data, err := serializedServerData(body)
	if err != nil {
		return "", err
	}

	var value interface{}
	if err := json.Unmarshal(data, &value); err != nil {
		return "", fmt.Errorf("failed to decode Mac product page: %w", err)
	}

	versions := make(map[string]struct{})
	collectMacOSExternalVersions(value, app, versions)

	if len(versions) == 0 {
		return "", errors.New("macOS purchase configuration has no external version id for the requested app")
	}

	if len(versions) != 1 {
		return "", errors.New("macOS purchase configurations contain conflicting external version ids")
	}

	for version := range versions {
		return version, nil
	}

	return "", errors.New("macOS external version id was not found")
}

func collectMacOSExternalVersions(value interface{}, app App, versions map[string]struct{}) {
	switch value := value.(type) {
	case []interface{}:
		for _, child := range value {
			collectMacOSExternalVersions(child, app, versions)
		}
	case map[string]interface{}:
		if configuration, ok := value["purchaseConfiguration"].(map[string]interface{}); ok {
			platforms, _ := configuration["appPlatforms"].([]interface{})
			bundleID, _ := configuration["bundleId"].(string)
			buyParams, _ := configuration["buyParams"].(string)

			params, err := url.ParseQuery(buyParams)
			if err == nil && slices.Contains(platforms, interface{}("mac")) &&
				params.Get("salableAdamId") == strconv.FormatInt(app.ID, 10) &&
				(app.BundleID == "" || app.BundleID == bundleID) {
				version := params.Get("appExtVrsId")
				if id, err := strconv.ParseUint(version, 10, 64); err == nil && id != 0 {
					versions[version] = struct{}{}
				}
			}
		}

		for _, child := range value {
			collectMacOSExternalVersions(child, app, versions)
		}
	}
}
