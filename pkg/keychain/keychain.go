package keychain

//go:generate go run go.uber.org/mock/mockgen -source=keychain.go -destination=keychain_mock.go -package keychain
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

func New(args Args) Keychain {
	return &keychain{
		keyring: args.Keyring,
	}
}
