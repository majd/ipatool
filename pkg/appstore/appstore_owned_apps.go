package appstore

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	gohttp "net/http"
	"sort"
	"strconv"
	"time"

	"github.com/majd/ipatool/v2/pkg/http"
)

const (
	DefaultOwnedAppsLimit = 10
	MaxOwnedAppsLimit     = 100

	ownedAppsMediaKind = 131072
)

type OwnedAppsInput struct {
	Account Account
	Page    int
	Limit   int
}

type OwnedAppsOutput struct {
	Count      int
	TotalCount int
	Page       int
	Results    []App
}

func (t *appstore) OwnedApps(input OwnedAppsInput) (OwnedAppsOutput, error) {
	input, err := normalizeOwnedAppsInput(input)
	if err != nil {
		return OwnedAppsOutput{}, err
	}

	macAddress, err := t.machine.MacAddress()
	if err != nil {
		return OwnedAppsOutput{}, fmt.Errorf("failed to get mac address: %w", err)
	}

	guid, machineID, err := machineIdentity(macAddress)
	if err != nil {
		return OwnedAppsOutput{}, err
	}

	bag, err := t.bag(guid)
	if err != nil {
		return OwnedAppsOutput{}, fmt.Errorf("failed to get bag: %w", err)
	}

	if t.actionSignerFactory == nil {
		return OwnedAppsOutput{}, errors.New("SAP action signer is not configured")
	}

	signer, err := t.actionSignerFactory(bag.SAPConfig, machineID)
	if err != nil {
		return OwnedAppsOutput{}, fmt.Errorf("failed to initialize SAP action signer: %w", err)
	}

	if signer == nil {
		return OwnedAppsOutput{}, errors.New("SAP action signer factory returned nil")
	}

	apps, fetchErr := t.fetchOwnedApps(input.Account, guid, signer)
	apps = ownedAppsSortedByPurchaseDate(apps)
	pageApps := ownedAppsPage(apps, input.Page, input.Limit)
	output := OwnedAppsOutput{
		Count:      len(pageApps),
		TotalCount: len(apps),
		Page:       input.Page,
		Results:    pageApps,
	}
	closeErr := signer.Close()

	if closeErr != nil {
		closeErr = fmt.Errorf("failed to close SAP action signer: %w", closeErr)
	}

	if fetchErr != nil {
		if closeErr != nil {
			return OwnedAppsOutput{}, errors.Join(fetchErr, closeErr)
		}

		return OwnedAppsOutput{}, fetchErr
	}

	if closeErr != nil {
		return output, closeErr
	}

	return output, nil
}

func normalizeOwnedAppsInput(input OwnedAppsInput) (OwnedAppsInput, error) {
	if input.Page == 0 {
		input.Page = 1
	}

	if input.Limit == 0 {
		input.Limit = DefaultOwnedAppsLimit
	}

	if input.Page < 1 {
		return OwnedAppsInput{}, errors.New("page must be greater than 0")
	}

	if input.Limit < 1 {
		return OwnedAppsInput{}, errors.New("limit must be greater than 0")
	}

	if input.Limit > MaxOwnedAppsLimit {
		return OwnedAppsInput{}, fmt.Errorf("limit must not exceed %d", MaxOwnedAppsLimit)
	}

	return input, nil
}

