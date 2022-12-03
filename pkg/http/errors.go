package http

import "errors"

var (
	ErrCreateRequest          = errors.New("failed to create HTTP request")
	ErrEncodePayloadXML       = errors.New("failed to encode XML object")
	ErrGetPayloadData         = errors.New("failed to get payload data")
	ErrGetResponseBody        = errors.New("failed to get response body")
	ErrRequest                = errors.New("failed to send request")
	ErrSaveCookie             = errors.New("failed to save cookies")
	ErrUnmarshalJSON          = errors.New("failed to unmarshal JSON data")
	ErrUnmarshalXML           = errors.New("failed to unmarshal XML data")
	ErrUnsupportedContentType = errors.New("unsupported response body content type")
	ErrUnsupportedValueType   = errors.New("unsupported value type")
)
