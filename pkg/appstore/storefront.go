package appstore

import (
	"errors"
	"fmt"
	gohttp "net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/majd/ipatool/v2/pkg/http"
	"howett.net/plist"
)

type availableStorefrontsResult struct {
	Storefronts []availableStorefront `plist:"storefronts"`
}

type availableStorefront struct {
	CountryCode string `plist:"country-code"`
	ID          int64  `plist:"id"`
}

type storefrontBagResult struct {
	StorefrontID            any    `plist:"storeFrontId"`
	CountryCode             string `plist:"currentStorefrontCountryCodeISO2A"`
	AvailableStorefrontsURL string `plist:"available-storefronts"`
	URLBag                  struct {
		AvailableStorefrontsURL string `plist:"available-storefronts"`
	} `plist:"urlBag"`
}

type storefrontMapping struct{ id, country string }

func (t *appstore) countryCodeFromStoreFront(storeFront string) (string, error) {
	if !validStorefrontIdentifier(storefrontIdentifier(storeFront)) {
		return "", storefrontMappingError(storeFront)
	}

	mapping, err := t.discoverStorefront(storeFront, "")
	if err != nil {
		return "", err
	}

	return mapping.country, nil
}

func (t *appstore) storefrontID(countryCode string) string {
	t.storefrontsMu.Lock()
	defer t.storefrontsMu.Unlock()

	return findStorefrontMapping(t.storefrontCountryCodes, "", countryCode).id
}

func (t *appstore) discoverStorefrontID(countryCode string) (string, error) {
	countryCode = strings.ToUpper(strings.TrimSpace(countryCode))
	if !validStorefrontCountry(countryCode) {
		return "", fmt.Errorf("invalid storefront country code (%s)", countryCode)
	}

	mapping, err := t.discoverStorefront("", countryCode)
	if err != nil {
		return "", err
	}

	return mapping.id, nil
}

// A scoped bag identifies its actual storefront, including countries omitted by
// available-storefronts. Validate both fields: Apple defaults unknown IDs to US
// while echoing the requested ID in the response headers.
func (t *appstore) discoverStorefront(storeFront, country string) (storefrontMapping, error) {
	t.storefrontsMu.Lock()
	defer t.storefrontsMu.Unlock()

	identifier := storefrontIdentifier(storeFront)
	if mapping := findStorefrontMapping(t.storefrontCountryCodes, identifier, country); mapping.id != "" {
		return mapping, nil
	}

	if t.storefrontBagClient == nil {
		return storefrontMapping{}, errors.New("storefront bag client is not configured")
	}

	request := t.bagRequest("")
	if identifier != "" {
		request.Headers["X-Apple-Store-Front"] = strings.TrimSpace(storeFront)
	} else {
		request.URL += "&cc=" + url.QueryEscape(country)
	}

	response, err := t.storefrontBagClient.Send(request)
	if err != nil {
		return storefrontMapping{}, fmt.Errorf("storefront bag request failed: %w", err)
	}

	if response.StatusCode != gohttp.StatusOK {
		return storefrontMapping{}, NewErrorWithMetadata(errors.New("storefront bag request failed"), response)
	}

	bag := response.Data
	id := fmt.Sprint(bag.StorefrontID)
	code := strings.ToUpper(strings.TrimSpace(bag.CountryCode))

	if validStorefrontIdentifier(id) && validStorefrontCountry(code) &&
		(identifier == "" || id == identifier) && (country == "" || code == country) {
		if t.storefrontCountryCodes == nil {
			t.storefrontCountryCodes = make(map[string]string)
		}

		t.storefrontCountryCodes[id] = code

		return storefrontMapping{id: id, country: code}, nil
	}

	// Older bag formats can omit the identity metadata. Their advertised list
	// remains a dynamic fallback; never guess an endpoint or a country mapping.
	endpoint := bag.URLBag.AvailableStorefrontsURL
	if endpoint == "" {
		endpoint = bag.AvailableStorefrontsURL
	}

	if err := t.discoverAvailableStorefronts(endpoint); err != nil {
		return storefrontMapping{}, err
	}

	if mapping := findStorefrontMapping(t.storefrontCountryCodes, identifier, country); mapping.id != "" {
		return mapping, nil
	}

	if identifier != "" {
		return storefrontMapping{}, storefrontMappingError(storeFront)
	}

	return storefrontMapping{}, fmt.Errorf("store front mapping for country code (%s) was not found", country)
}

// Called with storefrontsMu held. Partial mappings from scoped bags do not mean
// the full list has been loaded, and loading it must preserve those mappings.
func (t *appstore) discoverAvailableStorefronts(endpoint string) error {
	if t.storefrontListLoaded {
		return nil
	}

	if t.storefrontClient == nil {
		return errors.New("storefront client is not configured")
	}

	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") ||
		(!strings.EqualFold(u.Hostname(), "apple.com") && !strings.HasSuffix(strings.ToLower(u.Hostname()), ".apple.com")) {
		return errors.New("invalid available-storefronts endpoint in bag")
	}

	res, err := t.storefrontClient.Send(http.Request{
		URL: endpoint, Method: http.MethodGET, ResponseFormat: http.ResponseFormatRaw,
		Headers: map[string]string{"Accept": "application/xml", "User-Agent": storeAuthenticationUserAgent},
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
		code := strings.ToUpper(strings.TrimSpace(storefront.CountryCode))
		if storefront.ID > 0 && validStorefrontCountry(code) {
			countryCodes[strconv.FormatInt(storefront.ID, 10)] = code
		}
	}

	if len(countryCodes) == 0 {
		return errors.New("available storefronts response contains no storefronts")
	}

	if t.storefrontCountryCodes == nil {
		t.storefrontCountryCodes = make(map[string]string)
	}

	for id, code := range countryCodes {
		if t.storefrontCountryCodes[id] == "" {
			t.storefrontCountryCodes[id] = code
		}
	}

	t.storefrontListLoaded = true

	return nil
}

func findStorefrontMapping(countryCodes map[string]string, identifier, country string) storefrontMapping {
	if identifier != "" {
		if code := countryCodes[identifier]; code != "" {
			return storefrontMapping{id: identifier, country: code}
		}

		return storefrontMapping{}
	}

	for id, code := range countryCodes {
		if code == country {
			return storefrontMapping{id: id, country: code}
		}
	}

	return storefrontMapping{}
}

func validStorefrontIdentifier(value string) bool {
	identifier, err := strconv.ParseInt(value, 10, 64)

	return err == nil && identifier > 0 && strconv.FormatInt(identifier, 10) == value
}

func validStorefrontCountry(value string) bool {
	return len(value) == 2 && value[0] >= 'A' && value[0] <= 'Z' && value[1] >= 'A' && value[1] <= 'Z'
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
