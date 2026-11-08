//nolint:wsl // Provisioning steps follow Apple wire order.
package storeauth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/majd/ipatool/v2/internal/sap/assets"
	"github.com/majd/ipatool/v2/internal/sap/machine"
	"howett.net/plist"
)

const (
	anisetteLimit      = 1 << 20
	machineDSID        = ^uint64(1)
	machineStartURL    = "https://gsa.apple.com/grandslam/MidService/startMachineProvisioning"
	machineFinishURL   = "https://gsa.apple.com/grandslam/MidService/finishMachineProvisioning"
	anisetteClientInfo = "<MacBookPro13,3> <Mac OS X;10.12.6;16G29> <com.apple.AuthKit/1 (com.apple.akd/1.0)>"
	anisetteUserAgent  = "akd/1.0 CFNetwork/811.5.4 Darwin/16.7.0"
)

type anisetteState struct {
	Identifier, Provisioning []byte
	RoutingInfo              uint64
}

type machineProvisioningEnvelope struct {
	Response map[string]any `plist:"Response"`
}

// AnisetteHeaders provisions a local software identity when necessary and
// returns a fresh machine-validation proof.
func AnisetteHeaders(ctx context.Context, client *http.Client, encoded []byte) (http.Header, []byte, error) {
	if ctx == nil {
		return nil, nil, errors.New("anisette context is nil")
	}
	if client == nil {
		return nil, nil, errors.New("anisette HTTP client is nil")
	}

	state, err := decodeAnisetteState(encoded)
	if err != nil {
		return nil, nil, err
	}

	// Store provisioning replaces the ADI database. If its proof cannot be
	// reused, start general machine provisioning from the persisted identity.
	state.Provisioning = nil
	state.RoutingInfo = 0

	bundle, err := assets.Load(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("load anisette assets: %w", err)
	}

	adi, err := machine.OpenModernADI(ctx, bundle, state.Identifier, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("open anisette runtime: %w", err)
	}
	defer func() { _ = adi.Close() }()

	if err = provisionMachine(ctx, client, state, adi); err != nil {
		return nil, nil, err
	}

	mid, otp, err := adi.RequestOTP(machineDSID)
	if err != nil {
		return nil, nil, fmt.Errorf("generate anisette proof: %w", err)
	}

	state.Provisioning, err = adi.State()
	if err != nil {
		return nil, nil, fmt.Errorf("read anisette state: %w", err)
	}

	updated, err := json.Marshal(state)
	if err != nil {
		return nil, nil, errors.New("encode anisette state")
	}

	return anisetteHeaders(state, otp, mid), updated, nil
}

func provisionMachine(ctx context.Context, client *http.Client, state *anisetteState, adi *machine.ModernADI) error {
	start, err := sendMachineProvisioning(ctx, client, machineStartURL, state.Identifier, map[string]any{})
	if err != nil {
		return err
	}

	spim, err := decodeProvisioningField(start, "spim")
	if err != nil {
		return err
	}

	cpim, session, err := adi.StartProvisioning(machineDSID, spim)
	if err != nil {
		return fmt.Errorf("start anisette provisioning: %w", err)
	}

	finish, err := sendMachineProvisioning(ctx, client, machineFinishURL, state.Identifier, map[string]any{
		"cpim": base64.StdEncoding.EncodeToString(cpim),
	})
	if err != nil {
		return err
	}

	ptm, err := decodeProvisioningField(finish, "ptm")
	if err != nil {
		return err
	}

	trustKey, err := decodeProvisioningField(finish, "tk")
	if err != nil {
		return err
	}

	routing, err := provisioningString(finish, "X-Apple-I-MD-RINFO")
	if err != nil {
		return err
	}

	state.RoutingInfo, err = strconv.ParseUint(routing, 10, 64)
	if err != nil || state.RoutingInfo == 0 {
		return errors.New("decode anisette routing information")
	}

	if err = adi.EndProvisioning(session, ptm, trustKey); err != nil {
		return fmt.Errorf("finish anisette provisioning: %w", err)
	}

	return nil
}

