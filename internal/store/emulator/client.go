package emulator

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"howett.net/plist"
)

// Request keeps caller session storage and package assembly outside the guest.
// Encoding, protocol handling and response interpretation execute in Apple code.
type Request struct {
	Timeout                        time.Duration
	URL, Method, Operation, Format string
	Headers                        http.Header
	Parameters                     map[string]any
	Body, HardwareID               []byte
	Bag                            map[string]any
	Sign                           func([]byte) ([]byte, error)
	Transport                      func(*http.Request) (*http.Response, error)
	Stream, Raw, NoRedirects       bool
}

//nolint:nonamedreturns // Recovery and streamed-body cleanup must update the returned error.
func (r *Runtime) Execute(input Request) (response *http.Response, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = recoveredError("Store request preparation", failure)
		}

		if err != nil && r.lastResponse != nil && r.streamResponse {
			_ = r.lastResponse.Body.Close()
		}
	}()

	if input.Transport == nil {
		return nil, errors.New("store transport is not configured")
	}

	if len(input.HardwareID) != 6 {
		return nil, errors.New("invalid Store hardware identity")
	}

	r.hardwareID = append([]byte(nil), input.HardwareID...)
	r.transport = input.Transport
	r.streamResponse = input.Stream
	r.noRedirects = input.NoRedirects
	r.authRequest = input.Operation == "authenticate"

	var saved map[string]any
	if dsid := input.Headers.Get("X-Dsid"); dsid != "" {
		saved = map[string]any{"directoryServicesIdentifier": dsid, "passwordToken": input.Headers.Get("X-Token"), "storeFront": input.Headers.Get("X-Apple-Store-Front"), "email": ""}
	}

	account, err := r.ephemeralAccount(saved)
	if err != nil {
		return nil, err
	}

	var bag uint64

	if len(input.Bag) > 0 {
		values := map[string]any{}
		for key, value := range input.Bag {
			values[key] = value
		}

		if nested, ok := values["urlBag"].(map[string]any); ok {
			for key, value := range nested {
				values[key] = value
			}
		}

		if input.Operation == "purchase" {
			values["buyProduct"] = input.URL
		}

		bag, err = r.frozenBagFromValues(values)
		if err != nil {
			return nil, err
		}
	}

	session, err := r.newSession()
	if err != nil {
		return nil, err
	}

	if input.Operation != "authenticate" {
		if _, err = r.Invoke(session, "setDelegate:", r.interactiveDelegate()); err != nil {
			return nil, err
		}
	}

	parameters := input.Parameters

	if input.Operation == "authenticate" {
		if input.Sign == nil {
			return nil, errors.New("authentication requires a SAP signer")
		}

		if _, err = r.Invoke(account, "setUsername:", r.importValue(parameters["appleId"])); err != nil {
			return nil, err
		}

		task, e := r.newCredentialTask(account, bag, fmt.Sprint(parameters["password"]), 0)
		if e != nil {
			return nil, e
		}

		value, e := r.Invoke(task, "_createAuthenticateRequestParametersForAccount:", account)
		if e != nil {
			return nil, e
		}

		built, e := r.Export(value)
		if e != nil {
			return nil, e
		}

		native := built.(map[string]any)
		if !strings.EqualFold(fmt.Sprint(native["guid"]), hex.EncodeToString(input.HardwareID)) {
			return nil, errors.New("authentication hardware identity mismatch")
		}
		// Preserve the CLI's second credential attempt and 2FA retry contract.
		for _, key := range []string{"attempt", "why"} {
			if value, ok := parameters[key]; ok {
				native[key] = value
			}
		}

		parameters = native

		if _, err = r.Invoke(session, "setDelegate:", task); err != nil {
			return nil, err
		}
	}

	var request uint64
	if input.Operation == "purchase" {
		request, err = r.purchaseRequest(account, bag, input)
	} else if parameters != nil {
		request, err = r.encodeRequest(account, bag, input.Method, input.URL, parameters)
	} else {
		request, err = r.Invoke(r.New("AMSURLRequest", nil), "initWithURL:cachePolicy:timeoutInterval:", r.New("NSURL", input.URL), 0)
		if err == nil {
			_, err = r.Invoke(request, "setHTTPMethod:", r.New("NSString", input.Method))
		}

		if err == nil && input.Body != nil {
			_, err = r.Invoke(request, "setHTTPBody:", r.New("NSData", input.Body))
		}
	}

	if err != nil {
		return nil, err
	}

	if err = r.configureRequest(request, account, bag, input.Stream || input.Raw); err != nil {
		return nil, err
	}

	for key, values := range input.Headers {
		if strings.EqualFold(key, "Content-Length") {
			continue
		}

		if _, err = r.Invoke(request, "setValue:forHTTPHeaderField:", r.New("NSString", strings.Join(values, ", ")), r.New("NSString", key)); err != nil {
			return nil, err
		}
	}

	if input.Sign != nil {
		body, e := r.Invoke(request, "HTTPBody")
		if e != nil {
			return nil, e
		}

		if body == 0 {
			return nil, errors.New("signed request has no body")
		}

		signature, e := input.Sign(r.objects[body].Value.([]byte))
		if e != nil {
			return nil, fmt.Errorf("sign Store request: %w", e)
		}

		if _, err = r.Invoke(request, "setValue:forHTTPHeaderField:", r.New("NSString", base64.StdEncoding.EncodeToString(signature)), r.New("NSString", "X-Apple-ActionSignature")); err != nil {
			return nil, err
		}
	}

	result, requestErr := r.sendRequest(session, request)

	if r.transportError != nil {
		return nil, r.transportError
	}

	if requestErr != nil {
		// A decoded finance error is a real server response. Return its original
		// body to the compatibility layer for the existing 2FA/license sentinels.
		var finance *FrameworkError
		if errors.As(requestErr, &finance) && r.lastResponse != nil && !input.Stream {
			return r.lastResponse, nil
		}

		return nil, requestErr
	}

	if r.lastResponse == nil {
		return nil, errors.New("store session returned no HTTP response")
	}

	if input.Stream || input.Raw {
		return r.lastResponse, nil
	}

	object, e := r.Invoke(result, "object")
	if e != nil {
		return nil, e
	}

	if object != 0 {
		value, e := r.Export(object)
		if e != nil {
			return nil, e
		}

		var encoded []byte
		if input.Format == "json" || strings.Contains(strings.ToLower(r.lastResponse.Header.Get("Content-Type")), "json") {
			encoded, e = json.Marshal(value)
		} else {
			encoded, e = plist.Marshal(value, plist.XMLFormat)
		}

		if e != nil {
			return nil, fmt.Errorf("copy decoded Store response: %w", e)
		}

		r.lastResponse.Body = io.NopCloser(bytes.NewReader(encoded))
		r.lastResponse.ContentLength = int64(len(encoded))
		r.lastResponse.Header.Del("Content-Encoding")
		r.lastResponse.Header.Del("Content-Length")
	}

	return r.lastResponse, nil
}

