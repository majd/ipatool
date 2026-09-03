package appstore

import (
	"errors"
	"strings"
)

func downloadPackagePlatform(platform Platform, item downloadItemResult) (Platform, error) {
	if platform != PlatformMacOS {
		return platform, nil
	}

	softwarePlatform := downloadMetadataString(item.Metadata, "software-platform")
	productType := downloadMetadataString(item.Metadata, "product-type")
	mobileMetadata := strings.EqualFold(softwarePlatform, "ios") || strings.EqualFold(productType, "ios-app")
	macMetadata := strings.EqualFold(softwarePlatform, "macos") || strings.EqualFold(productType, "mac-os-app")
	mobileSinf := hasMobileSinf(item.Sinfs)
	mobileSinfEvidence := hasMobileSinfEvidence(item.Sinfs)
	macDPInfo := hasMacDPInfo(item.Sinfs)

	if (mobileMetadata && macMetadata) || (mobileMetadata && macDPInfo) || (macMetadata && mobileSinfEvidence) || (mobileSinfEvidence && macDPInfo) {
		return "", errors.New("download response contains conflicting package metadata")
	}

	if mobileMetadata || mobileSinfEvidence {
		if !mobileSinf {
			return "", errors.New("download response does not contain mobile sinf data")
		}

		return PlatformIPhone, nil
	}

	return PlatformMacOS, nil
}

func downloadMetadataString(metadata map[string]interface{}, key string) string {
	value, _ := metadata[key].(string)

	return value
}

func hasMobileSinf(sinfs []Sinf) bool {
	if len(sinfs) == 0 {
		return false
	}

	for _, sinf := range sinfs {
		if len(sinf.Data) == 0 || len(sinf.DPInfo) > 0 {
			return false
		}
	}

	return true
}

func hasMobileSinfEvidence(sinfs []Sinf) bool {
	for _, sinf := range sinfs {
		if len(sinf.Data) > 0 && len(sinf.DPInfo) == 0 {
			return true
		}
	}

	return false
}

func hasMacDPInfo(sinfs []Sinf) bool {
	for _, sinf := range sinfs {
		if len(sinf.DPInfo) > 0 {
			return true
		}
	}

	return false
}