func sendMachineProvisioning(ctx context.Context, client *http.Client, endpoint string, identifier []byte, values map[string]any) (map[string]any, error) {
	body, err := plist.Marshal(map[string]any{
		"Header":  map[string]any{},
		"Request": values,
	}, plist.XMLFormat)
	if err != nil {
		return nil, errors.New("encode anisette provisioning request")
	}

	localUserID, deviceID := anisetteIdentityHeaders(identifier)
	send := func() (*http.Response, error) {
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if requestErr != nil {
			return nil, fmt.Errorf("create anisette provisioning request: %w", requestErr)
		}
		request.Header.Set("Accept", "*/*")
		request.Header.Set("Connection", "close")
		request.Header.Set("Content-Type", "text/x-xml-plist")
		request.Header.Set("User-Agent", anisetteUserAgent)
		request.Header.Set("X-Apple-I-MD-LU", localUserID)
		request.Header.Set("X-MMe-Client-Info", anisetteClientInfo)
		request.Header.Set("X-Mme-Device-Id", deviceID)

		return client.Do(request)
	}

	response, err := send()
	if isUnknownAuthority(err) {
		if rootErr := installAppleRoot(ctx, client); rootErr != nil {
			return nil, fmt.Errorf("install Apple provisioning root: %w", rootErr)
		}

		response, err = send()
	}
	if err != nil {
		return nil, fmt.Errorf("send anisette provisioning request: %w", err)
	}
	defer response.Body.Close()

	data, err := io.ReadAll(io.LimitReader(response.Body, anisetteLimit+1))
	if err != nil {
		return nil, fmt.Errorf("read anisette provisioning response: %w", err)
	}
	if response.StatusCode != http.StatusOK || len(data) > anisetteLimit {
		return nil, fmt.Errorf("anisette provisioning returned HTTP %d", response.StatusCode)
	}

	var envelope machineProvisioningEnvelope
	if _, err = plist.Unmarshal(data, &envelope); err != nil || envelope.Response == nil {
		return nil, errors.New("decode anisette provisioning response")
	}

	return envelope.Response, nil
}

func decodeAnisetteState(encoded []byte) (*anisetteState, error) {
	if len(encoded) == 0 {
		identifier := make([]byte, 16)
		if _, err := rand.Read(identifier); err != nil {
			return nil, fmt.Errorf("generate anisette identity: %w", err)
		}

		return &anisetteState{Identifier: identifier}, nil
	}

	var state anisetteState
	if json.Unmarshal(encoded, &state) != nil || len(state.Identifier) != 16 || len(state.Provisioning) > anisetteLimit {
		return nil, errors.New("invalid persisted anisette state")
	}

	return &state, nil
}

func anisetteHeaders(state *anisetteState, otp, mid []byte) http.Header {
	localUserID, deviceID := anisetteIdentityHeaders(state.Identifier)
	now := time.Now()
	headers := make(http.Header)
	headers.Set("X-Apple-I-Client-Time", now.UTC().Format("2006-01-02T15:04:05Z"))
	headers.Set("X-Apple-I-MD", base64.StdEncoding.EncodeToString(otp))
	headers.Set("X-Apple-I-MD-M", base64.StdEncoding.EncodeToString(mid))
	headers.Set("X-Apple-I-MD-RINFO", strconv.FormatUint(state.RoutingInfo, 10))
	headers.Set("X-Apple-I-MD-LU", localUserID)
	headers.Set("X-Apple-I-SRL-NO", "0")
	headers.Set("X-Apple-I-TimeZone", now.Location().String())
	headers.Set("X-Apple-Locale", "en_US")
	headers.Set("X-Mme-Device-Id", deviceID)
	headers.Set("X-MMe-Client-Info", anisetteClientInfo)

	return headers
}

func anisetteIdentityHeaders(identifier []byte) (string, string) {
	digest := sha256.Sum256(identifier)
	encoded := hex.EncodeToString(identifier)
	deviceID := encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]

	return hex.EncodeToString(digest[:]), deviceID
}

func provisioningString(values map[string]any, key string) (string, error) {
	value, ok := values[key].(string)
	if !ok || !headerValue(value, 32768) {
		return "", fmt.Errorf("missing anisette %s", key)
	}

	return value, nil
}

func decodeProvisioningField(values map[string]any, key string) ([]byte, error) {
	value, err := provisioningString(values, key)
	if err != nil {
		return nil, err
	}

	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(decoded) == 0 || len(decoded) > anisetteLimit {
		return nil, fmt.Errorf("decode anisette %s", key)
	}

	return decoded, nil
}

func headerValue(value string, limit int) bool {
	if value == "" || len(value) > limit {
		return false
	}

	for _, character := range value {
		if character < ' ' || character > '~' {
			return false
		}
	}

	return true
}
