package keychain

import (
	"fmt"
)

func (k *keychain) Remove(key string) error {
	err := k.keyring.Remove(key)
	if err != nil {
		return fmt.Errorf("failed to remove item: %w", err)
	}

	return nil
}
