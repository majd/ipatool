package appstore

import (
	"fmt"
	"github.com/majd/ipatool/pkg/http"
	"github.com/pkg/errors"
	"net/url"
)

func (a *appstore) lookup(bundleID, countryCode string) (App, error) {
	if StoreFronts[countryCode] == "" {
		return App{}, ErrInvalidCountryCode
	}

	request := a.lookupRequest(bundleID, countryCode)

	res, err := a.searchClient.Send(request)
	if err != nil {
		return App{}, errors.Wrap(err, ErrRequest.Error())
	}

	if res.StatusCode != 200 {
		a.logger.Verbose().Interface("data", res.Data).Int("status", res.StatusCode).Send()
		return App{}, ErrRequest
	}

	if len(res.Data.Results) == 0 {
		return App{}, ErrAppNotFound
	}

	return res.Data.Results[0], nil
}

func (a *appstore) lookupRequest(bundleID, countryCode string) http.Request {
	return http.Request{
		URL:            a.lookupURL(bundleID, countryCode),
		Method:         http.MethodGET,
		ResponseFormat: http.ResponseFormatJSON,
	}
}

func (a *appstore) lookupURL(bundleID, countryCode string) string {
	params := url.Values{}
	params.Add("entity", "software,iPadSoftware")
	params.Add("limit", "1")
	params.Add("media", "software")
	params.Add("bundleId", bundleID)
	params.Add("country", countryCode)

	return fmt.Sprintf("https://%s%s?%s", iTunesAPIDomain, iTunesAPIPathLookup, params.Encode())
}
