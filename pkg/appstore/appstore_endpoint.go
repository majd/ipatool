package appstore

import (
	"errors"
	"fmt"

	"github.com/majd/ipatool/v2/pkg/http"
)

// The App Store exposes two interchangeable download endpoints that, for a
// consumer Apple ID, serve disjoint sets of apps (issue #464):
//
//   - volumeStore (buy.itunes.apple.com/.../volumeStoreDownloadProduct) serves
//     apps the account holds no consumer license for. For apps it does hold a
//     license for it returns failureType 5002 (FailureTypeLicenseAlreadyExists).
//   - redownload (advertised by the bag as redownloadProduct) serves the
//     licensed apps. For everything else it returns an empty failureType with a
//     "<App> No Longer Available" customerMessage.
//
// volumeStore also returns 5002 *intermittently* for apps it can otherwise serve
// (observed ~1 in 8 calls, typically the first), so a 5002 is not on its own
// proof that an app is licensed. The redownload response disambiguates: if
// redownload serves the app it was genuinely licensed; if redownload cannot
// serve it either, the 5002 was transient and volumeStore is retried.
//
// The two endpoints also disagree on the key used to pin an external version:
// volumeStore reads externalVersionId, redownload reads appExtVrsId. The unused
// key is silently ignored, so the request must carry the right one.
const (
	downloadVersionKeyVolumeStore = "externalVersionId"
	downloadVersionKeyRedownload  = "appExtVrsId"

	// volumeStoreRetriesAfterFallback is how many extra times volumeStore is
	// tried when it returned 5002 but redownload also could not serve the app —
	// i.e. when the 5002 was a transient hiccup rather than a real license.
	volumeStoreRetriesAfterFallback = 2
)

// downloadProductEndpoint identifies a download endpoint and the version-pin key
// it expects.
type downloadProductEndpoint struct {
	baseURL    string
	versionKey string
}

// volumeStoreEndpoint is the primary endpoint. It is not resolved from the bag:
// the bag advertises a volumeStoreDownloadProduct URL, but that host speaks the
// enterprise/VPP protocol and rejects ipatool's request shape, so the long-lived
// MZFinance URL is used directly.
func (*appstore) volumeStoreEndpoint(acc Account) downloadProductEndpoint {
	podPrefix := ""
	if acc.Pod != "" {
		podPrefix = "p" + acc.Pod + "-"
	}

	return downloadProductEndpoint{
		baseURL:    fmt.Sprintf("https://%s%s%s", podPrefix, PrivateAppStoreAPIDomain, PrivateAppStoreAPIPathDownload),
		versionKey: downloadVersionKeyVolumeStore,
	}
}

// redownloadEndpoint is the fallback endpoint, resolved from the bag.
func redownloadEndpoint(bagURL string) downloadProductEndpoint {
	return downloadProductEndpoint{
		baseURL:    bagURL,
		versionKey: downloadVersionKeyRedownload,
	}
}

func (*appstore) downloadProductRequest(endpoint downloadProductEndpoint, acc Account, app App, guid, externalVersionID string) http.Request {
	payload := map[string]interface{}{
		"creditDisplay": "",
		"guid":          guid,
		"salableAdamId": app.ID,
	}

	if externalVersionID != "" {
		payload[endpoint.versionKey] = externalVersionID
	}

	return http.Request{
		URL:            fmt.Sprintf("%s?guid=%s", endpoint.baseURL, guid),
		Method:         http.MethodPOST,
		ResponseFormat: http.ResponseFormatXML,
		Headers: map[string]string{
			"Content-Type": "application/x-apple-plist",
			"iCloud-DSID":  acc.DirectoryServicesID,
			"X-Dsid":       acc.DirectoryServicesID,
		},
		Payload: &http.XMLPayload{
			Content: payload,
		},
	}
}

