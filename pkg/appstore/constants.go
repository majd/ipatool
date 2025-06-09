package appstore

const (
	FailureTypeInvalidCredentials     = "-5000"
	FailureTypePasswordTokenExpired   = "2034"
	FailureTypeLicenseNotFound        = "9610"
	FailureTypeTemporarilyUnavailable = "2059"

	CustomerMessageBadLogin             = "MZFinance.BadLogin.Configurator_message"
	CustomerMessageAccountDisabled      = "Your account is disabled."
	CustomerMessageSubscriptionRequired = "Subscription Required"

	iTunesAPIDomain     = "itunes.apple.com"
	iTunesAPIPathSearch = "/search"
	iTunesAPIPathLookup = "/lookup"

	PrivateAppStoreAPIDomainPrefixWithoutAuthCode = "p25"
	PrivateAppStoreAPIDomainPrefixWithAuthCode    = "p71"
	PrivateAppStoreAPIDomain                      = "buy." + iTunesAPIDomain
	PrivateAppStoreAPIPathAuthenticate            = "/WebObjects/MZFinance.woa/wa/authenticate"
	PrivateAppStoreAPIPathPurchase                = "/WebObjects/MZFinance.woa/wa/buyProduct"
	PrivateAppStoreAPIPathDownload                = "/WebObjects/MZFinance.woa/wa/volumeStoreDownloadProduct"

	HTTPHeaderStoreFront = "X-Set-Apple-Store-Front"

	PricingParameterAppStore    = "STDQ"
	PricingParameterAppleArcade = "GAME"
)
