package appstore

import (
	"encoding/json"
	"errors"
	"fmt"
	gohttp "net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/majd/ipatool/v2/internal/store/terms"
	"github.com/majd/ipatool/v2/pkg/http"
)

type termsResult struct {
	Status               *int            `json:"status"`
	TermsVersionID       json.RawMessage `json:"termsVersionId"`
	LatestTermsVersionID json.RawMessage `json:"latestTermsVersionId"`
	IsCurrent            bool            `json:"isCurrent"`
	ErrorMessageKey      string          `json:"errorMessageKey"`
}

func (t *appstore) acceptPurchaseTerms(acc Account, guid string) error {
	if t.termsClient == nil || t.bagClient == nil {
		return errors.New("terms client is not configured")
	}

	if dsid, err := strconv.ParseUint(acc.DirectoryServicesID, 10, 64); err != nil || dsid == 0 {
		return errors.New("account identifier is required for terms acceptance")
	}

	country, err := t.countryCodeFromStoreFront(acc.StoreFront)
	if err != nil {
		return err
	}

	bag, err := t.fetchURLBag(guid)
	if err != nil {
		return err
	}

	infoURL, err := termsEndpoint(bag.TermsInfoEndpoint, acc.Pod, false)
	if err != nil {
		return err
	}

	checkURL, err := termsEndpoint(bag.TermsCheckEndpoint, acc.Pod, true)
	if err != nil {
		return err
	}

	query := infoURL.Query()
	query.Set("cc", country)
	query.Set("termsType", "MZStore.NewTermsAndConditionsStatement")
	infoURL.RawQuery = query.Encode()

	info, err := t.termsClient.Send(termsRequest(acc, infoURL.String(), http.MethodGET, nil))
	if err != nil {
		return fmt.Errorf("failed to retrieve current terms: %w", err)
	}

	if err = checkTermsResponse(info); err != nil {
		return err
	}

	version, err := termsVersion(info.Data)
	if err != nil {
		return err
	}

	// Apple's Commerce UI posts the account DSID and the current version returned
	// by getTermsByCountryAndTypeSrv to the bag's terms-check endpoint.
	body, err := json.Marshal(map[string]any{"id": acc.DirectoryServicesID, "accepted": version})
	if err != nil {
		return fmt.Errorf("failed to encode terms acceptance: %w", err)
	}

	accepted, err := t.termsClient.Send(termsRequest(acc, checkURL.String(), http.MethodPOST, &http.RawPayload{Content: body}))
	if err != nil {
		return fmt.Errorf("failed to submit terms acceptance: %w", err)
	}

	if err = checkTermsResponse(accepted); err != nil {
		return err
	}

	if !accepted.Data.IsCurrent {
		return errors.New("apple did not confirm acceptance of the current terms")
	}

	return nil
}

func termsVersion(result termsResult) (json.RawMessage, error) {
	version := result.LatestTermsVersionID
	if len(version) == 0 || string(version) == "null" {
		version = result.TermsVersionID
	}

	var number json.Number
	if err := json.Unmarshal(version, &number); err != nil {
		return nil, errors.New("apple returned an invalid terms version")
	}

	value, err := strconv.ParseUint(number.String(), 10, 64)
	if err != nil || value == 0 || strconv.FormatUint(value, 10) != number.String() {
		return nil, errors.New("apple returned an invalid terms version")
	}

	return version, nil
}

func checkTermsResponse(response http.Result[termsResult]) error {
	if response.Data.ErrorMessageKey == "MZFinance.TermsCheckLoginRequired" {
		return ErrPasswordTokenExpired
	}

	if response.StatusCode != gohttp.StatusOK {
		return fmt.Errorf("unexpected terms response status: %d", response.StatusCode)
	}

	if response.Data.Status == nil {
		return errors.New("apple returned a terms response without a status")
	}

	if *response.Data.Status != 0 {
		return fmt.Errorf("apple terms request failed with status %d", *response.Data.Status)
	}

	return nil
}

func termsRequest(acc Account, endpoint, method string, payload http.Payload) http.Request {
	return http.Request{
		URL: endpoint, Method: method, ResponseFormat: http.ResponseFormatJSON, Payload: payload,
		Headers: map[string]string{
			"Accept": "application/json", "Content-Type": "application/json", "User-Agent": storeAuthenticationUserAgent,
			"X-Dsid": acc.DirectoryServicesID, "iCloud-DSID": acc.DirectoryServicesID,
			"X-Token": acc.PasswordToken, "X-Apple-Store-Front": acc.StoreFront,
		},
	}
}

func termsEndpoint(raw, pod string, acceptance bool) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.RawPath != "" || (u.Port() != "" && u.Port() != "443") {
		return nil, errors.New("invalid terms endpoint in bag")
	}

	if _, err = url.ParseQuery(u.RawQuery); err != nil {
		return nil, errors.New("invalid terms endpoint query in bag")
	}

	if !acceptance {
		if !strings.EqualFold(u.Hostname(), "amp-account.itunes.apple.com") || u.Path != "/account/terms/info" {
			return nil, errors.New("unsupported terms information endpoint in bag")
		}

		return u, nil
	}

	if !terms.IsBuyHost(u.Hostname()) || (u.Path != "/account/terms" && u.Path != "/WebObjects/MZFinance.woa/wa/termsCheckSrv") {
		return nil, errors.New("unsupported terms acceptance endpoint in bag")
	}

	if strings.EqualFold(u.Hostname(), "buy.itunes.apple.com") && pod != "" {
		host := "p" + pod + "-buy.itunes.apple.com"
		if !terms.IsBuyHost(host) {
			return nil, errors.New("invalid account pod for terms acceptance")
		}

		u.Host = host
	}

	return u, nil
}
