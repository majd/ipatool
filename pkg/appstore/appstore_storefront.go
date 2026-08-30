package appstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

const (
	serializedServerDataID   = "serialized-server-data"
	maxVisionOSSearchResults = 12
)

type storefrontSearchPage struct {
	Data []struct {
		Data struct {
			Shelves []struct {
				Items []json.RawMessage `json:"items,omitempty"`
			} `json:"shelves,omitempty"`
		} `json:"data,omitempty"`
	} `json:"data,omitempty"`
}

type storefrontSearchItem struct {
	Kind   string          `json:"$kind,omitempty"`
	Lockup json.RawMessage `json:"lockup,omitempty"`
}

type storefrontAppLockup struct {
	AdamID   string `json:"adamId,omitempty"`
	BundleID string `json:"bundleId,omitempty"`
	Title    string `json:"title,omitempty"`
}

func serializedServerData(body []byte) ([]byte, error) {
	markerIndex := bytes.Index(body, []byte(`id="`+serializedServerDataID+`"`))
	if markerIndex == -1 {
		markerIndex = bytes.Index(body, []byte(`id='`+serializedServerDataID+`'`))
	}

	if markerIndex == -1 {
		return nil, errors.New("serialized server data was not found")
	}

	scriptStart := bytes.LastIndex(body[:markerIndex], []byte("<script"))
	if scriptStart == -1 {
		return nil, errors.New("serialized server data script was not found")
	}

	contentOffset := bytes.IndexByte(body[markerIndex:], '>')
	if contentOffset == -1 {
		return nil, errors.New("serialized server data script is malformed")
	}

	contentStart := markerIndex + contentOffset + 1

	contentEndOffset := bytes.Index(body[contentStart:], []byte("</script>"))
	if contentEndOffset == -1 {
		return nil, errors.New("serialized server data script is not closed")
	}

	return bytes.TrimSpace(body[contentStart : contentStart+contentEndOffset]), nil
}

func storefrontVisionApps(body []byte, limit int64) ([]App, error) {
	data, err := serializedServerData(body)
	if err != nil {
		return nil, err
	}

	var page storefrontSearchPage
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, fmt.Errorf("failed to decode serialized server data: %w", err)
	}

	if limit <= 0 {
		return []App{}, nil
	}

	limit = min(limit, maxVisionOSSearchResults)

	apps := make([]App, 0, int(limit))
	seen := make(map[int64]struct{})

	for _, pageData := range page.Data {
		for _, shelf := range pageData.Data.Shelves {
			for _, rawItem := range shelf.Items {
				var item storefrontSearchItem
				if err := json.Unmarshal(rawItem, &item); err != nil || item.Kind != "AppSearchResult" {
					continue
				}

				var lockup storefrontAppLockup
				if err := json.Unmarshal(item.Lockup, &lockup); err != nil {
					continue
				}

				appID, err := strconv.ParseInt(lockup.AdamID, 10, 64)
				if err != nil || appID == 0 {
					continue
				}

				if _, ok := seen[appID]; ok {
					continue
				}

				if !containsVisionPurchaseConfiguration(rawItem, appID) {
					continue
				}

				seen[appID] = struct{}{}

				apps = append(apps, App{
					ID:       appID,
					BundleID: lockup.BundleID,
					Name:     lockup.Title,
				})

				if int64(len(apps)) == limit {
					return apps, nil
				}
			}
		}
	}

	return apps, nil
}

func containsVisionPurchaseConfiguration(data []byte, appID int64) bool {
	var value interface{}
	if err := json.Unmarshal(data, &value); err != nil {
		return false
	}

	_, found := findVisionExternalVersionID(value, appID)

	return found
}

func visionExternalVersionID(body []byte, appID int64) (string, error) {
	data, err := serializedServerData(body)
	if err != nil {
		return "", err
	}

	var value interface{}
	if err := json.Unmarshal(data, &value); err != nil {
		return "", fmt.Errorf("failed to decode serialized server data: %w", err)
	}

	externalVersionID, found := findVisionExternalVersionID(value, appID)
	if !found {
		return "", errors.New("visionOS purchase configuration was not found")
	}

	if externalVersionID == "" {
		return "", errors.New("visionOS purchase configuration has no external version id")
	}

	return externalVersionID, nil
}

func findVisionExternalVersionID(value interface{}, appID int64) (string, bool) {
	matched := false

	switch typedValue := value.(type) {
	case []interface{}:
		for _, item := range typedValue {
			if externalVersionID, found := findVisionExternalVersionID(item, appID); found {
				if externalVersionID != "" {
					return externalVersionID, true
				}

				matched = true
			}
		}
	case map[string]interface{}:
		if configuration, ok := typedValue["purchaseConfiguration"].(map[string]interface{}); ok {
			if externalVersionID, found := externalVersionIDFromVisionConfiguration(configuration, appID); found {
				if externalVersionID != "" {
					return externalVersionID, true
				}

				matched = true
			}
		}

		for _, child := range typedValue {
			if externalVersionID, found := findVisionExternalVersionID(child, appID); found {
				if externalVersionID != "" {
					return externalVersionID, true
				}

				matched = true
			}
		}
	}

	return "", matched
}

func externalVersionIDFromVisionConfiguration(configuration map[string]interface{}, appID int64) (string, bool) {
	if configuration["metricsPlatformDisplayStyle"] != "vision" {
		return "", false
	}

	platforms, ok := configuration["appPlatforms"].([]interface{})
	if !ok {
		return "", false
	}

	isVisionApp := false

	for _, platform := range platforms {
		if platform == "vision" {
			isVisionApp = true

			break
		}
	}

	if !isVisionApp {
		return "", false
	}

	buyParams, ok := configuration["buyParams"].(string)
	if !ok || buyParams == "" {
		return "", false
	}

	values, err := url.ParseQuery(buyParams)
	if err != nil || values.Get("salableAdamId") != strconv.FormatInt(appID, 10) {
		return "", false
	}

	return values.Get("appExtVrsId"), true
}

func visionSearchURL(term, countryCode string) string {
	params := url.Values{}
	params.Set("term", term)

	return fmt.Sprintf("https://apps.apple.com/%s/vision/search?%s", strings.ToLower(countryCode), params.Encode())
}

func visionProductURL(appID int64, countryCode string) string {
	params := url.Values{}
	params.Set("platform", "vision")

	return fmt.Sprintf("https://apps.apple.com/%s/app/id%d?%s", strings.ToLower(countryCode), appID, params.Encode())
}
