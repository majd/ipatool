package appstore

import (
	"encoding/json"
	"github.com/pkg/errors"
)

type InfoOutput struct {
	Name  string
	Email string
}

func (a *appstore) Info() (InfoOutput, error) {
	acc, err := a.account()
	if err != nil {
		return InfoOutput{}, errors.Wrap(err, ErrGetAccount.Error())
	}

	return InfoOutput{
		Name:  acc.Name,
		Email: acc.Email,
	}, nil
}

func (a *appstore) account() (Account, error) {
	data, err := a.keychain.Get("account")
	if err != nil {
		return Account{}, errors.Wrap(err, ErrGetKeychainItem.Error())
	}

	var acc Account
	err = json.Unmarshal(data, &acc)
	if err != nil {
		return Account{}, errors.Wrap(err, ErrUnmarshal.Error())
	}

	return acc, err
}