// sendDownloadProduct sends a download-family request to the primary volumeStore
// endpoint, resolving issue #464 with a fallback to the bag-advertised redownload
// endpoint. redownloadBagURL is the bag's redownloadProduct URL; when it is empty
// (a bag that does not advertise the key) only the primary endpoint is used.
//
// On a volumeStore 5002 it tries redownload. If redownload serves the app, that
// response is used (the app was genuinely licensed). If redownload cannot serve
// it either — an empty failureType with no items, the "No Longer Available"
// signature — the 5002 was transient, so volumeStore is retried.
func (t *appstore) sendDownloadProduct(acc Account, app App, guid, externalVersionID, redownloadBagURL string) (http.Result[downloadResult], error) {
	volumeStore := t.volumeStoreEndpoint(acc)

	res, err := t.downloadClient.Send(t.downloadProductRequest(volumeStore, acc, app, guid, externalVersionID))
	if err != nil {
		return res, fmt.Errorf("failed to send http request: %w", err)
	}

	if res.Data.FailureType != FailureTypeLicenseAlreadyExists || redownloadBagURL == "" {
		return res, nil
	}

	redownloadRes, err := t.downloadClient.Send(t.downloadProductRequest(redownloadEndpoint(redownloadBagURL), acc, app, guid, externalVersionID))
	if err != nil {
		return redownloadRes, fmt.Errorf("failed to send http request: %w", err)
	}

	// Redownload served the app (genuinely licensed, e.g. Microsoft Teams) or
	// returned an actionable failure (auth/license) — use that response.
	if len(redownloadRes.Data.Items) > 0 || redownloadRes.Data.FailureType != "" {
		return redownloadRes, nil
	}

	// Redownload cannot serve the app, so the volumeStore 5002 was a transient
	// hiccup rather than a real license. Retry volumeStore.
	for i := 0; i < volumeStoreRetriesAfterFallback; i++ {
		res, err = t.downloadClient.Send(t.downloadProductRequest(volumeStore, acc, app, guid, externalVersionID))
		if err != nil {
			return res, fmt.Errorf("failed to send http request: %w", err)
		}

		if res.Data.FailureType != FailureTypeLicenseAlreadyExists {
			return res, nil
		}
	}

	// volumeStore kept returning 5002 and redownload cannot serve the app; the
	// redownload response carries the most informative message for the caller.
	return redownloadRes, nil
}

// downloadProductItem interprets a download-family response, returning the first
// song-list item or a typed error. It is shared by Download, ListVersions and
// GetVersionMetadata, which all POST the same payload and decode the same shape.
func downloadProductItem(res http.Result[downloadResult]) (downloadItemResult, error) {
	switch {
	case res.Data.FailureType == FailureTypePasswordTokenExpired ||
		res.Data.FailureType == FailureTypeSignInRequired ||
		res.Data.FailureType == FailureTypeDeviceVerificationFailed:
		return downloadItemResult{}, ErrPasswordTokenExpired
	case res.Data.FailureType == FailureTypeLicenseNotFound:
		return downloadItemResult{}, ErrLicenseRequired
	case res.Data.FailureType == FailureTypeLicenseAlreadyExists:
		// 5002 means the account already holds a consumer license, so the app
		// can only be fetched from the redownload endpoint. Reaching here means
		// the fallback was unavailable — the bag advertised no redownloadProduct
		// URL. Surface that plainly instead of the endpoint's opaque "An unknown
		// error has occurred" customerMessage.
		return downloadItemResult{}, NewErrorWithMetadata(errors.New("the App Store requires the redownload endpoint for this app (failureType 5002), but the bag did not advertise one"), res)
	case res.Data.FailureType != "" && res.Data.CustomerMessage != "":
		return downloadItemResult{}, NewErrorWithMetadata(fmt.Errorf("received error: %s", res.Data.CustomerMessage), res)
	case res.Data.FailureType != "":
		return downloadItemResult{}, NewErrorWithMetadata(fmt.Errorf("received error: %s", res.Data.FailureType), res)
	case len(res.Data.Items) == 0:
		// The redownload endpoint reports apps it cannot serve with an empty
		// failureType and a "<App> No Longer Available" customerMessage; surface
		// that rather than a generic "invalid response".
		if res.Data.CustomerMessage != "" {
			return downloadItemResult{}, NewErrorWithMetadata(fmt.Errorf("received error: %s", res.Data.CustomerMessage), res)
		}

		return downloadItemResult{}, NewErrorWithMetadata(errors.New("invalid response"), res)
	default:
		return res.Data.Items[0], nil
	}
}