func (t *appstore) fetchOwnedApps(acc Account, guid string, signer ActionSigner) ([]App, error) {
	loginResult, err := t.ownedAppsClient.Send(t.ownedAppsLoginRequest(acc, guid))
	if err != nil {
		return nil, fmt.Errorf("failed to open purchase history session: %w", err)
	}

	if err := ownedAppsResponseError("purchase history login", loginResult); err != nil {
		return nil, err
	}

	if err := ownedAppsDMAPStatusError("purchase history login", loginResult.Data); err != nil {
		return nil, err
	}

	sessionID, ok, err := firstDMAPUint(loginResult.Data, "mlid")
	if err != nil {
		return nil, fmt.Errorf("failed to parse purchase history login response: %w", err)
	}

	if !ok || sessionID > math.MaxUint32 {
		return nil, errors.New("purchase history login response did not contain a valid session ID")
	}

	query := fmt.Sprintf("('com.apple.itunes.extended\\-media\\-kind:%d')", ownedAppsMediaKind)

	updateResult, err := t.ownedAppsClient.Send(t.ownedAppsUpdateRequest(acc, guid, uint32(sessionID), query, signer))
	if err != nil {
		return nil, fmt.Errorf("failed to update purchase history: %w", err)
	}

	if err := ownedAppsResponseError("purchase history update", updateResult); err != nil {
		return nil, err
	}

	if err := ownedAppsDMAPStatusError("purchase history update", updateResult.Data); err != nil {
		return nil, err
	}

	latestVersion, ok, err := firstDMAPUint(updateResult.Data, "musr")
	if err != nil {
		return nil, fmt.Errorf("failed to parse purchase history update response: %w", err)
	}

	if !ok || latestVersion > math.MaxUint32 {
		return nil, errors.New("purchase history update response did not contain a valid revision")
	}

	itemsResult, err := t.ownedAppsClient.Send(t.ownedAppsItemsRequest(
		acc,
		guid,
		uint32(sessionID),
		uint32(latestVersion),
		query,
		time.Now(),
		signer,
	))
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve purchase history items: %w", err)
	}

	if err := ownedAppsResponseError("purchase history items", itemsResult); err != nil {
		return nil, err
	}

	if err := ownedAppsDMAPStatusError("purchase history items", itemsResult.Data); err != nil {
		return nil, err
	}

	apps, err := parseOwnedApps(itemsResult.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse purchase history items response: %w", err)
	}

	return apps, nil
}

func (t *appstore) ownedAppsLoginRequest(acc Account, guid string) http.Request {
	return http.Request{
		URL:            PrivatePurchaseDAAPBaseURL + "/login",
		Method:         http.MethodPOST,
		ResponseFormat: http.ResponseFormatRaw,
		Headers:        ownedAppsHeaders(acc, guid, time.Now()),
	}
}

func (t *appstore) ownedAppsUpdateRequest(acc Account, guid string, sessionID uint32, query string, signer ActionSigner) http.Request {
	body := []byte(fmt.Sprintf("session-id=%d&revision-number=(null)&query=%s", sessionID, query))
	headers := ownedAppsHeaders(acc, guid, time.Now())
	headers["Content-Type"] = "application/x-www-form-urlencoded"

	return http.Request{
		URL:            PrivatePurchaseDAAPBaseURL + "/update",
		Method:         http.MethodPOST,
		ResponseFormat: http.ResponseFormatRaw,
		ActionSigner:   signer,
		Headers:        headers,
		Payload:        &http.RawPayload{Content: body},
	}
}

func (t *appstore) ownedAppsItemsRequest(acc Account, guid string, sessionID, latestVersion uint32, query string, now time.Time, signer ActionSigner) http.Request {
	headers := ownedAppsHeaders(acc, guid, now)
	headers["Content-Type"] = "application/x-dmap-tagged"

	return http.Request{
		URL:            fmt.Sprintf("%s/databases/%d/items", PrivatePurchaseDAAPBaseURL, latestVersion),
		Method:         http.MethodPOST,
		ResponseFormat: http.ResponseFormatRaw,
		ActionSigner:   signer,
		Headers:        headers,
		Payload: &http.RawPayload{Content: ownedAppsItemsBody(
			sessionID,
			latestVersion,
			query,
			now,
		)},
	}
}

func ownedAppsHeaders(acc Account, guid string, now time.Time) map[string]string {
	utc := now.UTC()
	_, offsetSeconds := now.Zone()

	return map[string]string{
		"Accept":                             "*/*",
		"Accept-Language":                    "en-us",
		"Client-Cloud-DAAP-Request-Reason":   "5",
		"Client-Cloud-Purchase-Daap-Version": "1.1/Configurator-2.0",
		"Client-DAAP-Version":                "3.12",
		"Date":                               utc.Format(gohttp.TimeFormat),
		"iCloud-DSID":                        acc.DirectoryServicesID,
		"X-Apple-I-Client-Time":              utc.Format("2006-01-02T15:04:05Z"),
		"X-Apple-I-Locale":                   "en_US",
		"X-Apple-I-TimeZone":                 now.Location().String(),
		"X-Apple-Store-Front":                acc.StoreFront,
		"X-Apple-TZ":                         strconv.Itoa(offsetSeconds / 60),
		"X-Dsid":                             acc.DirectoryServicesID,
		"X-Guid":                             guid,
		"X-Token":                            acc.PasswordToken,
	}
}

