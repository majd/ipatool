package profilegen

import (
	"fmt"

	"howett.net/plist"
)

func (s *source) features() (map[string]bool, error) {
	result := map[string]bool{}

	for _, domain := range []string{"AppleMediaServices", "CFNetwork", "Foundation"} {
		b, err := s.cache.FeatureFlags(domain)
		if err != nil {
			return nil, fmt.Errorf("read Store feature defaults: %w", err)
		}

		var flags map[string]struct{ DevelopmentPhase string }
		if _, err = plist.Unmarshal(b, &flags); err != nil {
			return nil, fmt.Errorf("read Store feature defaults: %w", err)
		}

		for key, flag := range flags {
			switch flag.DevelopmentPhase {
			case "FeatureComplete":
				result[domain+"/"+key] = true
			default:
				return nil, fmt.Errorf("unsupported feature phase %s for %s/%s", flag.DevelopmentPhase, domain, key)
			}
		}
	}
	// Absent flags are disabled, matching the public feature-query ABI.
	for _, key := range []string{"UseNewNetworkStack", "AppendWithUnmodifiedParameters", "CompressPurchaseRequestBodies"} {
		if _, ok := result["AppleMediaServices/"+key]; !ok {
			result["AppleMediaServices/"+key] = false
		}
	}

	return result, nil
}
