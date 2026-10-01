package appstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	gohttp "net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"

	"github.com/majd/ipatool/v2/pkg/http"
)

var (
	ErrAuthCodeRequired = errors.New("auth code is required")
)

const (
	maxAuthenticationRequestAttempts = 3
	maxAuthenticationRedirects       = 4
	authenticationRetryDelay         = 10 * time.Second
	maxAuthenticationRetryDelay      = 30 * time.Second
)

type LoginInput struct {
	Email    string
	Password string
	AuthCode string
	// Endpoint is deprecated. Login always uses the SAP configuration from
	// Apple's current bag so unsigned or caller-selected fallbacks are impossible.
	Endpoint string
}

type LoginOutput struct {
	Account Account
}

func (t *appstore) Login(input LoginInput) (LoginOutput, error) {
	authCode, err := normalizeAuthCode(input.AuthCode)
	if err != nil {
		return LoginOutput{}, err
	}

	macAddr, err := t.machine.MacAddress()
	if err != nil {
		return LoginOutput{}, fmt.Errorf("failed to get mac address: %w", err)
	}

	guid, machineID, err := machineIdentity(macAddr)
	if err != nil {
		return LoginOutput{}, err
	}

	bag, err := t.bag(guid)
	if err != nil {
		return LoginOutput{}, fmt.Errorf("failed to get bag: %w", err)
	}

	if t.actionSignerFactory == nil {
		return LoginOutput{}, errors.New("SAP action signer is not configured")
	}

	signer, err := t.actionSignerFactory(bag.SAPConfig, machineID)
	if err != nil {
		return LoginOutput{}, fmt.Errorf("failed to initialize SAP action signer: %w", err)
	}

	if signer == nil {
		return LoginOutput{}, errors.New("SAP action signer factory returned nil")
	}

	acc, loginErr := t.login(input.Email, input.Password, authCode, guid, bag.SAPConfig.AuthEndpoint, signer)
	closeErr := signer.Close()

	if closeErr != nil {
		closeErr = fmt.Errorf("failed to close SAP action signer: %w", closeErr)
	}

	if loginErr != nil {
		if closeErr != nil {
			return LoginOutput{}, errors.Join(loginErr, closeErr)
		}

		return LoginOutput{}, loginErr
	}

	output := LoginOutput{Account: acc}
	if closeErr != nil {
		return output, closeErr
	}

	return output, nil
}

func normalizeAuthCode(code string) (string, error) {
	if code == "" {
		return "", nil
	}

	// Terminals may wrap pasted input in bracketed-paste markers. Strip only
	// a matched outer pair; other escape sequences are invalid input.
	code = strings.TrimSpace(code)
	if strings.HasPrefix(code, "\x1b[200~") && strings.HasSuffix(code, "\x1b[201~") {
		code = strings.TrimSuffix(strings.TrimPrefix(code, "\x1b[200~"), "\x1b[201~")
	}

	code = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}

		return r
	}, code)
	if len(code) != 6 || strings.IndexFunc(code, func(r rune) bool { return r < '0' || r > '9' }) != -1 {
		return "", errors.New("2FA code must contain exactly six digits")
	}

	return code, nil
}

type loginAddressResult struct {
	FirstName string `plist:"firstName,omitempty"`
	LastName  string `plist:"lastName,omitempty"`
}

type loginAccountResult struct {
	Email   string             `plist:"appleId,omitempty"`
	Address loginAddressResult `plist:"address,omitempty"`
}

type loginResult struct {
	FailureType         string             `plist:"failureType,omitempty"`
	CustomerMessage     string             `plist:"customerMessage,omitempty"`
	Account             loginAccountResult `plist:"accountInfo,omitempty"`
	DirectoryServicesID string             `plist:"dsPersonId,omitempty"`
	PasswordToken       string             `plist:"passwordToken,omitempty"`
}

