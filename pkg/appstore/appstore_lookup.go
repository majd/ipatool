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
}

type LookupOutput struct {
	App App
}

func (t *appstore) Lookup(input LookupInput) (LookupOutput, error) {
	countryCode, err := countryCodeFromStoreFront(input.Account.StoreFront)
	if err != nil {
		return LookupOutput{}, fmt.Errorf("failed to resolve the country code: %w", err)
	}

	request := t.lookupRequest(input.BundleID, countryCode)

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

func (t *appstore) lookupRequest(bundleID, countryCode string) http.Request {
	return http.Request{
		URL:            t.lookupURL(bundleID, countryCode),
		Method:         http.MethodGET,
		ResponseFormat: http.ResponseFormatJSON,
	}
}

func (t *appstore) lookupURL(bundleID, countryCode string) string {
	params := url.Values{}
	params.Add("entity", "software,iPadSoftware")
	params.Add("limit", "1")
	params.Add("media", "software")
	params.Add("bundleId", bundleID)
	params.Add("country", countryCode)

	return fmt.Sprintf("https://%s%s?%s", iTunesAPIDomain, iTunesAPIPathLookup, params.Encode())
}
