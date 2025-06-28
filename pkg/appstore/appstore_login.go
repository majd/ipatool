package appstore

import (
	"encoding/json"
	"errors"
	"fmt"
	gohttp "net/http"
	"strconv"
	"strings"

	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/util"
)

var (
	ErrAuthCodeRequired = errors.New("auth code is required")
)

type LoginInput struct {
	Email    string
	Password string
	AuthCode string
}

type LoginOutput struct {
	Account Account
}

func (t *appstore) Login(input LoginInput) (LoginOutput, error) {
	macAddr, err := t.machine.MacAddress()
	if err != nil {
		return LoginOutput{}, fmt.Errorf("failed to get mac address: %w", err)
	}

	guid := strings.ReplaceAll(strings.ToUpper(macAddr), ":", "")

	acc, err := t.login(input.Email, input.Password, input.AuthCode, guid)
	if err != nil {
		return LoginOutput{}, err
	}

	return LoginOutput{
		Account: acc,
	}, nil
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

func (t *appstore) login(email, password, authCode, guid string) (Account, error) {
	redirect := ""

	var (
		err error
		res http.Result[loginResult]
	)

	retry := true

	for attempt := 1; retry && attempt <= 4; attempt++ {
		request := t.loginRequest(email, password, authCode, guid, attempt)
		request.URL, _ = util.IfEmpty(redirect, request.URL), ""
		res, err = t.loginClient.Send(request)

		if err != nil {
			return Account{}, fmt.Errorf("request failed: %w", err)
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

	addr := res.Data.Account.Address
	acc := Account{
		Name:                strings.Join([]string{addr.FirstName, addr.LastName}, " "),
		Email:               res.Data.Account.Email,
		PasswordToken:       res.Data.PasswordToken,
		DirectoryServicesID: res.Data.DirectoryServicesID,
		StoreFront:          sf,
		Password:            password,
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

func (t *appstore) parseLoginResponse(res *http.Result[loginResult], attempt int, authCode string) (bool, string, error) {
	var (
		retry    bool
		redirect string
		err      error
	)

	if res.StatusCode == gohttp.StatusFound {
		if redirect, err = res.GetHeader("location"); err != nil {
			err = fmt.Errorf("failed to retrieve redirect location: %w", err)
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
		err = NewErrorWithMetadata(errors.New("something went wrong"), res)
	}

	return retry, redirect, err
}

func (t *appstore) loginRequest(email, password, authCode, guid string, attempt int) http.Request {
	return http.Request{
		Method:         http.MethodPOST,
		URL:            fmt.Sprintf("https://%s%s", PrivateAppStoreAPIDomain, PrivateAppStoreAPIPathAuthenticate),
		ResponseFormat: http.ResponseFormatXML,
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
