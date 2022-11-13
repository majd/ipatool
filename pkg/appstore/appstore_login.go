package appstore

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/majd/ipatool/pkg/http"
	"github.com/majd/ipatool/pkg/util"
	"github.com/pkg/errors"
	"strings"
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
	macAddr, err := a.machine.MacAddress()
	if err != nil {
		return errors.Wrap(err, "failed to read MAC address")
	}

	guid := strings.ReplaceAll(strings.ToUpper(macAddr), ":", "")
	a.logger.Debug().
		Str("mac", macAddr).
		Str("guid", guid).
		Send()

	return a.login(email, password, authCode, guid, 0)
}

func (a *appstore) login(email, password, authCode, guid string, attempt int) error {
	a.logger.Debug().
		Int("attempt", attempt).
		Str("password", password).
		Str("email", email).
		Str("authCode", util.IfEmpty(authCode, "<nil>")).
		Msg("sending login request")

	request := a.loginRequest(email, password, authCode, guid)
	res, err := a.loginClient.Send(request)
	if err != nil {
		return errors.Wrap(err, "request failed")
	}

	if attempt == 0 && res.Data.FailureType == FailureTypeInvalidCredentials {
		return a.login(email, password, authCode, guid, 1)
	}

	if res.Data.FailureType != "" && res.Data.CustomerMessage != "" {
		a.logger.Debug().
			Interface("response", res).
			Send()
		return errors.New(res.Data.CustomerMessage)
	}

	if res.Data.FailureType != "" {
		a.logger.Debug().
			Interface("response", res).
			Send()
		return errors.New("unknown error occurred")
	}

	if res.Data.FailureType == "" && authCode == "" && res.Data.CustomerMessage == CustomerMessageBadLogin {
		a.logger.Warn().
			Msg("enter 2FA code:")
		authCode, err = a.promptForAuthCode()
		if err != nil {
			return errors.Wrap(err, "failed to prompt for 2FA code")
		}

		return a.login(email, password, authCode, guid, 0)
	}

	addr := res.Data.Account.Address
	name := strings.Join([]string{addr.FirstName, addr.LastName}, " ")

	data, err := json.Marshal(Account{
		Name:                name,
		Email:               res.Data.Account.Email,
		PasswordToken:       res.Data.PasswordToken,
		DirectoryServicesID: res.Data.DirectoryServicesID,
	})
	if err != nil {
		return errors.Wrap(err, "failed to marshall account data")
	}

	err = a.keychain.Set("account", data)
	if err != nil {
		return errors.Wrap(err, "failed to save account data in keychain")
	}

	a.logger.Info().
		Str("name", name).
		Str("email", res.Data.Account.Email).
		Bool("success", true).
		Send()

	return nil
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
			"User-Agent":   "Configurator/2.15 (Macintosh; OS X 11.0.0; 16G29) AppleWebKit/2603.3.8",
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
		return "", errors.Wrap(err, "failed to read string from stdin")
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
