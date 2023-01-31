package appstore

import (
	"fmt"
	"github.com/majd/ipatool/pkg/http"
	"github.com/pkg/errors"
	"net/url"
)

func (a *appstore) lookup(bundleOrAppID any, countryCode string) (App, error) {
	if StoreFronts[countryCode] == "" {
		return App{}, ErrInvalidCountryCode
	}

	request := a.lookupRequest(bundleOrAppID, countryCode)

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

func (a *appstore) lookupRequest(bundleOrAppID any, countryCode string) http.Request {
	return http.Request{
		URL:            a.lookupURL(bundleOrAppID, countryCode),
		Method:         http.MethodGET,
		ResponseFormat: http.ResponseFormatJSON,
	}
}

func (a *appstore) lookupURL(bundleOrAppID any, countryCode string) string {
	params := url.Values{}
	if appID, ok := bundleOrAppID.(int64); ok {
		params.Add("id", fmt.Sprintf("%d", appID))
	} else {
		params.Add("bundleId", bundleOrAppID.(string))
	}
	params.Add("entity", "software,iPadSoftware")
	params.Add("limit", "1")
	params.Add("media", "software")
	params.Add("country", countryCode)

	return fmt.Sprintf("https://%s%s?%s", iTunesAPIDomain, iTunesAPIPathLookup, params.Encode())
}
