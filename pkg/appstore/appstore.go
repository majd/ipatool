package appstore

import (
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
	// VersionHistory lists the available versions of the specified app.
	ListVersions(input ListVersionsInput) (ListVersionsOutput, error)
	// GetVersionMetadata returns the metadata for the specified version.
	GetVersionMetadata(input GetVersionMetadataInput) (GetVersionMetadataOutput, error)
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
}

type Args struct {
	Keychain        keychain.Keychain
	CookieJar       http.CookieJar
	OperatingSystem operatingsystem.OperatingSystem
	Machine         machine.Machine
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
	}
}
