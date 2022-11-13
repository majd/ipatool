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
}

func (a *appstore) Purchase(bundleID, countryCode, deviceFamily string) error {
	acc, err := a.account()
	if err != nil {
		return errors.Wrap(err, ErrorReadAccount.Error())
	}

	storeFront := StoreFronts[countryCode]
	if storeFront == "" {
		return ErrorInvalidCountryCode
	}

	app, err := a.lookup(bundleID, countryCode, deviceFamily)
	if err != nil {
		return errors.Wrap(err, ErrorReadApp.Error())
	}

	if app.Price > 0 {
		return ErrorAppPaid
	}

	macAddr, err := a.machine.MacAddress()
	if err != nil {
		return errors.Wrap(err, ErrorReadMAC.Error())
	}

	guid := strings.ReplaceAll(strings.ToUpper(macAddr), ":", "")
	a.logger.Debug().Str("mac", macAddr).Str("guid", guid).Send()

	req := a.purchaseRequest(acc, app, storeFront, guid)
	res, err := a.purchaseClient.Send(req)
	if err != nil {
		return errors.Wrap(err, ErrorRequest.Error())
	}

	if res.Data.FailureType == FailureTypePasswordTokenExpired {
		return ErrorPasswordTokenExpired
	}

	if res.Data.FailureType != "" && res.Data.CustomerMessage != "" {
		a.logger.Debug().Interface("response", res).Send()
		return errors.New(res.Data.CustomerMessage)
	}

	if res.Data.FailureType != "" {
		a.logger.Debug().Interface("response", res).Send()
		return ErrorGeneric
	}

	if res.StatusCode == 500 {
		return ErrorLicenseExists
	}

	a.logger.Info().Bool("success", true).Send()
	return nil
}

func (a *appstore) purchaseRequest(acc Account, app App, storeFront, guid string) http.Request {
	return http.Request{
		URL:            fmt.Sprintf("https://%s%s", PrivateAppStoreAPIDomain, PrivateAppStoreAPIPathPurchase),
		Method:         http.MethodPOST,
		ResponseFormat: http.ResponseFormatXML,
		Headers: map[string]string{
			"User-Agent":          "Configurator/2.15 (Macintosh; OS X 11.0.0; 16G29) AppleWebKit/2603.3.8",
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
