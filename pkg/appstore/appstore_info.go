package appstore

import (
	"encoding/json"
	"github.com/pkg/errors"
)

func (a *appstore) Info() error {
	acc, err := a.account()
	if err != nil {
		return errors.Wrap(err, ErrorReadAccount.Error())
	}

	a.logger.Info().
		Str("name", acc.Name).
		Str("email", acc.Email).
		Bool("succes", true).
		Send()

	return nil
}

func (a *appstore) account() (Account, error) {
	data, err := a.keychain.Get("account")
	if err != nil {
		return Account{}, errors.Wrap(err, ErrorKeychainGet.Error())
	}

	var acc Account
	err = json.Unmarshal(data, &acc)
	if err != nil {
		return Account{}, errors.Wrap(err, ErrorUnmarshal.Error())
	}

	return acc, err
}
