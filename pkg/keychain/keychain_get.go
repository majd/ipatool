package keychain

import (
	"github.com/pkg/errors"
)

func (k *keychain) Get(key string) ([]byte, error) {
	item, err := k.keyring.Get(key)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get item from keyring")
	}

	return item.Data, nil
}
