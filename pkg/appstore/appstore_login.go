package appstore

import (
	"encoding/json"
	"errors"
	"fmt"
	gohttp "net/http"
	"strconv"
	"strings"
	"time"

	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/util"
)

var (
	ErrAuthCodeRequired = errors.New("auth code is required")
)

const (
	maxAuthenticationRequestAttempts = 3
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

	acc, loginErr := t.login(input.Email, input.Password, input.AuthCode, guid, bag.SAPConfig.AuthEndpoint, signer)
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
	redirect := ""

	var (
		err error
		res http.Result[loginResult]
	)

	retry := true

	for attempt := 1; retry && attempt <= 4; attempt++ {
		requestAttempt := attempt
		if redirect != "" {
			// The pod redirect is part of the same authentication attempt. Apple
			// expects the original XML plist body, including its attempt value.
			requestAttempt = 1
		}

		request := t.loginRequest(email, password, authCode, guid, endpoint, requestAttempt, signer)
		request.URL, _ = util.IfEmpty(redirect, request.URL), ""
		res, err = t.sendAuthenticationRequest(request)

		if err != nil {
			stage := "sign-in"
			if authCode != "" {
				stage = "2FA verification"
			}

			if redirect != "" {
				stage += " at Store pod"
			}

			return Account{}, fmt.Errorf("%s request failed: %w", stage, err)
		}

		if retry, redirect, err = t.parseLoginResponse(&res, attempt, authCode); err != nil {
			return Account{}, err
		}
	}

	if retry {
		return Account{}, NewErrorWithMetadata(errors.New("too many attempts"), res)
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

		statuses = append(statuses, strconv.Itoa(status))

		if attempt == maxAuthenticationRequestAttempts {
			return result, fmt.Errorf(
				"authentication request failed after %d attempts (HTTP %s): %w",
				maxAuthenticationRequestAttempts, strings.Join(statuses, ", "), authenticationRequestError(err),
			)
		}

		delay := min(authenticationRetryDelay<<(attempt-1), maxAuthenticationRetryDelay)

		var responseErr *http.UnexpectedResponseError
		if errors.As(err, &responseErr) {
			if requested, ok := authenticationRetryAfter(responseErr.RetryAfter, time.Now()); ok {
				if requested > maxAuthenticationRetryDelay {
					return result, fmt.Errorf("apple requested a wait longer than %s; try again later: %w", maxAuthenticationRetryDelay, err)
				}

				// Retry-After takes precedence over the fallback backoff.
				delay = max(requested, time.Second)
			}
		}

		sleep(delay)
	}
}

func retryableAuthenticationError(err error) (int, bool) {
	var responseErr *http.UnexpectedResponseError
	if !errors.As(err, &responseErr) {
		return 0, false
	}

	status := responseErr.StatusCode
	retry := status == gohttp.StatusNoContent ||
		status == gohttp.StatusNotFound ||
		status == gohttp.StatusTooManyRequests ||
		status/100 == 5

	return status, retry
}

func (t *appstore) parseLoginResponse(res *http.Result[loginResult], attempt int, authCode string) (bool, string, error) {
	var (
		retry    bool
		redirect string
		err      error
	)

	if res.StatusCode == gohttp.StatusFound {
		if redirect, err = res.GetHeader("location"); err != nil {
			err = fmt.Errorf("failed to retrieve redirect location: %w", err)
		} else if err = validateAuthenticationEndpoint(redirect); err != nil {
			err = fmt.Errorf("invalid authentication redirect: %w", err)
		} else {
			retry = true
		}
	} else if attempt == 1 && res.Data.FailureType == FailureTypeInvalidCredentials {
		retry = true
	} else if res.Data.FailureType == "" && authCode == "" && res.Data.CustomerMessage == CustomerMessageBadLogin {
		err = ErrAuthCodeRequired
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
