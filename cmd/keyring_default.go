//go:build !ios

package cmd

import (
	"github.com/byteness/keyring"
	"github.com/majd/ipatool/v2/pkg/keychain"
)

func openKeyring(config keyring.Config) (keychain.Keyring, error) {
	return keyring.Open(config) //nolint:wrapcheck
}
