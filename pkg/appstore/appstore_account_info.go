package appstore

import (
	"encoding/json"
	"fmt"
)

type AccountInfoOutput struct {
	Account Account
}

type AccountsInfoOutput struct {
	Accounts []Account
	Current  string
}

// Try read as Multiple Accounts storage
// otherwise as single account
func (t *appstore) AccountInfo() (AccountInfoOutput, error) {
	data, err := t.keychain.Get(AccountKey)
	if err != nil {
		return AccountInfoOutput{}, fmt.Errorf("failed to get account: %w", err)
	}

	var acc Account
	var accounts AccountStorage

	err = json.Unmarshal(data, &accounts)
	if err == nil && len(accounts.Accounts) > 0 {
		// Return current account
		for _, a := range accounts.Accounts {
			if a.Email == accounts.Current {
				return AccountInfoOutput{
					Account: a,
				}, nil
			}
		}
	}

	// Fallback to single account
	err = json.Unmarshal(data, &acc)
	if err != nil {
		return AccountInfoOutput{}, fmt.Errorf("failed to unmarshal json: %w", err)
	}

	return AccountInfoOutput{
		Account: acc,
	}, nil
}

func (t *appstore) AccountsInfo() (AccountsInfoOutput, error) {
	data, err := t.keychain.Get(AccountKey)
	if err != nil {
		return AccountsInfoOutput{}, fmt.Errorf("failed to get account storage: %w", err)
	}

	var storage AccountStorage

	err = json.Unmarshal(data, &storage)
	if err != nil {
		return AccountsInfoOutput{}, fmt.Errorf("failed to unmarshal json: %w", err)
	}

	return AccountsInfoOutput(storage), nil
}

// read from keychain and save to storage
// if is v2 data, just save it
// if is old data, convert and save it
func (t *appstore) saveAccount(acc Account) (Account, error) {

	var accountStorage AccountStorage
	var accInKeychain Account

	rootData, err := t.keychain.Get(AccountKey)
	if err != nil {
		// Ignore error if account does not exist yet
		return Account{}, err
	}
	err = json.Unmarshal(rootData, &accountStorage)

	if err != nil {
		err = json.Unmarshal(rootData, &accInKeychain)
		if err == nil {
			accountStorage = AccountStorage{
				Accounts: []Account{accInKeychain},
				Current:  accInKeychain.Email,
			}
		}
	}

	// handle deduplicate accounts
	var found bool
	for _, a := range accountStorage.Accounts {
		if a.Email == acc.Email {
			found = true
			break
		}
	}
	if !found {
		accountStorage.Accounts = append(accountStorage.Accounts, acc)
	}
	accountStorage.Current = acc.Email

	rootData, err = json.Marshal(accountStorage)
	if err != nil {
		return Account{}, fmt.Errorf("failed to marshal json: %w", err)
	}
	err = t.keychain.Set(AccountKey, rootData)
	if err != nil {
		return Account{}, fmt.Errorf("failed to save account storage in keychain: %w", err)
	}
	return acc, nil
}

func (t *appstore) SwitchAccount(email string) (Account, error) {
	accountStorage, err := t.AccountsInfo()
	if err != nil {
		return Account{}, fmt.Errorf("failed to get accounts info: %w", err)
	}

	var found bool
	var res Account
	for _, acc := range accountStorage.Accounts {
		if acc.Email == email {
			found = true
			res = acc
			break
		}
	}
	if !found {
		return Account{}, fmt.Errorf("account with email %s not found", email)
	}

	accountStorage.Current = email

	rootData, err := json.Marshal(accountStorage)
	if err != nil {
		return Account{}, fmt.Errorf("failed to marshal json: %w", err)
	}
	err = t.keychain.Set(AccountKey, rootData)
	if err != nil {
		return Account{}, fmt.Errorf("failed to save account storage in keychain: %w", err)
	}

	return res, nil
}
