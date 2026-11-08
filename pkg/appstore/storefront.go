package appstore

import (
	"encoding/json"
	"errors"
	"fmt"
	gohttp "net/http"
	"strconv"
	"strings"

	"github.com/majd/ipatool/v2/pkg/http"
	"howett.net/plist"
)

const (
	availableStorefrontsURL = "https://itunes.apple.com/WebObjects/MZStore.woa/wa/availableStoreFronts"
	storefrontGenreURL      = "https://apps.apple.com/genre/id36"
	maxStorefrontIDRange    = 1000
)

type availableStorefrontsResult struct {
	Storefronts []availableStorefront `plist:"storefronts"`
}

type availableStorefront struct {
	CountryCode string `plist:"country-code"`
	ID          int64  `plist:"id"`
}

type storefrontGenrePage struct {
	StorePlatformData map[string]struct {
		Meta struct {
			Storefront struct {
				CountryCode string `json:"cc"`
				ID          string `json:"id"`
			} `json:"storefront"`
		} `json:"meta"`
	} `json:"storePlatformData"`
}

func (t *appstore) countryCodeFromStoreFront(storeFront string) (string, error) {
	if err := t.discoverAvailableStorefronts(); err != nil {
		return "", err
	}

	identifier := storefrontIdentifier(storeFront)
	if countryCode := t.cachedStorefrontCountryCode(identifier); countryCode != "" {
		return countryCode, nil
	}

	countryCode, found, err := t.discoverStorefrontCountryCode(identifier)
	if err != nil {
		return "", err
	}

	if found {
		return countryCode, nil
	}

	return "", storefrontMappingError(storeFront)
}

func (t *appstore) storefrontID(countryCode string) string {
	t.storefrontsMu.Lock()
	defer t.storefrontsMu.Unlock()

	for identifier, code := range t.storefrontCountryCodes {
		if code == countryCode {
			return identifier
		}
	}

	return ""
}

func (t *appstore) discoverStorefrontID(countryCode string) (string, error) {
	if err := t.discoverAvailableStorefronts(); err != nil {
		return "", err
	}

	countryCode = strings.ToUpper(strings.TrimSpace(countryCode))
	if identifier := t.storefrontID(countryCode); identifier != "" {
		return identifier, nil
	}

	identifiers, err := t.missingStorefrontIdentifiers()
	if err != nil {
		return "", err
	}

	for _, identifier := range identifiers {
		resolvedCountryCode, found, resolveErr := t.discoverStorefrontCountryCode(identifier)
		if resolveErr != nil {
			return "", resolveErr
		}

		if found && resolvedCountryCode == countryCode {
			return identifier, nil
		}
	}

	return "", fmt.Errorf("store front mapping for country code (%s) was not found", countryCode)
}

func (t *appstore) cachedStorefrontCountryCode(identifier string) string {
	t.storefrontsMu.Lock()
	defer t.storefrontsMu.Unlock()

	return t.storefrontCountryCodes[identifier]
}

func (t *appstore) missingStorefrontIdentifiers() ([]string, error) {
	t.storefrontsMu.Lock()
	defer t.storefrontsMu.Unlock()

	minimum, maximum := int64(0), int64(0)
	known := make(map[int64]struct{}, len(t.storefrontCountryCodes))

	for identifier := range t.storefrontCountryCodes {
		value, err := strconv.ParseInt(identifier, 10, 64)
		if err != nil || value <= 0 {
			continue
		}

		known[value] = struct{}{}

		if minimum == 0 || value < minimum {
			minimum = value
		}

		if value > maximum {
			maximum = value
		}
	}

	if minimum == 0 || maximum-minimum > maxStorefrontIDRange {
		return nil, errors.New("available storefront identifier range is invalid")
	}

	identifiers := make([]string, 0, maximum-minimum+1-int64(len(known)))

	for identifier := minimum; identifier <= maximum; identifier++ {
		if _, ok := known[identifier]; !ok {
			identifiers = append(identifiers, strconv.FormatInt(identifier, 10))
		}
	}

	return identifiers, nil
}

