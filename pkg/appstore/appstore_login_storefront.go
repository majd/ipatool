package appstore

import "strings"

// phoneNumberStorefront supplies a routing hint for phone-based Apple Accounts
// in mainland China and India. It does not change the Apple ID sent to Apple.
func phoneNumberStorefront(appleID string) string {
	number := strings.ReplaceAll(strings.TrimSpace(appleID), " ", "")
	number = strings.ReplaceAll(number, "-", "")

	international := false

	switch {
	case strings.HasPrefix(number, "+"):
		international = true
		number = number[1:]
	case strings.HasPrefix(number, "00"):
		international = true
		number = number[2:]
	}

	for _, digit := range number {
		if digit < '0' || digit > '9' {
			return ""
		}
	}

	// Check the full length before removing a country code: a local Indian
	// number can itself start with 91. Explicit international numbers must
	// match a supported country code and cannot fall back to a local format.
	switch {
	case len(number) == 13 && strings.HasPrefix(number, "861"):
		return storeFronts["CN"]
	case len(number) == 12 && strings.HasPrefix(number, "91"):
		number = number[2:]
	case international:
		return ""
	}

	if len(number) == 11 && number[0] == '1' {
		return storeFronts["CN"]
	}

	// India also permits a trunk prefix when writing a national number.
	if !international && len(number) == 11 && number[0] == '0' {
		number = number[1:]
	}

	if len(number) == 10 && number[0] >= '6' && number[0] <= '9' {
		return storeFronts["IN"]
	}

	return ""
}
