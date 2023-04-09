package appstore

import (
	"fmt"
	"github.com/majd/ipatool/pkg/http"
	"github.com/pkg/errors"
	"strings"
)

type PurchaseResult struct {
	FailureType     string `plist:"failureType,omitempty"`
	CustomerMessage string `plist:"customerMessage,omitempty"`
	JingleDocType   string `plist:"jingleDocType,omitempty"`
	Status          int    `plist:"status,omitempty"`
}

func (a *appstore) Purchase(bundleID string) error {
	macAddr, err := a.machine.MacAddress()
	if err != nil {
		return errors.Wrap(err, ErrGetMAC.Error())
	}

	guid := strings.ReplaceAll(strings.ToUpper(macAddr), ":", "")
	a.logger.Verbose().Str("mac", macAddr).Str("guid", guid).Send()

	err = a.purchase(bundleID, guid, true)
	if err != nil {
		return errors.Wrap(err, ErrPurchase.Error())
	}

	return nil
}

func (a *appstore) purchase(bundleID string, guid string, attemptToRenewCredentials bool) error {
	acc, err := a.account()
	if err != nil {
		return errors.Wrap(err, ErrGetAccount.Error())
	}

	countryCode, err := a.countryCodeFromStoreFront(acc.StoreFront)
	if err != nil {
		return errors.Wrap(err, ErrInvalidCountryCode.Error())
	}

	app, err := a.lookup(bundleID, countryCode)
	if err != nil {
		return errors.Wrap(err, ErrAppLookup.Error())
	}

	if app.Price > 0 {
		return ErrPaidApp
	}

	err = a.purchaseWithParams(acc, app, bundleID, guid, attemptToRenewCredentials, PricingParameterAppStore)
	if err != nil {
		if err == ErrTemporarilyUnavailable {
			err = a.purchaseWithParams(acc, app, bundleID, guid, attemptToRenewCredentials, PricingParameterAppleArcade)
			if err != nil {
				return errors.Wrapf(err, "failed to purchase item with param '%s'", PricingParameterAppleArcade)
			}
		}

		return errors.Wrapf(err, "failed to purchase item with param '%s'", PricingParameterAppStore)
	}

	return nil
}

func (a *appstore) purchaseWithParams(acc Account, app App, bundleID string, guid string, attemptToRenewCredentials bool, pricingParameters string) error {
	req := a.purchaseRequest(acc, app, acc.StoreFront, guid, pricingParameters)
	res, err := a.purchaseClient.Send(req)
	if err != nil {
		return errors.Wrap(err, ErrRequest.Error())
	}

	if res.Data.FailureType == FailureTypeTemporarilyUnavailable {
		return ErrTemporarilyUnavailable
	}

	if res.Data.CustomerMessage == CustomerMessageSubscriptionRequired {
		a.logger.Verbose().Interface("response", res).Send()
		return ErrSubscriptionRequired
	}

	if res.Data.FailureType == FailureTypePasswordTokenExpired {
		if attemptToRenewCredentials {
			a.logger.Verbose().Msg("retrieving new password token")
			acc, err = a.login(acc.Email, acc.Password, "", guid, 0, true)
			if err != nil {
				return errors.Wrap(err, ErrPasswordTokenExpired.Error())
			}

			return a.purchaseWithParams(acc, app, bundleID, guid, false, pricingParameters)
		}

		return ErrPasswordTokenExpired
	}

	if res.Data.FailureType != "" && res.Data.CustomerMessage != "" {
		a.logger.Verbose().Interface("response", res).Send()
		return errors.New(res.Data.CustomerMessage)
	}

	if res.Data.FailureType != "" {
		a.logger.Verbose().Interface("response", res).Send()
		return ErrGeneric
	}

	if res.StatusCode == 500 {
		return ErrLicenseExists
	}

	if res.Data.JingleDocType != "purchaseSuccess" || res.Data.Status != 0 {
		a.logger.Verbose().Interface("response", res).Send()
		return errors.New(ErrPurchase.Error())
	}

	return nil
}

func (*appstore) countryCodeFromStoreFront(storeFront string) (string, error) {
	for key, val := range StoreFronts {
		parts := strings.Split(storeFront, "-")

		if len(parts) >= 1 && parts[0] == val {
			return key, nil
		}
	}

	return "", errors.New(ErrInvalidStoreFront.Error())
}

func (a *appstore) purchaseRequest(acc Account, app App, storeFront, guid string, pricingParameters string) http.Request {
	return http.Request{
		URL:            fmt.Sprintf("https://%s%s", PrivateAppStoreAPIDomain, PrivateAppStoreAPIPathPurchase),
		Method:         http.MethodPOST,
		ResponseFormat: http.ResponseFormatXML,
		Headers: map[string]string{
			"Content-Type":        "application/x-apple-plist",
			"iCloud-DSID":         acc.DirectoryServicesID,
			"X-Dsid":              acc.DirectoryServicesID,
			"X-Apple-Store-Front": storeFront,
			"X-Token":             acc.PasswordToken,
		},
		Payload: &http.XMLPayload{
			Content: map[string]interface{}{
				"appExtVrsId":               "0",
				"hasAskedToFulfillPreorder": "true",
				"buyWithoutAuthorization":   "true",
				"hasDoneAgeCheck":           "true",
				"guid":                      guid,
				"needDiv":                   "0",
				"origPage":                  fmt.Sprintf("Software-%d", app.ID),
				"origPageLocation":          "Buy",
				"price":                     "0",
				"pricingParameters":         pricingParameters,
				"productType":               "C",
				"salableAdamId":             app.ID,
			},
		},
	}
}
