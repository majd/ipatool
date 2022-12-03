package keychain

import "github.com/pkg/errors"

func (k *keychain) Remove(key string) error {
	err := k.keyring.Remove(key)
	if err != nil {
		return errors.Wrap(err, ErrRemoveKeychainItem.Error())
	}

	return nil
}
