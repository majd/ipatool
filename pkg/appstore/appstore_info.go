package appstore

import (
	"encoding/json"
	"github.com/pkg/errors"
)

func (a *appstore) Info() error {
	acc, err := a.account()
	if err != nil {
		return errors.Wrap(err, ErrReadAccount.Error())
	}

	a.logger.Log().
		Str("name", acc.Name).
		Str("email", acc.Email).
		Bool("success", true).
		Send()

	return nil
}

func (a *appstore) account() (Account, error) {
	data, err := a.keychain.Get("account")
	if err != nil {
		return Account{}, errors.Wrap(err, ErrKeychainGet.Error())
	}

	var acc Account
	err = json.Unmarshal(data, &acc)
	if err != nil {
		return Account{}, errors.Wrap(err, ErrUnmarshal.Error())
	}

	return acc, err
}
