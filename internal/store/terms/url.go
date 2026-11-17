// Package terms identifies the Store's account terms flow.
package terms

import (
	"net/url"
	"strings"
)

// IsBuyHost accepts the Store purchase service and its numbered account pods.
func IsBuyHost(host string) bool {
	host = strings.ToLower(host)
	if host == "buy.itunes.apple.com" {
		return true
	}

	pod, ok := strings.CutSuffix(host, "-buy.itunes.apple.com")
	if !ok || len(pod) < 2 || pod[0] != 'p' {
		return false
	}

	for _, digit := range pod[1:] {
		if digit < '0' || digit > '9' {
			return false
		}
	}

	return true
}

// IsPageURL identifies the terms action returned by buyProduct.
func IsPageURL(raw string) bool {
	u, err := url.Parse(raw)

	return err == nil && u.Scheme == "https" && IsBuyHost(u.Hostname()) &&
		(u.Port() == "" || u.Port() == "443") && u.User == nil && u.Fragment == "" && u.RawPath == "" &&
		u.Path == "/WebObjects/MZFinance.woa/wa/com.apple.jingle.app.finance.DirectAction/termsPage"
}
