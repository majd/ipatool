package appstore

import (
	"errors"
	"fmt"
	gohttp "net/http"
	"net/url"
	"strings"

	"github.com/majd/ipatool/v2/pkg/http"
)

const (
	downloadDispatchDomain        = "downloaddispatch." + iTunesAPIDomain
	redownloadProductPath         = "/r/redownload"
	updateProductPath             = "/up/updateProduct"
	downloadVersionKeyVolumeStore = "externalVersionId"
	downloadVersionKeyRedownload  = "appExtVrsId"
)

type downloadProductEndpoint struct {
	baseURL    string
	versionKey string
}

func (t *appstore) sendDownloadProduct(acc Account, app App, guid, externalVersionID string, platform Platform) (http.Result[downloadResult], Platform, error) {
	// An unpinned request can return the iOS build of a universal app.
	// Select the Mac offer before accepting any download response.
	if externalVersionID == "" && platform == PlatformMacOS {
		var err error

		externalVersionID, err = t.lookupLatestMacOSExternalVersionID(acc, app)
		if err != nil {
			return http.Result[downloadResult]{}, platform, fmt.Errorf("failed to resolve latest macOS version for download: %w", err)
		}
	}

	volumeStore := t.volumeStoreEndpoint(acc)

	res, err := t.downloadClient.Send(t.downloadProductRequest(volumeStore, acc, app, guid, externalVersionID))
	if err != nil {
		return res, platform, fmt.Errorf("failed to send http request: %w", err)
	}

	if !isEmptyDownloadProductResponse(res) && !isUnavailableDownloadProductResponse(res) {
		return res, platform, nil
	}

	// Try redownload when volumeStore returns no items, either silently or
	// with the message-only "No Longer Available" response.
	bag, err := t.fetchURLBag(guid)
	if err != nil {
		return res, platform, fmt.Errorf("failed to get bag for redownload fallback: %w", err)
	}

	if bag.RedownloadEndpoint == "" {
		return res, platform, nil
	}

	redownload, err := newDownloadEndpoint(bag.RedownloadEndpoint, redownloadProductPath)
	if err != nil {
		return res, platform, err
	}

	if externalVersionID == "" && (platform == "" || platform == PlatformIPhone || platform == PlatformIPad) {
		// Unpinned redownloads can fail or return a tvOS package. Select
		// the current iOS build before sending.
		if platform == "" {
			platform = PlatformIPhone
		}

		externalVersionID, err = t.lookupLatestExternalVersionID(acc, app, platform)
		if err != nil {
			return res, platform, fmt.Errorf("failed to resolve latest iOS version for redownload: %w", err)
		}
	}

	redownloadRes, err := t.downloadClient.Send(t.downloadProductRequest(redownload, acc, app, guid, externalVersionID))
	if bag.UpdateEndpoint != "" && externalVersionID != "" &&
		(platform == "" || platform == PlatformIPhone || platform == PlatformIPad || platform == PlatformMacOS) &&
		(isEmptyRedownloadError(err) || (err == nil && isUnavailableDownloadProductResponse(redownloadRes))) {
		updateRes, updateErr := t.sendUpdateProduct(bag.UpdateEndpoint, acc, app, guid, externalVersionID)

		if updateErr == nil && platform == "" {
			platform = PlatformIPhone
		}

		return updateRes, platform, updateErr
	}

	if err != nil {
		return redownloadRes, platform, fmt.Errorf("failed to send redownload request: %w", err)
	}

	return redownloadRes, platform, nil
}

func isEmptyRedownloadError(err error) bool {
	var unexpected *http.UnexpectedResponseError

	return errors.As(err, &unexpected) &&
		unexpected.StatusCode == gohttp.StatusInternalServerError && unexpected.Snippet == ""
}

func (t *appstore) sendUpdateProduct(endpoint string, acc Account, app App, guid, externalVersionID string) (http.Result[downloadResult], error) {
	update, err := newDownloadEndpoint(endpoint, updateProductPath)
	if err != nil {
		return http.Result[downloadResult]{}, err
	}

	// The bag's updateProduct can serve pinned iOS and macOS versions when redownload
	// returns an empty HTTP 500 or a message-only availability error. Keep
	// the same session and version selection.
	res, err := t.downloadClient.Send(t.downloadProductRequest(update, acc, app, guid, externalVersionID))
	if err != nil {
		return res, fmt.Errorf("failed to send update request: %w", err)
	}

	if res.Data.FailureType != "" {
		return res, nil
	}

	if res.Data.CustomerMessage != "" {
		return res, NewErrorWithMetadata(fmt.Errorf("received update error: %s", res.Data.CustomerMessage), res)
	}

	if res.StatusCode != gohttp.StatusOK {
		return res, fmt.Errorf("received unexpected update status code: %d", res.StatusCode)
	}

	if len(res.Data.Items) != 1 {
		return res, errors.New("update response must contain exactly one item")
	}

	metadata := res.Data.Items[0].Metadata
	if fmt.Sprint(metadata["itemId"]) != fmt.Sprint(app.ID) ||
		fmt.Sprint(metadata["softwareVersionExternalIdentifier"]) != externalVersionID {
		return res, errors.New("update response does not match the requested app or version")
	}

	bundleID, ok := metadata["softwareVersionBundleId"].(string)
	if !ok || bundleID == "" || (app.BundleID != "" && bundleID != app.BundleID) {
		return res, errors.New("update response does not match the requested bundle identifier")
	}

	return res, nil
}

func newDownloadEndpoint(endpoint, path string) (downloadProductEndpoint, error) {
	parsed, err := url.ParseRequestURI(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.Host != downloadDispatchDomain ||
		parsed.Path != path || parsed.RawPath != "" || parsed.RawQuery != "" ||
		parsed.ForceQuery || parsed.Fragment != "" || parsed.User != nil {
		return downloadProductEndpoint{}, errors.New("invalid download endpoint in bag")
	}

	return downloadProductEndpoint{
		baseURL:    endpoint,
		versionKey: downloadVersionKeyRedownload,
	}, nil
}

// Limit recovery to the observed availability response; other customer messages
// and structured failures must retain their normal error handling.
func isUnavailableDownloadProductResponse(res http.Result[downloadResult]) bool {
	message := strings.ToLower(strings.TrimSpace(res.Data.CustomerMessage))

	return res.StatusCode == gohttp.StatusOK &&
		res.Data.FailureType == "" && len(res.Data.Items) == 0 &&
		(message == "no longer available" || strings.HasSuffix(message, " no longer available"))
}

func isEmptyDownloadProductResponse(res http.Result[downloadResult]) bool {
	return res.StatusCode == gohttp.StatusOK &&
		res.Data.FailureType == "" &&
		res.Data.CustomerMessage == "" &&
		len(res.Data.Items) == 0
}

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

func (*appstore) downloadProductRequest(endpoint downloadProductEndpoint, acc Account, app App, guid, externalVersionID string) http.Request {
	payload := map[string]interface{}{
		"creditDisplay": "",
		"guid":          guid,
		"salableAdamId": app.ID,
		"serialNumber":  "0",
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
