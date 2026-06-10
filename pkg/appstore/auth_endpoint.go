package appstore

import (
	"errors"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"strings"

	"github.com/majd/ipatool/v2/pkg/http"
)

var authEndpointURLPattern = regexp.MustCompile(`https?://[^\s"'<>]+`)

func normalizeAuthEndpoint(endpoints ...string) string {
	for _, endpoint := range endpoints {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" {
			continue
		}

		normalized := normalizeNativeAuthEndpoint(endpoint)
		if normalized != "" {
			return normalized
		}

		return endpoint
	}

	return fmt.Sprintf("https://%s%s", PrivateAuthDomain, PrivateAuthPathNative)
}

func authEndpointFromResponseError(err error) string {
	var decodeErr *http.ResponseDecodeError
	if !errors.As(err, &decodeErr) {
		return ""
	}

	return authEndpointFromText(strings.Join(append(decodeErr.URLs, decodeErr.Body), " "))
}

func authEndpointFromText(text string) string {
	text = html.UnescapeString(strings.ReplaceAll(text, `\/`, `/`))
	matches := authEndpointURLPattern.FindAllString(text, -1)
	for _, match := range matches {
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
