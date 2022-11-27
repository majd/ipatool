package appstore

import (
	"github.com/majd/ipatool/pkg/http"
	"github.com/majd/ipatool/pkg/keychain"
	"github.com/majd/ipatool/pkg/log"
	"github.com/majd/ipatool/pkg/util"
	"io"
	"os"
)

type AppStore interface {
	Login(email, password, authCode string) error
	Info() error
	Revoke() error
	Search(term, countryCode, deviceFamily string, limit int64) error
	Purchase(bundleID, deviceFamily string) error
	Download(bundleID, deviceFamily, outputPath string, acquireLicense bool) error
}

type appstore struct {
	keychain       keychain.Keychain
	loginClient    http.Client[LoginResult]
	searchClient   http.Client[SearchResult]
	purchaseClient http.Client[PurchaseResult]
	downloadClient http.Client[DownloadResult]
	httpClient     http.Client[interface{}]
	ioReader       io.Reader
	machine        util.Machine
	os             util.OperatingSystem
	logger         log.Logger
	interactive    bool
}

type Args struct {
	Keychain        keychain.Keychain
	CookieJar       http.CookieJar
	Logger          log.Logger
	OperatingSystem util.OperatingSystem
	Machine         util.Machine
	Interactive     bool
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
		httpClient:     http.NewClient[interface{}](clientArgs),
		ioReader:       os.Stdin,
		machine:        args.Machine,
		os:             args.OperatingSystem,
		logger:         args.Logger,
		interactive:    args.Interactive,
	}
}
