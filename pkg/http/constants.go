package http

type ResponseFormat string

const (
	ResponseFormatJSON ResponseFormat = "json"
	ResponseFormatXML  ResponseFormat = "xml"
)

const (
	// Configurator UA mints a commerce-grade password token (the "Aw..." token)
	// that buyProduct accepts. The appstored UA only yields a download-grade
	// token that buyProduct rejects with failureType 2034. Requires the auth
	// endpoint to carry the trailing slash (see appstore_bag.go).
	DefaultUserAgent = "Configurator/2.17 (Macintosh; OS X 15.2; 24C5089c) AppleWebKit/0620.1.16.11.6"
)
