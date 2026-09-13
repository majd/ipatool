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

type SearchInput struct {
	Account  Account
	Term     string
	Limit    int64
	Platform Platform
}

type SearchOutput struct {
	Count   int
	Results []App
}

func (t *appstore) Search(input SearchInput) (SearchOutput, error) {
	countryCode, err := countryCodeFromStoreFront(input.Account.StoreFront)
	if err != nil {
		return SearchOutput{}, fmt.Errorf("country code is invalid: %w", err)
	}

	if input.Platform == PlatformVisionOS {
		return t.searchVisionOS(input, countryCode)
	}

	request, err := t.searchRequest(input.Term, countryCode, input.Limit, input.Platform)
	if err != nil {
		return SearchOutput{}, fmt.Errorf("failed to create search request: %w", err)
	}

	res, err := t.searchClient.Send(request)
	if err != nil {
		return SearchOutput{}, fmt.Errorf("request failed: %w", err)
	}

	if res.StatusCode != gohttp.StatusOK {
		return SearchOutput{}, NewErrorWithMetadata(errors.New("request failed"), res)
	}

	return SearchOutput{
		Count:   res.Data.Count,
		Results: res.Data.Results,
	}, nil
}

func (t *appstore) searchVisionOS(input SearchInput, countryCode string) (SearchOutput, error) {
	request := http.Request{
		URL:            visionSearchURL(input.Term, countryCode),
		Method:         http.MethodGET,
		ResponseFormat: http.ResponseFormatRaw,
	}

	res, err := t.storefrontClient.Send(request)
	if err != nil {
		return SearchOutput{}, fmt.Errorf("visionOS search request failed: %w", err)
	}

	if res.StatusCode != gohttp.StatusOK {
		return SearchOutput{}, NewErrorWithMetadata(errors.New("visionOS search request failed"), res)
	}

	storefrontApps, err := storefrontVisionApps(res.Data, input.Limit)
	if err != nil {
		return SearchOutput{}, fmt.Errorf("failed to parse visionOS search results: %w", err)
	}

	if len(storefrontApps) == 0 {
		return SearchOutput{Results: []App{}}, nil
	}

	ids := make([]string, 0, len(storefrontApps))
	for _, app := range storefrontApps {
		ids = append(ids, strconv.FormatInt(app.ID, 10))
	}

	lookupRequest, err := t.lookupIDsRequest(ids, countryCode, PlatformVisionOS)
	if err != nil {
		return SearchOutput{}, fmt.Errorf("failed to create visionOS metadata request: %w", err)
	}

	lookupResponse, err := t.searchClient.Send(lookupRequest)
	if err != nil {
		return SearchOutput{}, fmt.Errorf("visionOS metadata request failed: %w", err)
	}

	if lookupResponse.StatusCode != gohttp.StatusOK {
		return SearchOutput{}, NewErrorWithMetadata(errors.New("visionOS metadata request failed"), lookupResponse)
	}

	hydrated := make(map[int64]App, len(lookupResponse.Data.Results))
	for _, app := range lookupResponse.Data.Results {
		hydrated[app.ID] = app
	}

	for index, app := range storefrontApps {
		if metadata, ok := hydrated[app.ID]; ok {
			storefrontApps[index] = metadata
		}

		platforms := slices.DeleteFunc(storefrontApps[index].Platforms, func(platform Platform) bool {
			return platform == PlatformUnknown
		})
		if !slices.Contains(platforms, PlatformVisionOS) {
			platforms = append(platforms, PlatformVisionOS)
		}

		storefrontApps[index].Platforms = platforms
	}

	return SearchOutput{
		Count:   len(storefrontApps),
		Results: storefrontApps,
	}, nil
}

type searchResult struct {
	Count   int   `json:"resultCount,omitempty"`
	Results []App `json:"results,omitempty"`
}

// UnmarshalJSON derives platforms from catalog metadata without exposing the
// catalog's device list in app output.
func (r *searchResult) UnmarshalJSON(data []byte) error {
	var result struct {
		Count   int `json:"resultCount"`
		Results []struct {
			App
			Kind             string   `json:"kind"`
			SupportedDevices []string `json:"supportedDevices"`
		} `json:"results"`
	}

	if err := json.Unmarshal(data, &result); err != nil {
		return fmt.Errorf("failed to decode search results: %w", err)
	}

	r.Count = result.Count
	r.Results = nil

	if result.Results != nil {
		r.Results = make([]App, 0, len(result.Results))
	}

	for _, item := range result.Results {
		platforms := item.Platforms

		for _, family := range []struct {
			prefix   string
			platform Platform
		}{
			{"iPhone", PlatformIPhone},
			{"iPod", PlatformIPhone},
			{"iPad", PlatformIPad},
			{"AppleTV", PlatformAppleTV},
			{"RealityDevice", PlatformVisionOS},
			{"Mac", PlatformMacOS},
		} {
			for _, device := range item.SupportedDevices {
				if strings.HasPrefix(device, family.prefix) && !slices.Contains(platforms, family.platform) {
					platforms = append(platforms, family.platform)
				}
			}
		}

		if item.Kind == "mac-software" && !slices.Contains(platforms, PlatformMacOS) {
			platforms = append(platforms, PlatformMacOS)
		}

		if len(platforms) == 0 {
			platforms = []Platform{PlatformUnknown}
		}

		item.Platforms = platforms
		r.Results = append(r.Results, item.App)
	}

	return nil
}

func (t *appstore) searchRequest(term, countryCode string, limit int64, platform Platform) (http.Request, error) {
	url, err := t.searchURL(term, countryCode, limit, platform)
	if err != nil {
		return http.Request{}, err
	}

	return http.Request{
		URL:            url,
		Method:         http.MethodGET,
		ResponseFormat: http.ResponseFormatJSON,
	}, nil
}

func (t *appstore) searchURL(term, countryCode string, limit int64, platform Platform) (string, error) {
	entity, err := platform.searchEntity()
	if err != nil {
		return "", err
	}

	params := url.Values{}
	params.Add("entity", entity)
	params.Add("limit", strconv.Itoa(int(limit)))
	params.Add("media", "software")
	params.Add("term", term)
	params.Add("country", countryCode)

	return fmt.Sprintf("https://%s%s?%s", iTunesAPIDomain, iTunesAPIPathSearch, params.Encode()), nil
}

func (*appstore) lookupIDsRequest(ids []string, countryCode string, platform Platform) (http.Request, error) {
	entity, err := platform.lookupEntity()
	if err != nil {
		return http.Request{}, err
	}

	params := url.Values{}
	params.Add("entity", entity)
	params.Add("id", strings.Join(ids, ","))
	params.Add("country", countryCode)

	return http.Request{
		URL:            fmt.Sprintf("https://%s%s?%s", iTunesAPIDomain, iTunesAPIPathLookup, params.Encode()),
		Method:         http.MethodGET,
		ResponseFormat: http.ResponseFormatJSON,
	}, nil
}
