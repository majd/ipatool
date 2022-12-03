package keychain

import (
	"github.com/pkg/errors"
)

func (k *keychain) Get(key string) ([]byte, error) {
	item, err := k.keyring.Get(key)
	if err != nil {
		return nil, errors.Wrap(err, ErrGetKeychainItem.Error())
	}

	return item.Data, nil
}