func (r *Runtime) encodeRequest(account, bag uint64, method, endpoint string, parameters map[string]any) (uint64, error) {
	encoder, err := r.Invoke(r.New("AMSURLRequestEncoder", nil), "initWithBag:", bag)
	if err != nil {
		return 0, err
	}

	for selector, value := range map[string]uint64{"setAccount:": account, "setRequestEncoding:": 2, "setAnisetteType:": 0, "setMescalType:": 0, "setEnableRemoteSecuritySigning:": 0, "setUrlKnownToBeTrusted:": 1, "setShouldSetCookieHeader:": 0, "setShouldSetStorefrontHeader:": 0, "setDisableLoadURLMetrics:": 1} {
		if _, err = r.Invoke(encoder, selector, value); err != nil {
			return 0, err
		}
	}

	methodID := map[string]uint64{"GET": 2, "POST": 4, "PUT": 5}[method]
	if methodID == 0 {
		return 0, errors.New("unsupported encoded Store request method")
	}

	promise, err := r.Invoke(encoder, "requestWithMethod:URL:parameters:", methodID, r.New("NSURL", endpoint), r.importValue(parameters))
	if err != nil {
		return 0, err
	}

	return r.resolveAddress(promise)
}

func (r *Runtime) purchaseRequest(account, bag uint64, input Request) (uint64, error) {
	if bag == 0 {
		return 0, errors.New("purchase requires a Store bag")
	}

	if fmt.Sprint(input.Parameters["price"]) != "0" || input.Parameters["productType"] != "C" {
		return 0, errors.New("purchasing paid apps is not supported")
	}

	params, err := r.Invoke(r.New("AMSBuyParams", nil), "init")
	if err != nil {
		return 0, err
	}

	for key, value := range input.Parameters {
		if _, err = r.Invoke(params, "setParameter:forKey:", r.importValue(value), r.New("NSString", key)); err != nil {
			return 0, err
		}
	}

	purchase, err := r.Invoke(r.New("AMSPurchase", nil), "initWithPurchaseType:buyParams:", 0, params)
	if err != nil {
		return 0, err
	}

	if _, err = r.Invoke(purchase, "setAccount:", account); err != nil {
		return 0, err
	}

	info, err := r.Invoke(r.New("AMSPurchaseInfo", nil), "initWithPurchase:", purchase)
	if err != nil {
		return 0, err
	}

	for selector, value := range map[string]uint64{"setAddKBSync:": 0, "setHasBeenAuthedForBuy:": 1} {
		if _, err = r.Invoke(info, selector, value); err != nil {
			return 0, err
		}
	}

	encoder, err := r.configuredPurchaseEncoder(info, bag)
	if err != nil {
		return 0, err
	}

	for selector, value := range map[string]uint64{"setAnisetteType:": 0, "setMescalType:": 0, "setUrlKnownToBeTrusted:": 1, "setEnableRemoteSecuritySigning:": 0, "setDisableLoadURLMetrics:": 1, "setCompressRequestBody:": 0, "setShouldSetStorefrontHeader:": 0, "setShouldSetCookieHeader:": 0} {
		if _, err = r.Invoke(encoder, selector, value); err != nil {
			return 0, err
		}
	}

	promise, err := r.Invoke(encoder, "encodeRequest")
	if err != nil {
		return 0, err
	}

	request, err := r.resolveAddress(promise)
	if err != nil {
		return 0, err
	}

	body, err := r.Invoke(request, "HTTPBody")
	if err != nil {
		return 0, err
	}

	var encoded map[string]any

	if body == 0 {
		return 0, errors.New("purchase encoder returned no body")
	}

	if _, err = plist.Unmarshal(r.objects[body].Value.([]byte), &encoded); err != nil {
		return 0, errors.New("invalid encoded purchase")
	}

	for _, key := range []string{"price", "salableAdamId", "pricingParameters", "productType"} {
		if fmt.Sprint(encoded[key]) != fmt.Sprint(input.Parameters[key]) {
			return 0, errors.New("purchase encoder changed product or price")
		}
	}

	return request, nil
}

func allowedURL(value *url.URL, authenticated bool) bool {
	if value == nil || value.User != nil || value.Fragment != "" {
		return false
	}

	host := strings.ToLower(value.Hostname())
	if !authenticated {
		// Package/artwork URLs are supplied by the Store response. Retain the
		// downloader's support for signed CDN URLs without binding it to one CDN.
		return host != "" && (value.Scheme == "https" || value.Scheme == "http")
	}

	apple := host == "apple.com" || strings.HasSuffix(host, ".apple.com") || strings.HasSuffix(host, ".mzstatic.com") || strings.HasSuffix(host, ".cdn-apple.com")

	return apple && value.Scheme == "https"
}

type FrameworkError struct {
	Domain, Message string
	Code            int64
}

func (e *FrameworkError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("%s (%s/%d)", e.Message, e.Domain, e.Code)
	}

	return fmt.Sprintf("Store framework error %s/%d", e.Domain, e.Code)
}
