package appstore

import "errors"

var (
	ErrorGeneric              = errors.New("unknown error occurred")
	ErrorPasswordTokenExpired = errors.New("password token is expired")
	ErrorLicenseExists        = errors.New("account already has a license for this app")
	ErrorAppPaid              = errors.New("only free apps are supported")
	ErrorAppNotFound          = errors.New("could not find the app on the App Store")
	ErrorReadMAC              = errors.New("failed to read MAC address")
	ErrorReadAccount          = errors.New("failed to read account")
	ErrorReadData             = errors.New("failed to read data")
	ErrorReadApp              = errors.New("failed to read app")
	ErrorCreateRequest        = errors.New("failed to create HTTP request")
	ErrorRequest              = errors.New("request failed")
	ErrorURL                  = errors.New("failed to create URL")
	ErrorInvalidCountryCode   = errors.New("invalid country code")
	ErrorInvalidDeviceFamily  = errors.New("invalid device family")
	ErrorMarshal              = errors.New("failed to marshal data")
	ErrorUnmarshal            = errors.New("failed to unmarshal data")
	ErrorKeychainSet          = errors.New("failed to save item in keychain")
	ErrorKeychainGet          = errors.New("failed to read item from keychain")
	ErrorKeychainRemove       = errors.New("failed to remove item from keychain")
)
