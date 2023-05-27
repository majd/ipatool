package keychain

import (
	"fmt"

	"github.com/99designs/keyring"
)

func (k *keychain) Set(key string, data []byte) error {
	err := k.keyring.Set(keyring.Item{
		Key:  key,
		Data: data,
	})
	if err != nil {
		return fmt.Errorf("failed to set item: %w", err)
	}

	return nil
}
