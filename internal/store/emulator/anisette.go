package emulator

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// StoreAMDHeaders executes AppleMediaServices' Store anisette provider in the
// guest. Type 2 is the Configurator AMD proof, not AuthKit's I-MD proof.
func (r *Runtime) StoreAMDHeaders(endpoint string, hardwareID []byte, xpcHandler func(string, []byte) ([]byte, error)) (map[string]string, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return nil, errors.New("invalid Store AMD endpoint")
	}

	if len(hardwareID) != 6 {
		return nil, errors.New("invalid Store AMD hardware identity")
	}

	r.hardwareID = append([]byte(nil), hardwareID...)
	r.xpcSyncHandler = xpcHandler
	r.installStoreAMDAdapter()

	request, err := r.Invoke(r.ClassAddress("NSURLRequest"), "requestWithURL:", r.New("NSURL", parsed.String()))
	if err != nil {
		return nil, fmt.Errorf("create Store AMD request: %w", err)
	}

	value, err := r.Invoke(r.ClassAddress("AMSAnisette"), "_headersForRequest:account:type:", request, 0, 2)
	if err != nil {
		return nil, fmt.Errorf("generate Store AMD proof: %w", err)
	}

	exported, err := r.Export(value)
	if err != nil {
		return nil, fmt.Errorf("export Store AMD proof: %w", err)
	}

	dictionary, ok := exported.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("AppleMediaServices returned no Store AMD proof (%T)", exported)
	}

	result := make(map[string]string, 2)

	for _, key := range []string{"X-Apple-AMD", "X-Apple-AMD-M"} {
		text, ok := dictionary[key].(string)
		if !ok || text == "" || len(text) > 8192 || strings.ContainsAny(text, "\r\n") {
			return nil, errors.New("AppleMediaServices returned an incomplete Store AMD proof")
		}

		result[key] = text
	}

	return result, nil
}
