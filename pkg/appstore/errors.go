package appstore

import "errors"

var (
	ErrPasswordTokenExpired = errors.New("password token is expired")
	ErrLicenseExists        = errors.New("account already has a license for this app")
	ErrAppPaid              = errors.New("only free apps are supported")
	ErrInvalidCountryCode   = errors.New("invalid country code")
	ErrInvalidDeviceFamily  = errors.New("invalid device family")
	ErrGeneric              = errors.New("failed with an unknown error")
	ErrAppNotFound          = errors.New("failed to find app on the App Store")
	ErrReadMAC              = errors.New("failed to read MAC address")
	ErrReadAccount          = errors.New("failed to read account")
	ErrReadData             = errors.New("failed to read data")
	ErrReadApp              = errors.New("failed to read app")
	ErrCreateRequest        = errors.New("failed to create HTTP request")
	ErrRequest              = errors.New("failed to send request")
	ErrURL                  = errors.New("failed to create URL")
	ErrMarshal              = errors.New("failed to marshal data")
	ErrUnmarshal            = errors.New("failed to unmarshal data")
	ErrKeychainSet          = errors.New("failed to save item in keychain")
	ErrKeychainGet          = errors.New("failed to read item from keychain")
	ErrKeychainRemove       = errors.New("failed to remove item from keychain")
	ErrPurchase             = errors.New("failed to purchase app")
	ErrDownload             = errors.New("failed to download file")
	ErrLicenseRequired      = errors.New("license required")
	ErrAuthCodeRequired     = errors.New("auth code is required")
)
