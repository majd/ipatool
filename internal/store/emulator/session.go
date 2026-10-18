package emulator

import (
	"errors"
	"fmt"
	"strconv"
)

func (r *Runtime) resolveAddress(promise uint64) (uint64, error) {
	// Apple's resultWithError: starts lazy promises and drives their wait. The
	// NSCondition bridge runs queued callbacks until that operation completes.
	// Draining every unrelated background task first can fail after success.
	failure := r.alloc(8)

	result, err := r.Invoke(promise, "resultWithError:", failure)
	if err != nil {
		return 0, err
	}

	if p := r.read64(failure); p != 0 {
		domain, e := r.Invoke(p, "domain")
		if e != nil {
			return 0, errors.New("framework returned an error")
		}

		code, e := r.Invoke(p, "code")
		if e != nil {
			return 0, errors.New("framework returned an error")
		}

		failure := &FrameworkError{Domain: r.String(domain), Code: int64(code)}
		if message, e := r.Invoke(p, "localizedDescription"); e == nil && message != 0 {
			failure.Message = r.String(message)
		}

		return 0, failure
	}

	if result == 0 {
		return 0, errors.New("framework returned no result")
	}

	return result, nil
}

func (r *Runtime) ephemeralAccount(saved map[string]any) (uint64, error) {
	account, err := r.Invoke(r.ClassAddress("ACAccount"), "ams_createEphemeralAccount")
	if err != nil {
		return 0, err
	}

	if saved == nil {
		return account, nil
	}

	dsid, err := strconv.ParseInt(fmt.Sprint(saved["directoryServicesIdentifier"]), 10, 64)
	if err != nil {
		return 0, errors.New("invalid saved account identifier")
	}

	fields := map[string]uint64{"setUsername:": r.importValue(saved["email"]), "ams_setDSID:": r.New("NSNumber", dsid), "ams_setPassword:": r.importValue(saved["passwordToken"]), "ams_setStorefront:": r.importValue(saved["storeFront"])}
	for selector, value := range fields {
		if _, err = r.Invoke(account, selector, value); err != nil {
			return 0, err
		}
	}

	return account, nil
}
func (r *Runtime) configureRequest(request, account, bag uint64, raw bool) error {
	properties, err := r.Invoke(request, "properties")
	if err != nil {
		return err
	}

	fields := map[string]uint64{"setAccount:": account, "setBag:": bag, "setKnownToBeTrusted:": 1, "setShouldSetCookiesFromResponse:": 0, "setShouldSetStorefrontFromResponse:": 0, "setDisableLoadURLMetrics:": 1, "setDisableBiometricsResponseHandling:": 1, "setMaxRetryCount:": 0, "setDisableResponseDecoding:": boolValue(raw)}
	for selector, value := range fields {
		if _, err = r.Invoke(properties, selector, value); err != nil {
			return err
		}
	}

	return nil
}
func (r *Runtime) newSession() (uint64, error) {
	session, err := r.Invoke(r.New("AMSURLSession", nil), "initWithConfiguration:delegate:delegateQueue:", 0, 0, 0)
	if err != nil {
		return 0, err
	}

	_, err = r.Invoke(session, "setUseFallbackBag:", 0)

	return session, err
}
func (r *Runtime) sendRequest(session, request uint64) (uint64, error) {
	promise, err := r.Invoke(session, "dataTaskPromiseWithRequest:", request)
	if err != nil {
		return 0, err
	}

	return r.resolveAddress(promise)
}
