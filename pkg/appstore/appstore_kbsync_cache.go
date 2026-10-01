package appstore

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
)

// Version the key when the pinned StoreAgent profile or cache format changes.
const kbsyncCacheKey = "kbsync-v1"

type kbsyncCacheEntry struct {
	DSID uint64 `json:"dsid"`
	GUID string `json:"guid"`
	Data string `json:"data"`
}

// Cache a single account/device pair, separately from Account so the blob is
// neither exposed in account output nor overwritten by a token refresh.
type kbsyncCache struct {
	mu    sync.Mutex
	entry kbsyncCacheEntry
}

func (t *appstore) generateKBSync(ctx context.Context, hardwareID []byte, dsid uint64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("generate kbsync: %w", err)
	}

	if t.kbsyncGenerator == nil {
		return "", errors.New("kbsync generator is unavailable")
	}

	data, err := t.kbsyncGenerator(ctx, hardwareID, dsid)
	if err != nil {
		return "", fmt.Errorf("generate download kbsync: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("generate kbsync: %w", err)
	}

	if len(data) == 0 {
		return "", errors.New("generated download kbsync is empty")
	}

	return base64.StdEncoding.EncodeToString(data), nil
}

// Store only blobs that have served a valid ent/download response.
func (t *appstore) storeKBSync(dsid uint64, guid, data string) {
	entry := kbsyncCacheEntry{DSID: dsid, GUID: guid, Data: data}

	t.kbsyncCache.mu.Lock()
	defer t.kbsyncCache.mu.Unlock()

	t.kbsyncCache.entry = entry
	if t.keychain != nil {
		// Cache storage is best effort: an unavailable keychain must not turn
		// successful generation into a failed download.
		if encoded, err := json.Marshal(entry); err == nil {
			_ = t.keychain.Set(kbsyncCacheKey, encoded)
		}
	}
}

func (t *appstore) cachedKBSync(dsid uint64, guid string) string {
	t.kbsyncCache.mu.Lock()
	defer t.kbsyncCache.mu.Unlock()

	entry := t.kbsyncCache.entry
	if entry.DSID == dsid && entry.GUID == guid && entry.Data != "" {
		return entry.Data
	}

	if t.keychain == nil {
		return ""
	}

	encoded, err := t.keychain.Get(kbsyncCacheKey)
	if err != nil {
		return ""
	}

	entry = kbsyncCacheEntry{}
	if json.Unmarshal(encoded, &entry) != nil || entry.DSID != dsid || entry.GUID != guid {
		return ""
	}

	data, err := base64.StdEncoding.DecodeString(entry.Data)
	if err != nil || len(data) == 0 {
		return ""
	}

	t.kbsyncCache.entry = entry

	return entry.Data
}

func (t *appstore) invalidateKBSync(dsid uint64, guid, data string) {
	t.kbsyncCache.mu.Lock()
	defer t.kbsyncCache.mu.Unlock()

	rejected := kbsyncCacheEntry{DSID: dsid, GUID: guid, Data: data}
	if t.kbsyncCache.entry == rejected {
		t.kbsyncCache.entry = kbsyncCacheEntry{}
	}

	if t.keychain == nil {
		return
	}

	// A concurrent request may have cached a different account or a newer
	// blob. Only remove the exact entry used by the failed request.
	encoded, err := t.keychain.Get(kbsyncCacheKey)
	if err != nil {
		return
	}

	var entry kbsyncCacheEntry
	if json.Unmarshal(encoded, &entry) == nil && entry == rejected {
		_ = t.keychain.Remove(kbsyncCacheKey)
	}
}
