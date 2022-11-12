package keychain

import "github.com/pkg/errors"

func (k *keychain) Remove(key string) error {
	err := k.keyring.Remove(key)
	return errors.Wrap(err, "failed to remove item from keyring")
}
