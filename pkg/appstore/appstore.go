package appstore

import (
	"github.com/majd/ipatool/pkg/http"
	"github.com/majd/ipatool/pkg/keychain"
	"github.com/majd/ipatool/pkg/log"
	"github.com/majd/ipatool/pkg/util"
	"io"
	gohttp "net/http"
	"os"
)

type AppStore interface {
	Login(email, password, authCode string) error
	Info() error
	Revoke() error
	Search(term, countryCode, deviceFamily string, limit int64) error
	Purchase(bundleID, deviceFamily string) error
	Download(bundleID, deviceFamily, outputPath, logFormat string, acquireLicense bool) error
}

type appstore struct {
	keychain       keychain.Keychain
	loginClient    http.Client[LoginResult]
	searchClient   http.Client[SearchResult]
	purchaseClient http.Client[PurchaseResult]
	downloadClient http.Client[DownloadResult]
	httpClient     gohttp.Client
	ioReader       io.Reader
	machine        util.Machine
	logger         log.Logger
	interactive    bool
}

type Args struct {
	Keychain    keychain.Keychain
	CookieJar   http.CookieJar
	Logger      log.Logger
	Interactive bool
}

func NewAppStore(args *Args) AppStore {
	clientArgs := &http.Args{
		CookieJar: args.CookieJar,
	}

	return &appstore{
		keychain:       args.Keychain,
		loginClient:    http.NewClient[LoginResult](clientArgs),
		searchClient:   http.NewClient[SearchResult](clientArgs),
		purchaseClient: http.NewClient[PurchaseResult](clientArgs),
		downloadClient: http.NewClient[DownloadResult](clientArgs),
		ioReader:       os.Stdin,
		machine:        util.NewMachine(),
		logger:         args.Logger,
		interactive:    args.Interactive,
	}
}
