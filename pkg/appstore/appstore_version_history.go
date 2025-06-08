package appstore

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/majd/ipatool/v2/pkg/http"
)

type VersionHistoryInput struct {
	Account          Account
	App              App
	MaxCount         int
	OldestFirst      bool
	AllVersions      bool
	AppInfoCallback  func(info VersionHistoryInfo)
	ProgressCallback func(index int, detail VersionDetails)
}

type VersionHistoryOutput struct {
	VersionHistory VersionHistoryInfo
	VersionDetails []VersionDetails
}

func (t *appstore) VersionHistory(input VersionHistoryInput) (VersionHistoryOutput, error) {
	macAddr, err := t.machine.MacAddress()
	if err != nil {
		return VersionHistoryOutput{}, fmt.Errorf("failed to get mac address: %w", err)
	}

	guid := strings.ReplaceAll(strings.ToUpper(macAddr), ":", "")

	req := t.versionHistoryRequest(input.Account, input.App, guid, "")
	res, err := t.downloadClient.Send(req)

	if err != nil {
		return VersionHistoryOutput{}, fmt.Errorf("failed to send http request: %w", err)
	}

	if res.Data.FailureType != "" {
		if res.Data.FailureType == FailureTypePasswordTokenExpired {
			return VersionHistoryOutput{}, ErrPasswordTokenExpired
		}

		if res.Data.CustomerMessage != "" {
			return VersionHistoryOutput{}, errors.New(res.Data.CustomerMessage)
		}

		return VersionHistoryOutput{}, fmt.Errorf("app store request failed: %s", res.Data.FailureType)
	}

	if len(res.Data.Items) == 0 {
		return VersionHistoryOutput{}, errors.New("no app data found")
	}

	item := res.Data.Items[0]

	appInfo := VersionHistoryInfo{
		App: App{
			ID:       input.App.ID,
			BundleID: input.App.BundleID,
		},
	}

	if name, ok := item.Metadata["bundleDisplayName"].(string); ok {
		appInfo.App.Name = name
	}

	if bundleID, ok := item.Metadata["bundleIdentifier"].(string); ok && bundleID != "" {
		appInfo.App.BundleID = bundleID
	}

	if version, ok := item.Metadata["bundleShortVersionString"].(string); ok {
		appInfo.LatestVersion = version
	}

	versionIdentifiersRaw, ok := item.Metadata["softwareVersionExternalIdentifiers"].([]interface{})
	if !ok {
		return VersionHistoryOutput{}, errors.New("softwareVersionExternalIdentifiers not found or invalid type")
	}

	var versionIdentifiers []string

	for _, v := range versionIdentifiersRaw {
		if versionInt, ok := v.(uint64); ok {
			versionIdentifiers = append(versionIdentifiers, fmt.Sprintf("%d", versionInt))
		}
	}

	appInfo.VersionIdentifiers = versionIdentifiers

	if input.AppInfoCallback != nil {
		input.AppInfoCallback(appInfo)
	}

	if len(versionIdentifiers) == 0 {
		return VersionHistoryOutput{
			VersionHistory: appInfo,
			VersionDetails: []VersionDetails{},
		}, nil
	}

	var selectedVersionIds []string

	if input.AllVersions {
		// Fetch all versions
		if input.OldestFirst {
			selectedVersionIds = versionIdentifiers
		} else {
			// Reverse the order for newest first
			selectedVersionIds = make([]string, len(versionIdentifiers))
			copy(selectedVersionIds, versionIdentifiers)

			for i, j := 0, len(selectedVersionIds)-1; i < j; i, j = i+1, j-1 {
				selectedVersionIds[i], selectedVersionIds[j] = selectedVersionIds[j], selectedVersionIds[i]
			}
		}
	} else {
		// Use maxCount limit
		maxCount := input.MaxCount
		if maxCount <= 0 {
			maxCount = 10
		}

		if input.OldestFirst {
			if len(versionIdentifiers) <= maxCount {
				selectedVersionIds = versionIdentifiers
			} else {
				selectedVersionIds = versionIdentifiers[:maxCount]
			}
		} else {
			if len(versionIdentifiers) <= maxCount {
				selectedVersionIds = make([]string, len(versionIdentifiers))
				copy(selectedVersionIds, versionIdentifiers)
			} else {
				selectedVersionIds = make([]string, maxCount)
				copy(selectedVersionIds, versionIdentifiers[len(versionIdentifiers)-maxCount:])
			}

			for i, j := 0, len(selectedVersionIds)-1; i < j; i, j = i+1, j-1 {
				selectedVersionIds[i], selectedVersionIds[j] = selectedVersionIds[j], selectedVersionIds[i]
			}
		}
	}

	versionDetails := t.fetchVersionDetails(input.Account, input.App, guid, selectedVersionIds, input.ProgressCallback)

	return VersionHistoryOutput{
		VersionHistory: appInfo,
		VersionDetails: versionDetails,
	}, nil
}

func (t *appstore) fetchVersionDetails(account Account, app App, guid string, versionIds []string, progressCallback func(index int, detail VersionDetails)) []VersionDetails {
	const maxConcurrent = 5

	type result struct {
		index   int
		details VersionDetails
	}

	results := make([]VersionDetails, len(versionIds))
	resultChan := make(chan result, len(versionIds))
	semaphore := make(chan struct{}, maxConcurrent)

	var wg sync.WaitGroup

	for i, versionId := range versionIds {
		wg.Add(1)

		go func(index int, id string) {
			defer wg.Done()

			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			details := VersionDetails{
				VersionID: id,
				Success:   false,
			}

			req := t.versionHistoryRequest(account, app, guid, id)
			res, err := t.downloadClient.Send(req)

			if err != nil {
				details.Error = fmt.Sprintf("request failed: %v", err)
			} else if res.Data.FailureType != "" {
				details.Error = fmt.Sprintf("API error: %s", res.Data.FailureType)
				if res.Data.CustomerMessage != "" {
					details.Error = fmt.Sprintf("API error: %s", res.Data.CustomerMessage)
				}
			} else if len(res.Data.Items) == 0 {
				details.Error = "no app data found"
			} else {
				item := res.Data.Items[0]
				if metadata, ok := item.Metadata["bundleShortVersionString"].(string); ok {
					details.VersionString = metadata
					details.Success = true
				} else {
					details.Error = "version string not found in metadata"
				}
			}

			resultChan <- result{index: index, details: details}
		}(i, versionId)
	}

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	for result := range resultChan {
		results[result.index] = result.details

		if progressCallback != nil {
			progressCallback(result.index, result.details)
		}
	}

	return results
}

func (t *appstore) versionHistoryRequest(acc Account, app App, guid string, versionId string) http.Request {
	host := fmt.Sprintf("%s-%s", PrivateAppStoreAPIDomainPrefixWithoutAuthCode, PrivateAppStoreAPIDomain)

	payload := map[string]interface{}{
		"creditDisplay": "",
		"guid":          guid,
		"salableAdamId": app.ID,
	}

	if versionId != "" {
		payload["externalVersionId"] = versionId
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