func (t *appstore) login(email, password, authCode, guid, endpoint string, signer ActionSigner) (Account, error) {
	request := t.loginRequest(email, password, authCode, guid, endpoint, 1, signer)
	redirects := 0

	var (
		err error
		res http.Result[loginResult]
	)

	for attempt := 1; ; {
		res, err = t.sendAuthenticationRequest(request)

		if err != nil {
			stage := "sign-in"
			if authCode != "" {
				stage = "2FA verification"
			}

			if redirects > 0 {
				stage += " at Store pod"
			}

			return Account{}, fmt.Errorf("%s request failed: %w", stage, err)
		}

		retry, redirect, err := t.parseLoginResponse(&res, attempt, authCode, request.URL)
		if err != nil {
			return Account{}, err
		}

		if !retry {
			break
		}

		if redirect != "" {
			if redirects == maxAuthenticationRedirects {
				return Account{}, authenticationRedirectError(errors.New("too many authentication redirects"), res.StatusCode, redirect)
			}

			redirects++
			// Only the destination changes: redirects belong to the same login
			// attempt and must retain its payload, including the attempt value.
			request.URL = redirect

			continue
		}

		attempt++
		request = t.loginRequest(email, password, authCode, guid, request.URL, attempt, signer)
	}

	sf, err := res.GetHeader(HTTPHeaderStoreFront)
	if err != nil {
		return Account{}, NewErrorWithMetadata(fmt.Errorf("failed to get storefront header: %w", err), res)
	}

	pod, err := res.GetHeader(HTTPHeaderPod)
	if err != nil && !errors.Is(err, http.ErrHeaderNotFound) {
		return Account{}, NewErrorWithMetadata(fmt.Errorf("failed to get pod header: %w", err), res)
	}

	addr := res.Data.Account.Address
	acc := Account{
		Name:                strings.Join([]string{addr.FirstName, addr.LastName}, " "),
		Email:               res.Data.Account.Email,
		PasswordToken:       res.Data.PasswordToken,
		DirectoryServicesID: res.Data.DirectoryServicesID,
		StoreFront:          sf,
		Password:            password,
		Pod:                 pod,
	}

	data, err := json.Marshal(acc)
	if err != nil {
		return Account{}, fmt.Errorf("failed to marshal json: %w", err)
	}

	err = t.keychain.Set("account", data)
	if err != nil {
		return Account{}, fmt.Errorf("failed to save account in keychain: %w", err)
	}

	return acc, nil
}

func (t *appstore) sendAuthenticationRequest(request http.Request) (http.Result[loginResult], error) {
	statuses := make([]string, 0, maxAuthenticationRequestAttempts)
	onlyHTTP := true

	sleep := t.authRetrySleep
	if sleep == nil {
		sleep = time.Sleep
	}

	for attempt := 1; ; attempt++ {
		result, err := t.loginClient.Send(request)

		status, retry := retryableAuthenticationError(err)
		if !retry {
			if err != nil {
				return result, authenticationRequestError(err)
			}

			return result, nil
		}

		outcome := fmt.Sprintf("HTTP %d", status)
		if status == 0 {
			outcome = "transport error"
			onlyHTTP = false
		}

		statuses = append(statuses, outcome)

		if attempt == maxAuthenticationRequestAttempts {
			summary := strings.Join(statuses, ", ")
			if onlyHTTP {
				// Preserve the existing diagnostic format for HTTP-only failures.
				summary = strings.ReplaceAll(summary, ", HTTP ", ", ")
			}

			return result, fmt.Errorf(
				"authentication request failed after %d attempts (%s): %w",
				maxAuthenticationRequestAttempts, summary, authenticationRequestError(err),
			)
		}

		delay := min(authenticationRetryDelay<<(attempt-1), maxAuthenticationRetryDelay)

		var (
			responseErr  *http.UnexpectedResponseError
			transportErr *http.TransportError
			retryAfter   string
		)

		if errors.As(err, &responseErr) {
			retryAfter = responseErr.RetryAfter
		} else if errors.As(err, &transportErr) {
			retryAfter = transportErr.RetryAfter
		}

		if requested, ok := authenticationRetryAfter(retryAfter, time.Now()); ok {
			if requested > maxAuthenticationRetryDelay {
				return result, fmt.Errorf("apple requested a wait longer than %s; try again later: %w", maxAuthenticationRetryDelay, err)
			}

			// Retry-After takes precedence over the fallback backoff.
			delay = max(requested, time.Second)
		}

		sleep(delay)
	}
}

func retryableAuthenticationError(err error) (int, bool) {
	var responseErr *http.UnexpectedResponseError
	if !errors.As(err, &responseErr) {
		var transportErr *http.TransportError
		if !errors.As(err, &transportErr) || errors.Is(err, context.Canceled) {
			return 0, false
		}

		// A url.Error can report Timeout false when its cause is wrapped by
		// AddHeaderTransport, so inspect each underlying error as well.
		for cause := transportErr.Err; cause != nil; cause = errors.Unwrap(cause) {
			if networkErr, ok := cause.(net.Error); ok && networkErr.Timeout() {
				return 0, true
			}
		}

		return 0, errors.Is(transportErr, io.EOF) || errors.Is(transportErr, io.ErrUnexpectedEOF) ||
			errors.Is(transportErr, syscall.ECONNRESET) || errors.Is(transportErr, syscall.EPIPE)
	}

	status := responseErr.StatusCode
	// A Store pod can transiently return an HTML 403 page. Populated Apple
	// credential errors are decoded normally and never reach this branch.
	retry := status == gohttp.StatusNoContent ||
		status == gohttp.StatusForbidden ||
		status == gohttp.StatusNotFound ||
		status == gohttp.StatusTooManyRequests ||
		status/100 == 5

	return status, retry
}

