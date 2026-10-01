package appstore

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/byteness/keyring"
)

func (t *appstore) Revoke() error {
	accountErr := t.keychain.Remove("account")
	if accountErr != nil {
		accountErr = fmt.Errorf("failed to remove account from keychain: %w", accountErr)
	}

	t.kbsyncCache.mu.Lock()
	defer t.kbsyncCache.mu.Unlock()

	t.kbsyncCache.entry = kbsyncCacheEntry{}

	cacheErr := t.keychain.Remove(kbsyncCacheKey)
	if errors.Is(cacheErr, keyring.ErrKeyNotFound) || errors.Is(cacheErr, fs.ErrNotExist) {
		cacheErr = nil
	} else if cacheErr != nil {
		cacheErr = fmt.Errorf("failed to remove kbsync cache from keychain: %w", cacheErr)
	}

	return errors.Join(accountErr, cacheErr)
}
