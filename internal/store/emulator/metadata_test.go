package emulator

import (
	"context"
	"encoding/json"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Prepared Store metadata", func() {
	var cache metadataCache
	var raw []byte
	BeforeEach(func() {
		cache = metadataCache{gate: make(chan struct{}, 1)}
		var err error
		raw, err = json.Marshal(Profile{
			UUID:    "5DFC37A5-4689-3F9D-9655-D4774E1D8D08",
			Strings: map[string]string{"8192": "original"},
			Constants: map[string]Constant{
				"4096":  {Kind: "NSDictionary", Entries: map[string]uint64{"value": 8192}},
				"12288": {Kind: "NSArray", Items: []uint64{8192}},
			},
			Classes: []Class{{Name: "NSObject", Address: 16384, Size: 16}},
		})
		Expect(err).NotTo(HaveOccurred())
	})
	It("reuses metadata by content and rejects changed invalid metadata", func() {
		first, err := cache.load(context.Background(), raw)
		Expect(err).NotTo(HaveOccurred())
		second, err := cache.load(context.Background(), append([]byte(nil), raw...))
		Expect(err).NotTo(HaveOccurred())
		Expect(second).To(BeIdenticalTo(first))
		_, err = cache.load(context.Background(), []byte(`{"UUID":"wrong"}`))
		Expect(err).To(MatchError(ContainSubstring("unsupported Store profile")))
		_, err = cache.load(context.Background(), []byte(`invalid json`))
		Expect(err).To(HaveOccurred())
		second, err = cache.load(context.Background(), raw)
		Expect(err).NotTo(HaveOccurred())
		Expect(second).To(BeIdenticalTo(first))
	})
	It("does not share mutable constants or dynamic registrations between VMs", func() {
		prepared, err := cache.load(context.Background(), raw)
		Expect(err).NotTo(HaveOccurred())
		first, second := prepared.newRuntime(), prepared.newRuntime()
		first.objects[4096].Value.(map[string]uint64)["value"] = 99
		first.objects[12288].Value.([]uint64)[0] = 99
		first.objects[8192].Value = "changed"
		first.objects[8192].Properties = map[string]uint64{"account": 99}
		first.classes["OnlyFirst"] = &Class{Name: "OnlyFirst"}
		first.classNames[99] = "OnlyFirst"
		first.methods["dynamic"] = 99
		first.selectors[99] = "dynamic"
		first.selectorAddresses["dynamic"] = 99
		for _, guest := range []*Runtime{second, prepared.newRuntime()} {
			Expect(guest.objects[4096].Value.(map[string]uint64)["value"]).To(Equal(uint64(8192)))
			Expect(guest.objects[12288].Value).To(Equal([]uint64{8192}))
			Expect(guest.objects[8192].Value).To(Equal("original"))
			Expect(guest.objects[8192].Properties).To(BeEmpty())
			Expect(guest.classes).NotTo(HaveKey("OnlyFirst"))
			Expect(guest.classNames).NotTo(HaveKey(uint64(99)))
			Expect(guest.methods).NotTo(HaveKey("dynamic"))
			Expect(guest.selectors).NotTo(HaveKey(uint64(99)))
			Expect(guest.selectorAddresses).NotTo(HaveKey("dynamic"))
		}
	})
	It("prepares one template for concurrent independent runtimes", func() {
		var wg sync.WaitGroup
		prepared := make([]*runtimeMetadata, 8)
		for i := range prepared {
			wg.Go(func() {
				defer GinkgoRecover()
				var err error
				prepared[i], err = cache.load(context.Background(), raw)
				Expect(err).NotTo(HaveOccurred())
				guest := prepared[i].newRuntime()
				guest.objects[4096].Value.(map[string]uint64)["value"] = uint64(i)
			})
		}
		wg.Wait()
		for _, value := range prepared {
			Expect(value).To(BeIdenticalTo(prepared[0]))
		}
		Expect(prepared[0].objects[4096].Value.(map[string]uint64)["value"]).To(Equal(uint64(8192)))
	})
	It("honors cancellation while another preparation holds the cache", func() {
		cache.gate <- struct{}{}
		defer func() { <-cache.gate }()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := cache.load(ctx, raw)
		Expect(err).To(MatchError(context.Canceled))
	})
})
