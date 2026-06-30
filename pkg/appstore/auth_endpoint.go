package appstore

import (
	"errors"
	"html"
	"net/url"
	"strings"

	"github.com/majd/ipatool/v2/pkg/http"
)

func normalizeAuthEndpoint(endpoints ...string) string {
	for _, endpoint := range endpoints {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" {
			continue
		}

		if normalized := normalizeNativeAuthEndpoint(endpoint); normalized != "" {
			return normalized
		}

		return endpoint
	}

	return defaultNativeAuthEndpoint
}

func authEndpointFromResponseError(err error) string {
	var decodeErr *http.ResponseDecodeError
	if !errors.As(err, &decodeErr) {
		return ""
	}

	parts := make([]string, 0, len(decodeErr.URLs)+1)
	parts = append(parts, decodeErr.URLs...)
	if decodeErr.Body != "" {
		parts = append(parts, decodeErr.Body)
	}
	if len(parts) == 0 {
		return ""
	}

	return authEndpointFromText(strings.Join(parts, " "))
}

func authEndpointFromText(text string) string {
	text = html.UnescapeString(strings.ReplaceAll(text, `\/`, `/`))
	for _, match := range http.ExtractURLs([]byte(text)) {
		if endpoint := normalizeNativeAuthEndpoint(strings.TrimRight(match, ".,;)")); endpoint != "" {
			return endpoint
		}
	}

	return ""
}

func normalizeNativeAuthEndpoint(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host != PrivateAuthDomain {
		return ""
	}

	path := strings.TrimRight(parsed.Path, "/")
	if !strings.HasSuffix(path, "/fast") {
		path = strings.TrimRight(path, "/") + "/fast"
	}
	parsed.Path = path + "/"

	return parsed.String()
}
