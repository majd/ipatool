package appstore

import (
	"time"

	"github.com/majd/ipatool/v2/internal/store"

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
	// Lookup looks apps up by bundle identifier or numeric app ID.
	Lookup(input LookupInput) (LookupOutput, error)
	// Search searches the App Store for apps matching the specified term.
	Search(input SearchInput) (SearchOutput, error)
	// OwnedApps lists apps owned by the authenticated account.
	OwnedApps(input OwnedAppsInput) (OwnedAppsOutput, error)
	// Purchase acquires a license for the desired app.
	// Note: only free apps are supported.
	Purchase(input PurchaseInput) error
	// Download downloads the app package from the App Store to the desired location.
	Download(input DownloadInput) (DownloadOutput, error)
	// ReplicateSinf replicates the sinf for the IPA package.
	ReplicateSinf(input ReplicateSinfInput) error
	// VersionHistory lists the available versions of the specified app.
	ListVersions(input ListVersionsInput) (ListVersionsOutput, error)
	// GetVersionMetadata returns the metadata for the specified version.
	GetVersionMetadata(input GetVersionMetadataInput) (GetVersionMetadataOutput, error)
	// Bag fetches the bag which contains endpoint definitions.
	Bag(input BagInput) (BagOutput, error)
}

type appstore struct {
	keychain            keychain.Keychain
	loginClient         http.Client[loginResult]
	searchClient        http.Client[searchResult]
	purchaseClient      http.Client[purchaseResult]
	downloadClient      http.Client[downloadResult]
	kbsyncGenerator     kbsyncGenerator
	kbsyncCache         kbsyncCache
	platformClient      http.Client[platformVersionLookupResult]
	storefrontClient    http.Client[[]byte]
	bagClient           http.Client[bagResult]
	ownedAppsClient     http.Client[[]byte]
	httpClient          http.Client[interface{}]
	macDecrypterFactory macPackageDecrypterFactory
	actionSignerFactory ActionSignerFactory
	authRetrySleep      func(time.Duration)
	machine             machine.Machine
	os                  operatingsystem.OperatingSystem
}

type Args struct {
	Keychain            keychain.Keychain
	CookieJar           http.CookieJar
	OperatingSystem     operatingsystem.OperatingSystem
	Machine             machine.Machine
	ActionSignerFactory ActionSignerFactory
}

// NewAppStore creates a client using the compiled Store profile.
func NewAppStore(args Args) AppStore {
	var macAddress func() (string, error)
	if args.Machine != nil {
		macAddress = args.Machine.MacAddress
	}

	backend := store.New(args.CookieJar, macAddress)
	clientArgs := http.Args{
		CookieJar: args.CookieJar,
		Executor:  backend.Client("request"),
	}

	actionSignerFactory := args.ActionSignerFactory
	if actionSignerFactory == nil {
		actionSignerFactory = frameworkActionSignerFactory(backend.Client("api"))
	}

	return &appstore{
		keychain:            args.Keychain,
		loginClient:         http.NewClient[loginResult](http.Args{CookieJar: args.CookieJar, Authentication: true, Executor: backend.Client("authenticate")}),
		searchClient:        http.NewClient[searchResult](clientArgs),
		purchaseClient:      http.NewClient[purchaseResult](http.Args{CookieJar: args.CookieJar, Executor: backend.Client("purchase")}),
		downloadClient:      http.NewClient[downloadResult](clientArgs),
		kbsyncGenerator:     defaultKBSyncGenerator,
		platformClient:      http.NewClient[platformVersionLookupResult](clientArgs),
		storefrontClient:    http.NewClient[[]byte](clientArgs),
		bagClient:           http.NewClient[bagResult](http.Args{CookieJar: args.CookieJar, Timeout: http.DefaultAuthenticationTimeout, Executor: backend.Client("bag")}),
		ownedAppsClient:     http.NewClient[[]byte](clientArgs),
		httpClient:          http.NewClient[interface{}](http.Args{CookieJar: args.CookieJar, Executor: backend.Client("transfer")}),
		macDecrypterFactory: defaultMacPackageDecrypterFactory,
		actionSignerFactory: actionSignerFactory,
		authRetrySleep:      time.Sleep,
		machine:             args.Machine,
		os:                  args.OperatingSystem,
	}
}

// NewAppStoreWithSetup is retained for source compatibility. The callback is no
// longer invoked because Store profiles are precomputed.
//
// Deprecated: Use NewAppStore.
func NewAppStoreWithSetup(args Args, _ func()) AppStore { return NewAppStore(args) }