func (t *appstore) discoverStorefrontCountryCode(identifier string) (string, bool, error) {
	if _, err := strconv.ParseInt(identifier, 10, 64); err != nil {
		return "", false, nil
	}

	if t.storefrontClient == nil {
		return "", false, errors.New("storefront client is not configured")
	}

	res, err := t.storefrontClient.Send(http.Request{
		URL:            storefrontGenreURL,
		Method:         http.MethodGET,
		ResponseFormat: http.ResponseFormatRaw,
		NoRedirects:    true,
		Headers: map[string]string{
			"X-Apple-Store-Front": identifier + "-2,29",
		},
	})
	if err != nil {
		return "", false, fmt.Errorf("storefront country code request failed: %w", err)
	}

	if (res.StatusCode >= gohttp.StatusMultipleChoices && res.StatusCode < gohttp.StatusBadRequest) || res.StatusCode == gohttp.StatusNotFound {
		return "", false, nil
	}

	if res.StatusCode != gohttp.StatusOK {
		return "", false, NewErrorWithMetadata(errors.New("storefront country code request failed"), res)
	}

	var page storefrontGenrePage

	if err = json.Unmarshal(res.Data, &page); err != nil {
		return "", false, fmt.Errorf("failed to decode storefront country code: %w", err)
	}

	var (
		countryCode     string
		foundIdentifier bool
	)

	for _, data := range page.StorePlatformData {
		storefront := data.Meta.Storefront
		if storefront.ID == identifier {
			foundIdentifier = true
			countryCode = strings.ToUpper(strings.TrimSpace(storefront.CountryCode))

			break
		}
	}

	if !foundIdentifier {
		return "", false, nil
	}

	if len(countryCode) != 2 {
		return "", false, errors.New("storefront country code response contains no country code")
	}

	for _, character := range countryCode {
		if character < 'A' || character > 'Z' {
			return "", false, errors.New("storefront country code response contains an invalid country code")
		}
	}

	t.storefrontsMu.Lock()
	defer t.storefrontsMu.Unlock()

	for knownIdentifier, knownCountryCode := range t.storefrontCountryCodes {
		if knownCountryCode == countryCode && knownIdentifier != identifier {
			return "", false, nil
		}
	}

	t.storefrontCountryCodes[identifier] = countryCode

	return countryCode, true, nil
}

func (t *appstore) discoverAvailableStorefronts() error {
	t.storefrontsMu.Lock()
	defer t.storefrontsMu.Unlock()

	if t.storefrontCountryCodes != nil {
		return nil
	}

	if t.storefrontClient == nil {
		return errors.New("storefront client is not configured")
	}

	res, err := t.storefrontClient.Send(http.Request{
		URL:            availableStorefrontsURL,
		Method:         http.MethodGET,
		ResponseFormat: http.ResponseFormatRaw,
	})
	if err != nil {
		return fmt.Errorf("available storefronts request failed: %w", err)
	}

	if res.StatusCode != gohttp.StatusOK {
		return NewErrorWithMetadata(errors.New("available storefronts request failed"), res)
	}

	var result availableStorefrontsResult
	if _, err = plist.Unmarshal(res.Data, &result); err != nil {
		return fmt.Errorf("failed to decode available storefronts: %w", err)
	}

	countryCodes := make(map[string]string, len(result.Storefronts))

	for _, storefront := range result.Storefronts {
		countryCode := strings.ToUpper(strings.TrimSpace(storefront.CountryCode))
		if storefront.ID <= 0 || countryCode == "" {
			continue
		}

		countryCodes[strconv.FormatInt(storefront.ID, 10)] = countryCode
	}

	if len(countryCodes) == 0 {
		return errors.New("available storefronts response contains no storefronts")
	}

	t.storefrontCountryCodes = countryCodes

	return nil
}

func countryCodeFromStoreFront(storeFront string, countryCodes map[string]string) (string, error) {
	identifier := storefrontIdentifier(storeFront)

	if countryCode, ok := countryCodes[identifier]; ok {
		return countryCode, nil
	}

	return "", storefrontMappingError(storeFront)
}

func storefrontIdentifier(storeFront string) string {
	identifier, _, _ := strings.Cut(strings.TrimSpace(storeFront), ",")
	identifier, _, _ = strings.Cut(identifier, "-")

	return identifier
}

func storefrontMappingError(storeFront string) error {
	return fmt.Errorf("country code mapping for store front (%s) was not found", storeFront)
}
