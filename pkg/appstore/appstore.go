package appstore

import (
	"github.com/majd/ipatool/pkg/http"
	"github.com/majd/ipatool/pkg/keychain"
	"github.com/majd/ipatool/pkg/util"
	"io"
	"os"
)

type AppStore interface {
	Login(email, password, authCode string) error
	Info() error
	Revoke() error
}

type appstore struct {
	keychain    keychain.Keychain
	loginClient http.Client[LoginResult]
	ioReader    io.Reader
	machine     util.Machine
}

type Args struct {
	Keychain  keychain.Keychain
	CookieJar http.CookieJar
}

func NewAppStore(args *Args) AppStore {
	return &appstore{
		keychain: args.Keychain,
		loginClient: http.NewClient[LoginResult](&http.Args{
			CookieJar: args.CookieJar,
		}),
		ioReader: os.Stdin,
		machine:  util.NewMachine(),
	}
}
