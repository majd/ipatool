package keychain

import (
	"github.com/99designs/keyring"
	"github.com/pkg/errors"
)

func (k *keychain) Set(key string, data []byte) error {
	err := k.keyring.Set(keyring.Item{
		Key:  key,
		Data: data,
	})
	if err != nil {
		return errors.Wrap(err, ErrSetKeychainItem.Error())
	}

	return nil
}
