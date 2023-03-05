package http

type ResponseFormat string

const (
	ResponseFormatJSON ResponseFormat = "json"
	ResponseFormatXML  ResponseFormat = "xml"
	DefaultUserAgent                  = "Configurator/2.15 (Macintosh; OperatingSystem X 11.0.0; 16G29) AppleWebKit/2603.3.8"
)