func (t *appstore) parseLoginResponse(res *http.Result[loginResult], attempt int, authCode, endpoint string) (bool, string, error) {
	var (
		retry    bool
		redirect string
		err      error
	)

	if res.StatusCode >= gohttp.StatusMultipleChoices && res.StatusCode < gohttp.StatusBadRequest {
		if redirect, err = res.GetHeader("location"); err != nil {
			err = fmt.Errorf("failed to retrieve redirect location: %w", err)
		} else if !http.IsAuthenticationRedirect(res.StatusCode) {
			err = errors.New("unsupported authentication redirect status")
		} else {
			redirect, err = resolveAuthenticationRedirect(endpoint, redirect)
		}

		if err != nil {
			location, _ := res.GetHeader("location")

			return false, "", authenticationRedirectError(err, res.StatusCode, location)
		}

		retry = true
	} else if attempt == 1 && res.Data.FailureType == FailureTypeInvalidCredentials {
		retry = true
	} else if res.Data.FailureType == "" && res.Data.CustomerMessage == CustomerMessageBadLogin {
		if authCode == "" {
			err = ErrAuthCodeRequired
		} else {
			err = errors.New("apple did not complete verification; try a fresh 2FA code")
		}
	} else if res.Data.FailureType == "" && res.Data.CustomerMessage == CustomerMessageAccountDisabled {
		err = NewErrorWithMetadata(errors.New("account is disabled"), res)
	} else if res.Data.FailureType != "" {
		if res.Data.CustomerMessage != "" {
			err = NewErrorWithMetadata(errors.New(res.Data.CustomerMessage), res)
		} else {
			err = NewErrorWithMetadata(errors.New("something went wrong"), res)
		}
	} else if res.StatusCode != gohttp.StatusOK || res.Data.PasswordToken == "" || res.Data.DirectoryServicesID == "" {
		err = fmt.Errorf("apple returned no usable authentication response (HTTP %d): missing account credentials or unexpected status; try again later or from another network", res.StatusCode)
	}

	return retry, redirect, err
}

func resolveAuthenticationRedirect(endpoint, location string) (string, error) {
	base, err := url.Parse(endpoint)
	if err != nil {
		return "", errors.New("invalid authentication redirect base URL")
	}

	reference, err := url.Parse(strings.TrimSpace(location))
	if err != nil || strings.TrimSpace(location) == "" {
		return "", errors.New("invalid authentication redirect location")
	}

	destination := base.ResolveReference(reference).String()
	if err := validateAuthenticationEndpoint(destination); err != nil {
		return "", fmt.Errorf("invalid authentication redirect: %w", err)
	}

	return destination, nil
}

func authenticationRedirectError(err error, status int, location string) error {
	// Keep useful routing diagnostics without retaining URL credentials, query
	// parameters, fragments, response bodies or arbitrary response headers.
	destination := "invalid URL"
	if parsed, parseErr := url.Parse(location); parseErr == nil {
		destination = (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: parsed.Path}).String()
	}

	return NewErrorWithMetadata(fmt.Errorf("%w (HTTP %d)", err, status), map[string]interface{}{
		"statusCode":  status,
		"destination": destination,
	})
}

func (t *appstore) loginRequest(email, password, authCode, guid, endpoint string, attempt int, signer ActionSigner) http.Request {
	return http.Request{
		Method:         http.MethodPOST,
		URL:            endpoint,
		ResponseFormat: http.ResponseFormatXML,
		ActionSigner:   signer,
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
		Payload: &http.XMLPayload{
			Content: map[string]interface{}{
				"appleId":  email,
				"attempt":  strconv.Itoa(attempt),
				"guid":     guid,
				"password": fmt.Sprintf("%s%s", password, strings.ReplaceAll(authCode, " ", "")),
				"rmp":      "0",
				"why":      "signIn",
			},
		},
	}
}

func authenticationRequestError(err error) error {
	var responseErr *http.UnexpectedResponseError
	if !errors.As(err, &responseErr) {
		return err
	}

	if responseErr.StatusCode == gohttp.StatusTooManyRequests {
		return fmt.Errorf("apple rate limited authentication; try again later: %w", err)
	}

	return fmt.Errorf("apple returned no usable authentication response; try again later or from another network: %w", err)
}

func authenticationRetryAfter(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.ParseUint(value, 10, 64); err == nil {
		// Saturate before converting to Duration to avoid overflow. A wait over
		// the budget ends this login rather than retrying before Apple's deadline.
		if seconds > uint64(maxAuthenticationRetryDelay/time.Second) {
			return maxAuthenticationRetryDelay + time.Second, true
		}

		return time.Duration(seconds) * time.Second, true
	}

	if date, err := gohttp.ParseTime(value); err == nil {
		return max(time.Duration(0), date.Sub(now)), true
	}

	return 0, false
}
