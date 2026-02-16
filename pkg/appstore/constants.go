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

	PrivateInitDomain = "init." + iTunesAPIDomain
	PrivateInitPath   = "/bag.xml"

	PrivateAppStoreAPIDomainPrefixWithoutAuthCode = "p25"
	PrivateAppStoreAPIDomainPrefixWithAuthCode    = "p71"
	PrivateAppStoreAPIDomain                      = "buy." + iTunesAPIDomain
	PrivateAppStoreAPIPathPurchase                = "/WebObjects/MZFinance.woa/wa/buyProduct"
	PrivateAppStoreAPIPathDownload                = "/WebObjects/MZFinance.woa/wa/volumeStoreDownloadProduct"

	HTTPHeaderStoreFront = "X-Set-Apple-Store-Front"
	HTTPHeaderPod        = "pod"

	PricingParameterAppStore    = "STDQ"
	PricingParameterAppleArcade = "GAME"
)
