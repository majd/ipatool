package appstore

import (
	"fmt"
	gohttp "net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/majd/ipatool/v2/pkg/http"
)

type BagInput struct{}

type BagOutput struct {
	// AuthEndpoint is retained for callers that inspect the bag directly.
	AuthEndpoint string
	SAPConfig    SAPConfig
}

func (t *appstore) Bag(input BagInput) (BagOutput, error) {
	macAddr, err := t.machine.MacAddress()
	if err != nil {
		return BagOutput{}, fmt.Errorf("failed to get mac address: %w", err)
	}

	guid, _, err := machineIdentity(macAddr)
	if err != nil {
		return BagOutput{}, err
	}

	return t.bag(guid)
}

func (t *appstore) bag(guid string) (BagOutput, error) {
	req := t.bagRequest(guid)

	res, err := t.bagClient.Send(req)
	if err != nil {
		return BagOutput{}, fmt.Errorf("failed to send http request: %w", err)
	}

	if res.StatusCode != gohttp.StatusOK {
		return BagOutput{}, fmt.Errorf("received unexpected status code: %d", res.StatusCode)
	}

	version, err := strconv.ParseUint(res.Data.URLBag.SAPVersion, 10, 32)
	if err != nil {
		return BagOutput{}, fmt.Errorf("invalid SAP version %q in bag: %w", res.Data.URLBag.SAPVersion, err)
	}

	config := SAPConfig{
		AuthEndpoint:   res.Data.URLBag.AuthEndpoint,
		SetupURL:       res.Data.URLBag.SAPSetupEndpoint,
		CertificateURL: res.Data.URLBag.SAPSetupCertEndpoint,
		Version:        uint32(version),
	}
	if err := validateSAPConfig(config); err != nil {
		return BagOutput{}, err
	}

	return BagOutput{AuthEndpoint: config.AuthEndpoint, SAPConfig: config}, nil
}

type bagResult struct {
	URLBag urlBag `plist:"urlBag,omitempty"`
}

type urlBag struct {
	AuthEndpoint         string `plist:"authenticateAccount,omitempty"`
	SAPSetupEndpoint     string `plist:"sign-sap-setup,omitempty"`
	SAPSetupCertEndpoint string `plist:"sign-sap-setup-cert,omitempty"`
	SAPVersion           string `plist:"sign-sap-version,omitempty"`
}

func validateSAPConfig(config SAPConfig) error {
	if err := validateAuthenticationEndpoint(config.AuthEndpoint); err != nil {
		return err
	}

	endpoints := []struct {
		name string
		url  string
	}{
		{name: "SAP setup", url: config.SetupURL},
		{name: "SAP setup certificate", url: config.CertificateURL},
	}

	for _, endpoint := range endpoints {
		parsed, err := url.ParseRequestURI(endpoint.url)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return fmt.Errorf("invalid %s endpoint %q in bag", endpoint.name, endpoint.url)
		}
	}

	if config.Version != 200 {
		return fmt.Errorf("unsupported SAP version %d in bag", config.Version)
	}

	return nil
}

func validateAuthenticationEndpoint(endpoint string) error {
	parsed, err := url.ParseRequestURI(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("invalid authentication endpoint %q", endpoint)
	}

	host := strings.ToLower(parsed.Hostname())
	if host != PrivateAppStoreAPIDomain && !strings.HasSuffix(host, "-buy.itunes.apple.com") {
		return fmt.Errorf("unsupported authentication endpoint %q", endpoint)
	}

	if parsed.Path != PrivateAppStoreAPIPathAuth {
		return fmt.Errorf("unsupported authentication endpoint %q", endpoint)
	}

	return nil
}

func (*appstore) bagRequest(guid string) http.Request {
	return http.Request{
		URL:            fmt.Sprintf("https://%s%s?guid=%s", PrivateInitDomain, PrivateInitPath, guid),
		Method:         http.MethodGET,
		ResponseFormat: http.ResponseFormatXML,
		Headers: map[string]string{
			"Accept": "application/xml",
		},
	}
}
