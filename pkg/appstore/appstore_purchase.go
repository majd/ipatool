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

func (a *appstore) Purchase(bundleID, deviceFamily string) error {
	macAddr, err := a.machine.MacAddress()
	if err != nil {
		return errors.Wrap(err, ErrReadMAC.Error())
	}

	guid := strings.ReplaceAll(strings.ToUpper(macAddr), ":", "")
	a.logger.Verbose().Str("mac", macAddr).Str("guid", guid).Send()

	err = a.purchase(bundleID, deviceFamily, guid, true)
	if err != nil {
		return errors.Wrap(err, ErrPurchase.Error())
	}

	a.logger.Log().Bool("success", true).Send()
	return nil
}

func (a *appstore) purchase(bundleID, deviceFamily, guid string, attemptToRenewCredentials bool) error {
	acc, err := a.account()
	if err != nil {
		return errors.Wrap(err, ErrReadAccount.Error())
	}

	countryCode, err := a.countryCodeFromStoreFront(acc.StoreFront)
	if err != nil {
		return errors.Wrap(err, ErrInvalidCountryCode.Error())
	}

	app, err := a.lookup(bundleID, countryCode, deviceFamily)
	if err != nil {
		return errors.Wrap(err, ErrReadApp.Error())
	}

	if app.Price > 0 {
		return ErrAppPaid
	}

	req := a.purchaseRequest(acc, app, acc.StoreFront, guid)
	res, err := a.purchaseClient.Send(req)
	if err != nil {
		return errors.Wrap(err, ErrRequest.Error())
	}

	if res.Data.FailureType == FailureTypePasswordTokenExpired {
		if attemptToRenewCredentials {
			a.logger.Verbose().Msg("retrieving new password token")
			acc, err = a.login(acc.Email, acc.Password, "", guid, 0, true)
			if err != nil {
				return errors.Wrap(err, ErrPasswordTokenExpired.Error())
			}

			return a.purchase(bundleID, deviceFamily, guid, false)
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
		return errors.New("failed to acquire license")
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

	return "", errors.New("could not infer country code from store front")
}

func (a *appstore) purchaseRequest(acc Account, app App, storeFront, guid string) http.Request {
	return http.Request{
		URL:            fmt.Sprintf("https://%s%s", PrivateAppStoreAPIDomain, PrivateAppStoreAPIPathPurchase),
		Method:         http.MethodPOST,
		ResponseFormat: http.ResponseFormatXML,
		Headers: map[string]string{
			"User-Agent":          "Configurator/2.15 (Macintosh; OperatingSystem X 11.0.0; 16G29) AppleWebKit/2603.3.8",
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
				"pricingParameters":         "STDQ",
				"productType":               "C",
				"salableAdamId":             app.ID,
			},
		},
	}
}