func ownedAppsItemsBody(sessionID, latestVersion uint32, query string, now time.Time) []byte {
	payload := make([]byte, 0, 73+len(query))
	payload = append(payload, dmapUint32("mstc", uint32(now.Unix()))...)
	payload = append(payload, dmapUint32("mlid", sessionID)...)
	payload = append(payload, dmapUint8("mikd", 2)...)
	payload = append(payload, dmapUint32("musr", latestVersion)...)
	payload = append(payload, dmapUint32("mder", 0)...)
	payload = append(payload, dmapString("mque", query)...)
	payload = append(payload, dmapTag("aetl", nil)...)

	return dmapTag("adsr", payload)
}

func dmapTag(name string, payload []byte) []byte {
	result := make([]byte, 8+len(payload))
	copy(result[:4], name)
	binary.BigEndian.PutUint32(result[4:8], uint32(len(payload)))
	copy(result[8:], payload)

	return result
}

func dmapUint8(name string, value uint8) []byte {
	return dmapTag(name, []byte{value})
}

func dmapUint32(name string, value uint32) []byte {
	payload := make([]byte, 4)
	binary.BigEndian.PutUint32(payload, value)

	return dmapTag(name, payload)
}

func dmapString(name, value string) []byte {
	return dmapTag(name, []byte(value))
}

func ownedAppsResponseError(label string, result http.Result[[]byte]) error {
	if result.StatusCode == gohttp.StatusUnauthorized || result.StatusCode == gohttp.StatusForbidden {
		return fmt.Errorf("%w: %s request returned HTTP %d", ErrPasswordTokenExpired, label, result.StatusCode)
	}

	if result.StatusCode != gohttp.StatusOK {
		return fmt.Errorf("%s request returned HTTP %d", label, result.StatusCode)
	}

	return nil
}

func ownedAppsDMAPStatusError(label string, data []byte) error {
	status, found, err := firstDMAPUint(data, "mstt")
	if err != nil {
		return fmt.Errorf("failed to parse %s response: %w", label, err)
	}

	if found && (status == gohttp.StatusUnauthorized || status == gohttp.StatusForbidden) {
		return fmt.Errorf("%w: %s response returned DAAP status %d", ErrPasswordTokenExpired, label, status)
	}

	if found && status != gohttp.StatusOK {
		return fmt.Errorf("%s response returned DAAP status %d", label, status)
	}

	return nil
}

func firstDMAPUint(data []byte, target string) (uint64, bool, error) {
	var (
		result uint64
		found  bool
	)

	err := walkDMAP(data, 0, func(tag string, payload []byte) error {
		if found || tag != target {
			return nil
		}

		switch len(payload) {
		case 4:
			result = uint64(binary.BigEndian.Uint32(payload))
		case 8:
			result = binary.BigEndian.Uint64(payload)
		default:
			return fmt.Errorf("tag %s has invalid integer length %d", target, len(payload))
		}

		found = true

		return nil
	})

	return result, found, err
}

func parseOwnedApps(data []byte) ([]App, error) {
	apps := make([]App, 0)
	seen := make(map[int64]struct{})

	err := walkDMAP(data, 0, func(tag string, payload []byte) error {
		if tag != "mlit" {
			return nil
		}

		app, err := parseOwnedApp(payload)
		if err != nil {
			return err
		}

		if app.ID == 0 {
			return nil
		}

		if _, ok := seen[app.ID]; ok {
			return nil
		}

		seen[app.ID] = struct{}{}

		apps = append(apps, app)

		return nil
	})
	if err != nil {
		return nil, err
	}

	return apps, nil
}

