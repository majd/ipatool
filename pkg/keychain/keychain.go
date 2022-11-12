package keychain

import "github.com/99designs/keyring"

//go:generate mockgen -source=keychain.go -destination=../../mocks/keychain_mock.go -package=mocks

type Keyring interface {
	Get(key string) (keyring.Item, error)
	Set(item keyring.Item) error
	Remove(key string) error
}

type Keychain interface {
	Get(key string) ([]byte, error)
	Set(key string, data []byte) error
	Remove(key string) error
}

type keychain struct {
	keyring Keyring
}

type Args struct {
	Keyring Keyring
}

func NewKeychain(args *Args) Keychain {
	return &keychain{
		keyring: args.Keyring,
	}
}
