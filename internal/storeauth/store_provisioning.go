package storeauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/majd/ipatool/v2/internal/sap/assets"
	"github.com/majd/ipatool/v2/internal/sap/machine"
	"howett.net/plist"
)

const storeAnisetteDSID = ^uint64(0)

// ProvisionStoreAnisette completes the Store-specific ADI provisioning action
// returned by auth.itunes.apple.com.
func ProvisionStoreAnisette(ctx context.Context, client *http.Client, encoded []byte, challenge, endpoint, userAgent, kbsync string, hardwareID []byte, identity http.Header) ([]byte, error) {
	if client == nil {
		return nil, errors.New("anisette HTTP client is nil")
	}

	state, err := decodeAnisetteState(encoded)
	if err != nil {
		return nil, err
	}

	spim, err := base64.StdEncoding.DecodeString(strings.TrimSpace(challenge))
	if err != nil || len(spim) == 0 || len(spim) > anisetteLimit {
		return nil, errors.New("decode Apple Store provisioning data")
	}

	bundle, err := assets.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("load local anisette assets: %w", err)
	}

	adi, err := machine.OpenModernADI(ctx, bundle, state.Identifier, nil)
	if err != nil {
		return nil, fmt.Errorf("open local anisette runtime: %w", err)
	}

	defer func() { _ = adi.Close() }()

	cpim, session, err := adi.StartProvisioning(storeAnisetteDSID, spim)
	if err != nil {
		return nil, fmt.Errorf("start Apple Store provisioning: %w", err)
	}

	payload := map[string]any{"clientData": base64.StdEncoding.EncodeToString(cpim)}
	if kbsync != "" {
		payload["kbsync"] = kbsync
	}

	settingValue, transportValue, keyBag, err := finishStoreProvisioning(ctx, client, payload, endpoint, userAgent, identity)
	if err != nil {
		return nil, err
	}

	settingInfo, err := decodeStoreProvisioningValue(settingValue, "settingInfo")
	if err != nil {
		return nil, err
	}

	transportKey, err := decodeStoreProvisioningValue(transportValue, "transportKey")
	if err != nil {
		return nil, err
	}

	if err = adi.EndProvisioning(session, settingInfo, transportKey); err != nil {
		return nil, fmt.Errorf("finish Apple Store provisioning (setting info %d bytes, transport key %d bytes): %w", len(settingInfo), len(transportKey), err)
	}

	if len(keyBag) != 0 {
		if err = adi.ImportKeyBag(hardwareID, keyBag); err != nil {
			return nil, fmt.Errorf("import Apple Store FairPlay keybag: %w", err)
		}
	}

	state.Provisioning, err = adi.State()
	if err != nil {
		return nil, fmt.Errorf("persist Apple Store provisioning: %w", err)
	}

	updated, err := json.Marshal(state)
	if err != nil {
		return nil, errors.New("encode anisette state")
	}

	return updated, nil
}

func finishStoreProvisioning(ctx context.Context, client *http.Client, payload map[string]any, endpoint, userAgent string, identity http.Header) (string, string, []byte, error) {
	body, err := plist.Marshal(payload, plist.XMLFormat)
	if err != nil {
		return "", "", nil, errors.New("encode Apple Store provisioning request")
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", "", nil, fmt.Errorf("create Apple Store provisioning request: %w", err)
	}

	for key, values := range identity {
		if !strings.EqualFold(key, "X-Apple-I-MD") && !strings.EqualFold(key, "X-Apple-I-MD-M") && !strings.EqualFold(key, "X-Apple-I-MD-RINFO") {
			request.Header[key] = append([]string(nil), values...)
		}
	}

	request.Header.Set("Accept", "*/*")
	request.Header.Set("Content-Type", "application/x-apple-plist")
	request.Header.Set("User-Agent", userAgent)

	response, err := client.Do(request)
	if err != nil {
		return "", "", nil, fmt.Errorf("send Apple Store provisioning request: %w", err)
	}

	defer response.Body.Close()

	data, err := io.ReadAll(io.LimitReader(response.Body, anisetteLimit+1))
	if err != nil || len(data) > anisetteLimit {
		return "", "", nil, errors.New("read Apple Store provisioning response")
	}

	if response.StatusCode != http.StatusOK {
		return "", "", nil, fmt.Errorf("apple Store provisioning returned HTTP %d", response.StatusCode)
	}

	var result map[string]any
	if _, err = plist.Unmarshal(data, &result); err != nil {
		return "", "", nil, errors.New("decode Apple Store provisioning response")
	}

	settingInfo, ok := result["settingInfo"].(string)
	if !ok || !headerValue(settingInfo, anisetteLimit) {
		return "", "", nil, errors.New("apple Store provisioning response is missing settingInfo")
	}

	transportKey, ok := result["transportKey"].(string)
	if !ok || !headerValue(transportKey, anisetteLimit) {
		return "", "", nil, errors.New("apple Store provisioning response is missing transportKey")
	}

	keyBag, ok := result["keybag"].([]byte)
	if !ok || len(keyBag) == 0 || len(keyBag) > anisetteLimit {
		if encoded, stringOK := result["keybag"].(string); stringOK {
			keyBag, _ = base64.StdEncoding.DecodeString(encoded)
		}
	}

	if len(keyBag) > anisetteLimit {
		return "", "", nil, errors.New("apple Store provisioning response contains an invalid keybag")
	}

	return settingInfo, transportKey, append([]byte(nil), keyBag...), nil
}

func decodeStoreProvisioningValue(value, key string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(decoded) == 0 || len(decoded) > anisetteLimit {
		return nil, fmt.Errorf("decode Apple Store provisioning %s", key)
	}

	return decoded, nil
}
