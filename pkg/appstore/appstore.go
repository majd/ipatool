package appstore

import (
	"fmt"
	"strings"

	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/keychain"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	"github.com/majd/ipatool/v2/pkg/util/operatingsystem"
)

type AppStore interface {
	// Login authenticates with the App Store.
	Login(input LoginInput) (LoginOutput, error)
	// AccountInfo returns the information of the authenticated account.
	AccountInfo() (AccountInfoOutput, error)
	// Revoke revokes the active credentials.
	Revoke() error
	// Lookup looks apps up based on the specified bundle identifier.
	Lookup(input LookupInput) (LookupOutput, error)
	// Search searches the App Store for apps matching the specified term.
	Search(input SearchInput) (SearchOutput, error)
	// Purchase acquires a license for the desired app.
	// Note: only free apps are supported.
	Purchase(input PurchaseInput) error
	// Download downloads the IPA package from the App Store to the desired location.
	Download(input DownloadInput) (DownloadOutput, error)
	// ReplicateSinf replicates the sinf for the IPA package.
	ReplicateSinf(input ReplicateSinfInput) error
}

type appstore struct {
	keychain       keychain.Keychain
	loginClient    http.Client[loginResult]
	searchClient   http.Client[searchResult]
	purchaseClient http.Client[purchaseResult]
	downloadClient http.Client[downloadResult]
	httpClient     http.Client[interface{}]
	machine        machine.Machine
	os             operatingsystem.OperatingSystem
	guid           string
}

type Args struct {
	Keychain        keychain.Keychain
	CookieJar       http.CookieJar
	OperatingSystem operatingsystem.OperatingSystem
	Machine         machine.Machine
	Guid            string
}

func NewAppStore(args Args) AppStore {
	clientArgs := http.Args{
		CookieJar: args.CookieJar,
	}

	return &appstore{
		keychain:       args.Keychain,
		loginClient:    http.NewClient[loginResult](clientArgs),
		searchClient:   http.NewClient[searchResult](clientArgs),
		purchaseClient: http.NewClient[purchaseResult](clientArgs),
		downloadClient: http.NewClient[downloadResult](clientArgs),
		httpClient:     http.NewClient[interface{}](clientArgs),
		machine:        args.Machine,
		os:             args.OperatingSystem,
		guid:           args.Guid,
	}
}

func (t *appstore) getGuid() (string, error) {
	if t.guid == "" {
		if macAddr, err := t.machine.MacAddress(); err != nil {
			return "", fmt.Errorf("failed to get mac address: %w", err)
		} else {
			t.guid = strings.ReplaceAll(strings.ToUpper(macAddr), ":", "")
		}
	}
	return t.guid, nil
}
