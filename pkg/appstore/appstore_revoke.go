package appstore

import (
	"encoding/json"
	"fmt"
)

func (t *appstore) Revoke() error {
	var accountStorage AccountStorage

	data, err := t.keychain.Get(AccountKey)
	if err != nil {
		return fmt.Errorf("failed to remove account from keychain: %w", err)
	}
	err = json.Unmarshal(data, &accountStorage)
	if err != nil {
		return fmt.Errorf("failed to unmarshal account data: %w", err)
	}

	var remain []Account
	for _, acc := range accountStorage.Accounts {
		if acc.Email != accountStorage.Current {
			remain = append(remain, acc)
		}
	}
	accountStorage.Accounts = remain

	updatedData, err := json.Marshal(accountStorage)
	if err != nil {
		return fmt.Errorf("failed to marshal updated account data: %w", err)
	}

	err = t.keychain.Set(AccountKey, updatedData)
	if err != nil {
		return fmt.Errorf("failed to update account data in keychain: %w", err)
	}

	return nil
}
