package keychain

import "github.com/pkg/errors"

var (
	ErrGetKeychainItem    = errors.New("failed to get item from keyring")
	ErrRemoveKeychainItem = errors.New("failed to remove item from keyring")
	ErrSetKeychainItem    = errors.New("failed to set item in keyring")
)
