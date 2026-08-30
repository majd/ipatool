package appstore

import (
	"fmt"
	"strings"
)

type Platform string

const (
	PlatformIPhone   Platform = "iphone"
	PlatformIPad     Platform = "ipad"
	PlatformAppleTV  Platform = "appletv"
	PlatformVisionOS Platform = "visionos"
	PlatformMacOS    Platform = "macos"
)

func ParsePlatform(value string) (Platform, error) {
	switch strings.ToLower(value) {
	case "":
		return "", nil
	case "iphone", "ios":
		return PlatformIPhone, nil
	case "ipad", "ipados":
		return PlatformIPad, nil
	case "appletv", "apple-tv", "tvos":
		return PlatformAppleTV, nil
	case "vision", "visionos", "visionpro", "xros", "realitydevice":
		return PlatformVisionOS, nil
	case "mac", "macos", "osx":
		return PlatformMacOS, nil
	default:
		return "", fmt.Errorf("invalid platform %q", value)
	}
}

func (p Platform) lookupEntity() (string, error) {
	switch p {
	case "":
		return "software,iPadSoftware", nil
	case PlatformIPhone:
		return "software", nil
	case PlatformIPad:
		return "iPadSoftware", nil
	case PlatformAppleTV:
		return "tvSoftware", nil
	case PlatformVisionOS:
		return "xrosSoftware", nil
	case PlatformMacOS:
		return "macSoftware", nil
	default:
		return "", fmt.Errorf("invalid platform %q", p)
	}
}

func (p Platform) searchEntity() (string, error) {
	switch p {
	case "":
		return "software,iPadSoftware", nil
	case PlatformIPhone:
		return "software", nil
	case PlatformIPad:
		return "iPadSoftware", nil
	case PlatformAppleTV:
		return "software,tvSoftware", nil
	case PlatformVisionOS:
		return "xrosSoftware", nil
	case PlatformMacOS:
		return "macSoftware", nil
	default:
		return "", fmt.Errorf("invalid platform %q", p)
	}
}

func (p Platform) metadataPlatform() (string, error) {
	switch p {
	case PlatformIPhone, PlatformIPad:
		return "enterprisestore", nil
	case PlatformAppleTV:
		return "atv9", nil
	case PlatformVisionOS:
		return "realityDevice", nil
	default:
		return "", fmt.Errorf("invalid platform %q", p)
	}
}
