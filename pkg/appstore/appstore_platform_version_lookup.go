package appstore

import (
	"encoding/json"
	"errors"
	"fmt"
	gohttp "net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/majd/ipatool/v2/pkg/http"
)

type platformVersionLookupResult struct {
	Results map[string]platformVersionLookupItem `json:"results,omitempty"`
}

type platformVersionLookupItem struct {
	BundleID string                       `json:"bundleId,omitempty"`
	Name     string                       `json:"name,omitempty"`
	Offers   []platformVersionLookupOffer `json:"offers,omitempty"`
}

type platformVersionLookupOffer struct {
	BuyParams string                       `json:"buyParams,omitempty"`
	Version   platformVersionLookupVersion `json:"version,omitempty"`
}

type platformVersionLookupVersion struct {
	Display    string                    `json:"display,omitempty"`
	ExternalID platformVersionExternalID `json:"externalId,omitempty"`
}

type platformVersionExternalID string

func (id *platformVersionExternalID) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		return nil
	}

	var stringID string
	if err := json.Unmarshal(data, &stringID); err == nil {
		*id = platformVersionExternalID(stringID)

		return nil
	}

	var numberID json.Number
	if err := json.Unmarshal(data, &numberID); err == nil {
		*id = platformVersionExternalID(numberID.String())

		return nil
	}

	return fmt.Errorf("invalid external version id %s", string(data))
}

func (t *appstore) lookupLatestExternalVersionID(acc Account, app App, platform Platform) (string, error) {
	if app.ID == 0 {
		return "", errors.New("app ID is required for platform version lookup")
	}

	countryCode, err := countryCodeFromStoreFront(acc.StoreFront)
	if err != nil {
		return "", fmt.Errorf("failed to resolve the country code: %w", err)
	}

	request, err := t.platformVersionLookupRequest(app.ID, countryCode, platform)
	if err != nil {
		return "", fmt.Errorf("failed to create platform version lookup request: %w", err)
	}

	res, err := t.platformClient.Send(request)
	if err != nil {
		return "", fmt.Errorf("platform version lookup request failed: %w", err)
	}

	if res.StatusCode != gohttp.StatusOK {
		return "", NewErrorWithMetadata(errors.New("platform version lookup request failed"), res)
	}

	item, ok := res.Data.Results[strconv.FormatInt(app.ID, 10)]
	if !ok {
		return "", NewErrorWithMetadata(errors.New("platform version lookup returned no app"), res)
	}

	if len(item.Offers) == 0 {
		return "", NewErrorWithMetadata(errors.New("platform version lookup returned no offers"), res)
	}

	offer := item.Offers[0]
	externalVersionID := string(offer.Version.ExternalID)

	if externalVersionID == "" {
		externalVersionID, err = externalVersionIDFromBuyParams(offer.BuyParams)
		if err != nil {
			return "", fmt.Errorf("failed to parse buy params: %w", err)
		}
	}

	if externalVersionID == "" {
		return "", NewErrorWithMetadata(errors.New("platform version lookup returned no external version id"), res)
	}

	return externalVersionID, nil
}

func (*appstore) platformVersionLookupRequest(appID int64, countryCode string, platform Platform) (http.Request, error) {
	metadataPlatform, err := platform.metadataPlatform()
	if err != nil {
		return http.Request{}, err
	}

	params := url.Values{}
	params.Add("version", "2")
	params.Add("id", strconv.FormatInt(appID, 10))
	params.Add("p", "mdm-lockup")
	params.Add("caller", "MDM")
	params.Add("platform", metadataPlatform)
	params.Add("cc", strings.ToLower(countryCode))
	params.Add("l", "en")

	return http.Request{
		URL:            fmt.Sprintf("https://uclient-api.itunes.apple.com/WebObjects/MZStorePlatform.woa/wa/lookup?%s", params.Encode()),
		Method:         http.MethodGET,
		ResponseFormat: http.ResponseFormatJSON,
	}, nil
}

func externalVersionIDFromBuyParams(buyParams string) (string, error) {
	if buyParams == "" {
		return "", nil
	}

	values, err := url.ParseQuery(buyParams)
	if err != nil {
		return "", fmt.Errorf("failed to parse query: %w", err)
	}

	return values.Get("appExtVrsId"), nil
}
