package appstore

import (
	"encoding/json"
	"github.com/majd/ipatool/pkg/log"
	"github.com/pkg/errors"
)

func (a *appstore) Info() error {
	data, err := a.keychain.Get("account")
	if err != nil {
		return errors.Wrap(err, "account was not found")
	}

	var account Account
	err = json.Unmarshal(data, &account)
	if err != nil {
		return errors.Wrap(err, "failed to unmarshall account data")
	}

	log.Info().
		Str("name", account.Name).
		Str("email", account.Email).
		Bool("succes", true).
		Send()

	return nil
}
