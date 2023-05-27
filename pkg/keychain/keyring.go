package keychain

import "github.com/99designs/keyring"

//go:generate go run github.com/golang/mock/mockgen -source=keyring.go -destination=keyring_mock.go -package keychain
type Keyring interface {
	Get(key string) (keyring.Item, error)
	Set(item keyring.Item) error
	Remove(key string) error
}
