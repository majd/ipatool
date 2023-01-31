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

func (a *appstore) Purchase(bundleOrAppID any) error {
	macAddr, err := a.machine.MacAddress()
	if err != nil {
		return errors.Wrap(err, ErrGetMAC.Error())
	}

	guid := strings.ReplaceAll(strings.ToUpper(macAddr), ":", "")
	a.logger.Verbose().Str("mac", macAddr).Str("guid", guid).Send()

	acc, err := a.account()
	if err != nil {
		return errors.Wrap(err, ErrGetAccount.Error())
	}

	var appID int64
	if val, ok := bundleOrAppID.(int64); ok {
		appID = val
	} else {
		countryCode, err := a.countryCodeFromStoreFront(acc.StoreFront)
		if err != nil {
			return errors.Wrap(err, ErrInvalidCountryCode.Error())
		}
		app, err := a.lookup(bundleOrAppID, countryCode)
		if err != nil {
			return errors.Wrap(err, ErrAppLookup.Error())
		}
		if app.Price > 0 {
			return ErrPaidApp
		}
		appID = app.ID
	}

	err = a.purchase(acc, appID, guid, true)
	if err != nil {
		return errors.Wrap(err, ErrPurchase.Error())
	}

	a.logger.Log().Bool("success", true).Send()
	return nil
}

func (a *appstore) purchaseWithParams(acc Account, appID int64, guid string, attemptToRenewCredentials bool, pricingParameters string) error {
	req := a.purchaseRequest(acc, appID, acc.StoreFront, guid, pricingParameters)
	res, err := a.purchaseClient.Send(req)
	if err != nil {
		return errors.Wrap(err, ErrRequest.Error())
	}

	if res.Data.FailureType == FailureTypeTemporarilyUnavailable {
		return ErrTemporarilyUnavailable
	}

	if res.Data.FailureType == FailureTypePasswordTokenExpired {
		if attemptToRenewCredentials {
			a.logger.Verbose().Msg("retrieving new password token")
			acc, err = a.login(acc.Email, acc.Password, "", guid, 0, true)
			if err != nil {
				return errors.Wrap(err, ErrPasswordTokenExpired.Error())
			}

			return a.purchaseWithParams(acc, appID, guid, false, pricingParameters)
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

func (a *appstore) purchase(acc Account, appID int64, guid string, attemptToRenewCredentials bool) error {
	for _, pricingParameters := range []string{"STDQ", "GAME"} {
		if err := a.purchaseWithParams(acc, appID, guid, attemptToRenewCredentials, pricingParameters); err == ErrTemporarilyUnavailable {
			continue
		} else if err != nil {
			return err
		} else {
			return nil
		}
	}
	return ErrTemporarilyUnavailable
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

func (a *appstore) purchaseRequest(acc Account, appID int64, storeFront, guid, pricingParameters string) http.Request {
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
				"origPage":                  fmt.Sprintf("Software-%d", appID),
				"origPageLocation":          "Buy",
				"price":                     "0",
				"pricingParameters":         pricingParameters,
				"productType":               "C",
				"salableAdamId":             appID,
			},
		},
	}
}
