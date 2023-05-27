package appstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/majd/ipatool/pkg/http"
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

	acc, err := t.login(input.Email, input.Password, input.AuthCode, guid, 0)
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

func (t *appstore) login(email, password, authCode, guid string, attempt int) (Account, error) {
	request := t.loginRequest(email, password, authCode, guid)
	res, err := t.loginClient.Send(request)

	if err != nil {
		return Account{}, fmt.Errorf("request failed: %w", err)
	}

	if attempt == 0 && res.Data.FailureType == FailureTypeInvalidCredentials {
		return t.login(email, password, authCode, guid, 1)
	}

	if res.Data.FailureType != "" && res.Data.CustomerMessage != "" {
		return Account{}, NewErrorWithMetadata(errors.New(res.Data.CustomerMessage), res)
	}

	if res.Data.FailureType != "" {
		return Account{}, NewErrorWithMetadata(errors.New("something went wrong"), res)
	}

	if res.Data.FailureType == "" && authCode == "" && res.Data.CustomerMessage == CustomerMessageBadLogin {
		return Account{}, ErrAuthCodeRequired
	}

	addr := res.Data.Account.Address
	acc := Account{
		Name:                strings.Join([]string{addr.FirstName, addr.LastName}, " "),
		Email:               res.Data.Account.Email,
		PasswordToken:       res.Data.PasswordToken,
		DirectoryServicesID: res.Data.DirectoryServicesID,
		StoreFront:          res.Headers[HTTPHeaderStoreFront],
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

func (t *appstore) loginRequest(email, password, authCode, guid string) http.Request {
	attempt := "4"
	if authCode != "" {
		attempt = "2"
	}

	return http.Request{
		Method:         http.MethodPOST,
		URL:            t.authDomain(authCode, guid),
		ResponseFormat: http.ResponseFormatXML,
		Headers: map[string]string{
			"Content-Type": "application/x-www-form-urlencoded",
		},
		Payload: &http.XMLPayload{
			Content: map[string]interface{}{
				"appleId":       email,
				"attempt":       attempt,
				"createSession": "true",
				"guid":          guid,
				"password":      fmt.Sprintf("%s%s", password, authCode),
				"rmp":           "0",
				"why":           "signIn",
			},
		},
	}
}

func (*appstore) authDomain(authCode, guid string) string {
	prefix := PrivateAppStoreAPIDomainPrefixWithoutAuthCode
	if authCode != "" {
		prefix = PrivateAppStoreAPIDomainPrefixWithAuthCode
	}

	return fmt.Sprintf(
		"https://%s-%s%s?guid=%s", prefix, PrivateAppStoreAPIDomain, PrivateAppStoreAPIPathAuthenticate, guid)
}
