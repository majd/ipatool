package appstore

import (
	"errors"
	"fmt"
	gohttp "net/http"
	"strings"

	"github.com/majd/ipatool/v2/pkg/http"
)

var (
	ErrPasswordTokenExpired   = errors.New("password token is expired")
	ErrSubscriptionRequired   = errors.New("subscription required")
	ErrTemporarilyUnavailable = errors.New("item is temporarily unavailable")
)

type PurchaseInput struct {
	Account Account
	App     App
}

func (t *appstore) Purchase(input PurchaseInput) error {
	macAddr, err := t.machine.MacAddress()
	if err != nil {
		return fmt.Errorf("failed to get mac address: %w", err)
	}

	guid := strings.ReplaceAll(strings.ToUpper(macAddr), ":", "")

	if input.App.Price > 0 {
		return errors.New("purchasing paid apps is not supported")
	}

	err = t.purchaseWithParams(input.Account, input.App, guid, PricingParameterAppStore)
	if err != nil {
		if err == ErrTemporarilyUnavailable {
			err = t.purchaseWithParams(input.Account, input.App, guid, PricingParameterAppleArcade)
			if err != nil {
				return fmt.Errorf("failed to purchase item with param '%s': %w", PricingParameterAppleArcade, err)
			}

			return nil
		}

		return fmt.Errorf("failed to purchase item with param '%s': %w", PricingParameterAppStore, err)
	}

	return nil
}

type purchaseResult struct {
	FailureType     string `plist:"failureType,omitempty"`
	CustomerMessage string `plist:"customerMessage,omitempty"`
	JingleDocType   string `plist:"jingleDocType,omitempty"`
	Status          int    `plist:"status,omitempty"`
}

func (t *appstore) purchaseWithParams(acc Account, app App, guid string, pricingParameters string) error {
	req := t.purchaseRequest(acc, app, acc.StoreFront, guid, pricingParameters)
	res, err := t.purchaseClient.Send(req)

	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}

	if res.Data.FailureType == FailureTypeTemporarilyUnavailable {
		return ErrTemporarilyUnavailable
	}

	if res.Data.CustomerMessage == CustomerMessageSubscriptionRequired {
		return ErrSubscriptionRequired
	}

	if res.Data.FailureType == FailureTypePasswordTokenExpired {
		return ErrPasswordTokenExpired
	}

	if res.Data.FailureType != "" && res.Data.CustomerMessage != "" {
		return NewErrorWithMetadata(errors.New(res.Data.CustomerMessage), res)
	}

	if res.Data.FailureType != "" {
		return NewErrorWithMetadata(errors.New("something went wrong"), res)
	}

	if res.StatusCode == gohttp.StatusInternalServerError {
		return fmt.Errorf("license already exists")
	}

	if res.Data.JingleDocType != "purchaseSuccess" || res.Data.Status != 0 {
		return NewErrorWithMetadata(errors.New("failed to purchase app"), res)
	}

	return nil
}

func (t *appstore) purchaseRequest(acc Account, app App, storeFront, guid string, pricingParameters string) http.Request {
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
