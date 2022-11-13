package appstore

import (
	"fmt"
	"github.com/majd/ipatool/pkg/http"
	"github.com/pkg/errors"
	"net/url"
)

func (a *appstore) lookup(bundleID, countryCode, deviceFamily string) (App, error) {
	if StoreFront[countryCode] == "" {
		return App{}, errors.New("invalid country code")
	}

	request, err := a.lookupRequest(bundleID, countryCode, deviceFamily)
	if err != nil {
		return App{}, errors.Wrap(err, "failed to get lookup request")
	}

	res, err := a.searchClient.Send(request)
	if err != nil {
		return App{}, errors.Wrap(err, "lookup request failed")
	}

	if res.StatusCode != 200 {
		a.logger.Debug().
			Interface("data", res.Data).
			Int("status", res.StatusCode).
			Send()
		return App{}, errors.Errorf("lookup request failed with status %d", res.StatusCode)
	}

	if len(res.Data.Results) == 0 {
		return App{}, errors.New("app not found")
	}

	return res.Data.Results[0], nil
}

func (a *appstore) lookupRequest(bundleID, countryCode, deviceFamily string) (http.Request, error) {
	lookupURL, err := a.lookupURL(bundleID, countryCode, deviceFamily)
	if err != nil {
		return http.Request{}, errors.Wrap(err, "failed to get lookup URL")
	}

	return http.Request{
		URL:            lookupURL,
		Method:         http.MethodGET,
		ResponseFormat: http.ResponseFormatJSON,
	}, nil
}

func (a *appstore) lookupURL(bundleID, countryCode, deviceFamily string) (string, error) {
	var entity string

	switch deviceFamily {
	case DeviceFamilyPhone:
		entity = "software"
	case DeviceFamilyPad:
		entity = "iPadSoftware"
	default:
		return "", errors.Errorf("device family is not supported: %s", deviceFamily)
	}

	params := url.Values{}
	params.Add("entity", entity)
	params.Add("limit", "1")
	params.Add("media", "software")
	params.Add("bundleId", bundleID)
	params.Add("country", countryCode)

	return fmt.Sprintf("https://%s%s?%s", iTunesAPIDomain, iTunesAPIPathLookup, params.Encode()), nil
}
