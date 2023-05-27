package keychain

import (
	"fmt"
)

func (k *keychain) Get(key string) ([]byte, error) {
	item, err := k.keyring.Get(key)
	if err != nil {
		return nil, fmt.Errorf("failed to get item: %w", err)
	}

	return item.Data, nil
}