func parseOwnedApp(data []byte) (App, error) {
	var app App

	err := walkDMAP(data, 0, func(tag string, payload []byte) error {
		switch tag {
		case "aeSI":
			id, err := dmapInt64(payload)
			if err != nil {
				return fmt.Errorf("failed to parse owned app ID: %w", err)
			}

			app.ID = id
		case "aeBI":
			app.BundleID = string(payload)
		case "aeLN":
			app.Name = string(payload)
		case "minm":
			if app.Name == "" {
				app.Name = string(payload)
			}
		case "aePd":
			app.Version = string(payload)
		case "asdp":
			if len(payload) != 4 {
				return fmt.Errorf("owned app purchase date has invalid length %d", len(payload))
			}

			app.PurchaseDate = time.Unix(int64(binary.BigEndian.Uint32(payload)), 0).UTC()
		}

		return nil
	})

	return app, err
}

func ownedAppsSortedByPurchaseDate(apps []App) []App {
	sorted := append([]App(nil), apps...)
	sort.SliceStable(sorted, func(left, right int) bool {
		leftDate, rightDate := sorted[left].PurchaseDate, sorted[right].PurchaseDate
		if leftDate.IsZero() {
			return false
		}

		if rightDate.IsZero() {
			return true
		}

		return leftDate.After(rightDate)
	})

	return sorted
}

func dmapInt64(payload []byte) (int64, error) {
	switch len(payload) {
	case 4:
		return int64(binary.BigEndian.Uint32(payload)), nil
	case 8:
		value := binary.BigEndian.Uint64(payload)
		if value > math.MaxInt64 {
			return 0, errors.New("integer exceeds int64")
		}

		return int64(value), nil
	default:
		return 0, fmt.Errorf("invalid integer length %d", len(payload))
	}
}

func walkDMAP(data []byte, depth int, visit func(tag string, payload []byte) error) error {
	if depth > 16 {
		return errors.New("DMAP nesting is too deep")
	}

	if len(data) == 0 {
		return nil
	}

	for offset := 0; offset < len(data); {
		if len(data)-offset < 8 {
			return fmt.Errorf("truncated DMAP tag header at byte %d", offset)
		}

		tagBytes := data[offset : offset+4]
		if !validDMAPTag(tagBytes) {
			return fmt.Errorf("invalid DMAP tag at byte %d", offset)
		}

		length, remaining := uint64(binary.BigEndian.Uint32(data[offset+4:offset+8])), uint64(len(data)-offset-8)
		if length > remaining {
			return fmt.Errorf("DMAP tag %s length %d exceeds remaining response length %d", string(tagBytes), length, remaining)
		}

		end := offset + 8 + int(length)

		tag, payload := string(tagBytes), data[offset+8:end]
		if err := visit(tag, payload); err != nil {
			return err
		}

		if isDMAPContainer(tag) {
			if err := walkDMAP(payload, depth+1, visit); err != nil {
				return err
			}
		}

		offset = end
	}

	return nil
}

func validDMAPTag(tag []byte) bool {
	if len(tag) != 4 {
		return false
	}

	for _, character := range tag {
		if character < 0x20 || character > 0x7e {
			return false
		}
	}

	return true
}

func isDMAPContainer(tag string) bool {
	switch tag {
	case "adbs", "adsr", "aply", "avdb", "mbcl", "mccr", "mcty", "mdcl", "mlcl", "mlit", "mlog", "msrv", "mupd":
		return true
	default:
		return false
	}
}

func ownedAppsPage(apps []App, page, limit int) []App {
	if page < 1 || limit < 1 {
		return []App{}
	}

	pageIndex, pageSize := uint64(page-1), uint64(limit)
	if pageIndex > uint64(len(apps))/pageSize {
		return []App{}
	}

	start := pageIndex * pageSize
	if start >= uint64(len(apps)) {
		return []App{}
	}

	end := start + pageSize
	if end > uint64(len(apps)) {
		end = uint64(len(apps))
	}

	return append([]App(nil), apps[int(start):int(end)]...)
}
