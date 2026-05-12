package appstore

import (
	"errors"
	"fmt"
	gohttp "net/http"
	"net/url"

	"github.com/majd/ipatool/v2/pkg/http"
)

type LookupInput struct {
	Account  Account
	BundleID string
	Platform Platform
}

type LookupOutput struct {
	App App
}

func (t *appstore) Lookup(input LookupInput) (LookupOutput, error) {
	countryCode, err := countryCodeFromStoreFront(input.Account.StoreFront)
	if err != nil {
		return LookupOutput{}, fmt.Errorf("failed to resolve the country code: %w", err)
	}

	request, err := t.lookupRequest(input.BundleID, countryCode, input.Platform)
	if err != nil {
		return LookupOutput{}, fmt.Errorf("failed to create lookup request: %w", err)
	}

	res, err := t.searchClient.Send(request)
	if err != nil {
		return LookupOutput{}, fmt.Errorf("request failed: %w", err)
	}

	if res.StatusCode != gohttp.StatusOK {
		return LookupOutput{}, NewErrorWithMetadata(errors.New("invalid response"), res)
	}

	if len(res.Data.Results) == 0 {
		return LookupOutput{}, errors.New("app not found")
	}

	return LookupOutput{
		App: res.Data.Results[0],
	}, nil
}

func (t *appstore) lookupRequest(bundleID, countryCode string, platform Platform) (http.Request, error) {
	url, err := t.lookupURL(bundleID, countryCode, platform)
	if err != nil {
		return http.Request{}, err
	}

	return http.Request{
		URL:            url,
		Method:         http.MethodGET,
		ResponseFormat: http.ResponseFormatJSON,
	}, nil
}

func (t *appstore) lookupURL(bundleID, countryCode string, platform Platform) (string, error) {
	entity, err := platform.lookupEntity()
	if err != nil {
		return "", err
	}

	params := url.Values{}
	params.Add("entity", entity)
	params.Add("limit", "1")
	params.Add("media", "software")
	params.Add("bundleId", bundleID)
	params.Add("country", countryCode)

	return fmt.Sprintf("https://%s%s?%s", iTunesAPIDomain, iTunesAPIPathLookup, params.Encode()), nil
}
