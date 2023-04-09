package appstore

import (
	"fmt"
	"github.com/majd/ipatool/pkg/http"
	"github.com/pkg/errors"
	"net/url"
	"strconv"
)

type SearchResult struct {
	Count   int   `json:"resultCount,omitempty"`
	Results []App `json:"results,omitempty"`
}

type SearchOutput = SearchResult

func (a *appstore) Search(term string, limit int64) (SearchOutput, error) {
	acc, err := a.account()
	if err != nil {
		return SearchOutput{}, errors.Wrap(err, ErrGetAccount.Error())
	}

	countryCode, err := a.countryCodeFromStoreFront(acc.StoreFront)
	if err != nil {
		return SearchOutput{}, errors.Wrap(err, ErrInvalidCountryCode.Error())
	}

	request := a.searchRequest(term, countryCode, limit)

	res, err := a.searchClient.Send(request)
	if err != nil {
		return SearchOutput{}, errors.Wrap(err, ErrRequest.Error())
	}

	if res.StatusCode != 200 {
		a.logger.Verbose().Interface("data", res.Data).Int("status", res.StatusCode).Send()
		return SearchOutput{}, ErrRequest
	}

	return res.Data, nil
}

func (a *appstore) searchRequest(term, countryCode string, limit int64) http.Request {
	return http.Request{
		URL:            a.searchURL(term, countryCode, limit),
		Method:         http.MethodGET,
		ResponseFormat: http.ResponseFormatJSON,
	}
}

func (a *appstore) searchURL(term, countryCode string, limit int64) string {
	params := url.Values{}
	params.Add("entity", "software,iPadSoftware")
	params.Add("limit", strconv.Itoa(int(limit)))
	params.Add("media", "software")
	params.Add("term", term)
	params.Add("country", countryCode)

	return fmt.Sprintf("https://%s%s?%s", iTunesAPIDomain, iTunesAPIPathSearch, params.Encode())
}
