package appstore

import (
	"fmt"
	gohttp "net/http"
	"strings"

	"github.com/majd/ipatool/v2/pkg/http"
)

type BagInput struct{}

type BagOutput struct {
	AuthEndpoint string
}

func (t *appstore) Bag(input BagInput) (BagOutput, error) {
	macAddr, err := t.machine.MacAddress()
	if err != nil {
		return BagOutput{}, fmt.Errorf("failed to get mac address: %w", err)
	}

	guid := strings.ReplaceAll(strings.ToUpper(macAddr), ":", "")
	req := t.bagRequest(guid)

	res, err := t.bagClient.Send(req)
	if err != nil {
		return BagOutput{}, fmt.Errorf("failed to send http request: %w", err)
	}

	if res.StatusCode != gohttp.StatusOK {
		return BagOutput{}, fmt.Errorf("received unexpected status code: %d", res.StatusCode)
	}

	endpoint := res.Data.AuthEndpoint
	if endpoint == "" {
		endpoint = res.Data.URLBag.AuthEndpoint
	}
	// The new auth endpoint base requires the /fast sub-path
	if strings.Contains(endpoint, "auth.itunes.apple.com") && !strings.HasSuffix(endpoint, "/fast") {
		endpoint = endpoint + "/fast"
	}

	return BagOutput{
		AuthEndpoint: endpoint,
	}, nil
}

type bagResult struct {
	URLBag       urlBag `plist:"urlBag,omitempty"`
	AuthEndpoint string `plist:"authenticateAccount,omitempty"`
}

type urlBag struct {
	AuthEndpoint string `plist:"authenticateAccount,omitempty"`
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
