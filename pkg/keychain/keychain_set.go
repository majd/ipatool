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

	return errors.Wrap(err, "failed to set item in keyring")
}
