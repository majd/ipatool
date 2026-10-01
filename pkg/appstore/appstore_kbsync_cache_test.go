package appstore

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"

	"github.com/byteness/keyring"
	"github.com/majd/ipatool/v2/pkg/keychain"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("Persistent kbsync cache", func() {
	const guid = "001122334455"
	const dsid = uint64(123456789)
	var (
		chain             *keychain.MockKeychain
		stored            []byte
		readErr, writeErr error
		removed           int
		blob              string
		newStore          func() *appstore
	)
	BeforeEach(func() {
		chain = keychain.NewMockKeychain(gomock.NewController(GinkgoT()))
		stored = nil
		readErr, writeErr = nil, nil
		removed = 0
		blob = base64.StdEncoding.EncodeToString([]byte("working blob"))
		chain.EXPECT().Get(kbsyncCacheKey).DoAndReturn(func(string) ([]byte, error) {
			if readErr != nil {
				return nil, readErr
			}
			if stored == nil {
				return nil, keyring.ErrKeyNotFound
			}

			return bytes.Clone(stored), nil
		}).AnyTimes()
		chain.EXPECT().Set(kbsyncCacheKey, gomock.Any()).DoAndReturn(func(_ string, data []byte) error {
			if writeErr != nil {
				return writeErr
			}
			stored = bytes.Clone(data)

			return nil
		}).AnyTimes()
		chain.EXPECT().Remove(kbsyncCacheKey).DoAndReturn(func(string) error {
			removed++
			stored = nil

			return nil
		}).AnyTimes()
		newStore = func() *appstore { return &appstore{keychain: chain} }
	})

	It("reuses a keychain blob in a fresh client with the same account and device", func() {
		newStore().storeKBSync(dsid, guid, blob)
		var entry kbsyncCacheEntry
		Expect(json.Unmarshal(stored, &entry)).To(Succeed())
		Expect(entry).To(Equal(kbsyncCacheEntry{DSID: dsid, GUID: guid, Data: blob}))
		Expect(newStore().cachedKBSync(dsid, guid)).To(Equal(blob))
	})

	DescribeTable("misses for a different account or device", func(nextDSID uint64, nextGUID string) {
		store := newStore()
		store.storeKBSync(dsid, guid, blob)
		Expect(store.cachedKBSync(nextDSID, nextGUID)).To(BeEmpty())
		Expect(newStore().cachedKBSync(nextDSID, nextGUID)).To(BeEmpty())
	}, Entry("account changed", dsid+1, guid), Entry("device changed", dsid, "001122334466"))

	DescribeTable("ignores an unusable persistent entry", func(record string) {
		stored = []byte(record)
		Expect(newStore().cachedKBSync(dsid, guid)).To(BeEmpty())
	},
		Entry("malformed JSON", "{"), Entry("missing identity", `{"data":"YmxvYg=="}`),
		Entry("invalid base64", `{"dsid":123456789,"guid":"001122334455","data":"!"}`),
		Entry("empty data", `{"dsid":123456789,"guid":"001122334455","data":""}`),
	)

	It("does not borrow data from another account when a persistent entry is incomplete", func() {
		store := newStore()
		store.storeKBSync(dsid, guid, blob)
		stored = []byte(`{"dsid":987654321,"guid":"001122334455"}`)
		Expect(store.cachedKBSync(987654321, guid)).To(BeEmpty())
	})

	It("keeps the memory cache usable when keychain reads or writes fail", func() {
		readErr = errors.New("cache read failed")
		writeErr = errors.New("cache write failed")
		store := newStore()
		Expect(store.cachedKBSync(dsid, guid)).To(BeEmpty())
		store.storeKBSync(dsid, guid, blob)
		Expect(store.cachedKBSync(dsid, guid)).To(Equal(blob))
		Expect(stored).To(BeNil())
	})

	It("does not cache a generated blob before it has been used", func() {
		store := newStore()
		store.kbsyncGenerator = func(context.Context, []byte, uint64) ([]byte, error) { return []byte("new blob"), nil }
		generated, err := store.generateKBSync(context.Background(), nil, dsid)
		Expect(err).NotTo(HaveOccurred())
		Expect(generated).NotTo(BeEmpty())
		Expect(store.cachedKBSync(dsid, guid)).To(BeEmpty())
		Expect(stored).To(BeNil())
	})

	It("rejects failed generation and empty blobs", func() {
		store := newStore()
		failure := errors.New("guest failed")
		store.kbsyncGenerator = func(context.Context, []byte, uint64) ([]byte, error) { return nil, failure }
		_, err := store.generateKBSync(context.Background(), nil, dsid)
		Expect(errors.Is(err, failure)).To(BeTrue())
		store.kbsyncGenerator = func(context.Context, []byte, uint64) ([]byte, error) { return nil, nil }
		_, err = store.generateKBSync(context.Background(), nil, dsid)
		Expect(err).To(HaveOccurred())
		Expect(stored).To(BeNil())
	})

	It("removes a rejected blob from both caches", func() {
		store := newStore()
		store.storeKBSync(dsid, guid, blob)
		store.invalidateKBSync(dsid, guid, blob)
		Expect(removed).To(Equal(1))
		Expect(stored).To(BeNil())
		Expect(store.cachedKBSync(dsid, guid)).To(BeEmpty())
	})

	It("clears persistent and in-memory blobs when revoking the account", func() {
		store := newStore()
		store.storeKBSync(dsid, guid, blob)
		chain.EXPECT().Remove("account").Return(nil)
		Expect(store.Revoke()).To(Succeed())
		Expect(stored).To(BeNil())
		Expect(removed).To(Equal(1))
		Expect(store.cachedKBSync(dsid, guid)).To(BeEmpty())
	})

	It("does not evict a different account or a newer persisted blob", func() {
		first := newStore()
		first.storeKBSync(dsid, guid, blob)
		newStore().storeKBSync(dsid+1, guid, blob)
		first.invalidateKBSync(dsid, guid, blob)
		Expect(removed).To(BeZero())
		Expect(newStore().cachedKBSync(dsid+1, guid)).To(Equal(blob))

		newer := base64.StdEncoding.EncodeToString([]byte("newer"))
		newStore().storeKBSync(dsid, guid, newer)
		first.invalidateKBSync(dsid, guid, blob)
		Expect(removed).To(BeZero())
		Expect(newStore().cachedKBSync(dsid, guid)).To(Equal(newer))
	})
})
