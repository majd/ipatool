package appstore

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/majd/ipatool/pkg/http"
	"github.com/majd/ipatool/pkg/util"
	"github.com/pkg/errors"
	"strings"
	"syscall"
)

type LoginAddressResult struct {
	FirstName string `plist:"firstName,omitempty"`
	LastName  string `plist:"lastName,omitempty"`
}

type LoginAccountResult struct {
	Email   string             `plist:"appleId,omitempty"`
	Address LoginAddressResult `plist:"address,omitempty"`
}

type LoginResult struct {
	FailureType         string             `plist:"failureType,omitempty"`
	CustomerMessage     string             `plist:"customerMessage,omitempty"`
	Account             LoginAccountResult `plist:"accountInfo,omitempty"`
	DirectoryServicesID string             `plist:"dsPersonId,omitempty"`
	PasswordToken       string             `plist:"passwordToken,omitempty"`
}

func (a *appstore) Login(email, password, authCode string) error {
	if password == "" && !a.interactive {
		return ErrPasswordRequired
	}

	if password == "" && a.interactive {
		a.logger.Log().Msg("enter password:")

		var err error
		password, err = a.promptForPassword()
		if err != nil {
			return errors.Wrap(err, ErrGetData.Error())
		}
	}

	macAddr, err := a.machine.MacAddress()
	if err != nil {
		return errors.Wrap(err, ErrGetMAC.Error())
	}

	guid := strings.ReplaceAll(strings.ToUpper(macAddr), ":", "")
	a.logger.Verbose().Str("mac", macAddr).Str("guid", guid).Send()

	acc, err := a.login(email, password, authCode, guid, 0, false)
	if err != nil {
		return errors.Wrap(err, ErrLogin.Error())
	}

	a.logger.Log().
		Str("name", acc.Name).
		Str("email", acc.Email).
		Bool("success", true).
		Send()

	return nil
}

func (a *appstore) login(email, password, authCode, guid string, attempt int, failOnAuthCodeRequirement bool) (Account, error) {
	a.logger.Verbose().
		Int("attempt", attempt).
		Str("password", password).
		Str("email", email).
		Str("authCode", util.IfEmpty(authCode, "<nil>")).
		Msg("sending login request")

	request := a.loginRequest(email, password, authCode, guid)
	res, err := a.loginClient.Send(request)
	if err != nil {
		return Account{}, errors.Wrap(err, ErrRequest.Error())
	}

	if attempt == 0 && res.Data.FailureType == FailureTypeInvalidCredentials {
		return a.login(email, password, authCode, guid, 1, failOnAuthCodeRequirement)
	}

	if res.Data.FailureType != "" && res.Data.CustomerMessage != "" {
		a.logger.Verbose().Interface("response", res).Send()
		return Account{}, errors.New(res.Data.CustomerMessage)
	}

	if res.Data.FailureType != "" {
		a.logger.Verbose().Interface("response", res).Send()
		return Account{}, ErrGeneric
	}

	if res.Data.FailureType == "" && authCode == "" && res.Data.CustomerMessage == CustomerMessageBadLogin {
		if failOnAuthCodeRequirement {
			return Account{}, ErrAuthCodeRequired
		}

		if a.interactive {
			a.logger.Log().Msg("enter 2FA code:")
			authCode, err = a.promptForAuthCode()
			if err != nil {
				return Account{}, errors.Wrap(err, ErrGetData.Error())
			}

			return a.login(email, password, authCode, guid, 0, failOnAuthCodeRequirement)
		} else {
			a.logger.Log().Msg("2FA code is required; run the command again and supply a code using the `--auth-code` flag")
			return Account{}, nil
		}
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
		return Account{}, errors.Wrap(err, ErrMarshal.Error())
	}

	err = a.keychain.Set("account", data)
	if err != nil {
		return Account{}, errors.Wrap(err, ErrSetKeychainItem.Error())
	}

	return acc, nil
}

func (a *appstore) loginRequest(email, password, authCode, guid string) http.Request {
	attempt := "4"
	if authCode != "" {
		attempt = "2"
	}

	return http.Request{
		Method:         http.MethodPOST,
		URL:            a.authDomain(authCode, guid),
		ResponseFormat: http.ResponseFormatXML,
		Headers: map[string]string{
			"User-Agent":   "Configurator/2.15 (Macintosh; OperatingSystem X 11.0.0; 16G29) AppleWebKit/2603.3.8",
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

func (a *appstore) promptForAuthCode() (string, error) {
	reader := bufio.NewReader(a.ioReader)
	authCode, err := reader.ReadString('\n')
	if err != nil {
		return "", errors.Wrap(err, ErrGetData.Error())
	}

	return strings.Trim(authCode, "\n"), nil
}

func (*appstore) authDomain(authCode, guid string) string {
	prefix := PriavteAppStoreAPIDomainPrefixWithoutAuthCode
	if authCode != "" {
		prefix = PriavteAppStoreAPIDomainPrefixWithAuthCode
	}

	return fmt.Sprintf(
		"https://%s-%s%s?guid=%s", prefix, PrivateAppStoreAPIDomain, PrivateAppStoreAPIPathAuthenticate, guid)
}

func (a *appstore) promptForPassword() (string, error) {
	password, err := a.machine.ReadPassword(syscall.Stdin)
	if err != nil {
		return "", errors.Wrap(err, ErrGetData.Error())
	}

	return string(password), nil
}
