package appstore

import (
	"github.com/pkg/errors"
)

func (a *appstore) Revoke() error {
	err := a.keychain.Remove("account")
	if err != nil {
		return errors.Wrap(err, ErrKeychainRemove.Error())
	}

	a.logger.Log().Bool("success", false).Send()
	return nil
}
