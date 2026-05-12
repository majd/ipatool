package appstore

import "fmt"

type Platform string

const (
	PlatformIPhone  Platform = "iphone"
	PlatformIPad    Platform = "ipad"
	PlatformAppleTV Platform = "appletv"
)

func ParsePlatform(value string) (Platform, error) {
	switch value {
	case "":
		return "", nil
	case "iphone", "iPhone", "ios", "iOS":
		return PlatformIPhone, nil
	case "ipad", "iPad":
		return PlatformIPad, nil
	case "appletv", "AppleTV", "apple-tv", "tvos", "tvOS":
		return PlatformAppleTV, nil
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
	default:
		return "", fmt.Errorf("invalid platform %q", p)
	}
}
