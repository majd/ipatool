package appstore

import (
	"encoding/json"
	"fmt"
)

type AccountInfoOutput struct {
	Account Account
}

func (t *appstore) AccountInfo() (AccountInfoOutput, error) {
	data, err := t.keychain.Get("account")
	if err != nil {
		return AccountInfoOutput{}, fmt.Errorf("failed to get account: %w", err)
	}

	var acc Account

	err = json.Unmarshal(data, &acc)
	if err != nil {
		return AccountInfoOutput{}, fmt.Errorf("failed to unmarshal json: %w", err)
	}

	return AccountInfoOutput{
		Account: acc,
	}, nil
}
