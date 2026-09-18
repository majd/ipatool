package appstore

import (
	"errors"
	"net/url"

	"github.com/majd/ipatool/v2/pkg/http"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("Platform version catalog fallback", func() {
	var client *http.MockClient[platformVersionLookupResult]
	var store *appstore
	account := Account{StoreFront: "143469-1,34"}
	app := App{ID: 6472431552}
	missing := platformVersionLookupResult{}
	noOffers := platformVersionLookupResult{Results: map[string]platformVersionLookupItem{"6472431552": {}}}
	withOffer := func(offer platformVersionLookupOffer) platformVersionLookupResult {
		return platformVersionLookupResult{Results: map[string]platformVersionLookupItem{"6472431552": {Offers: []platformVersionLookupOffer{offer}}}}
	}
	valid := withOffer(platformVersionLookupOffer{Version: platformVersionLookupVersion{ExternalID: "891116578"}})

	BeforeEach(func() {
		client = http.NewMockClient[platformVersionLookupResult](gomock.NewController(GinkgoT()))
		store = &appstore{platformClient: client}
	})

	expectLookup := func(catalog string, status int, data platformVersionLookupResult, sendErr error) *gomock.Call {
		return client.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
			u, err := url.Parse(req.URL)
			Expect(err).ToNot(HaveOccurred())
			Expect(u.Query().Get("platform")).To(Equal(catalog))
			Expect(u.Query().Get("cc")).To(Equal("ru"))
			Expect(u.Query().Get("id")).To(Equal("6472431552"))
		}).Return(http.Result[platformVersionLookupResult]{StatusCode: status, Data: data}, sendErr)
	}

	DescribeTable("stops at the first available iOS catalog", func(platform Platform, empty platformVersionLookupResult, successIndex int, offer platformVersionLookupResult) {
		var calls []any
		for i, catalog := range []string{"enterprisestore", "iphone", "ipad"} {
			data := empty
			if i == successIndex {
				data = offer
			}
			calls = append(calls, expectLookup(catalog, 200, data, nil))
			if i == successIndex {
				break
			}
		}
		gomock.InOrder(calls...)
		version, err := store.lookupLatestExternalVersionID(account, app, platform)
		Expect(err).ToNot(HaveOccurred())
		Expect(version).To(Equal("891116578"))
	},
		Entry("enterprise succeeds", PlatformIPhone, missing, 0, valid),
		Entry("missing enterprise app", PlatformIPhone, missing, 1, valid),
		Entry("empty enterprise offers", PlatformIPhone, noOffers, 1, valid),
		Entry("only iPad catalog succeeds", PlatformIPhone, missing, 2, valid),
		Entry("empty offers until iPad", PlatformIPad, noOffers, 2, valid),
		Entry("iPad request uses iPhone fallback first", PlatformIPad, missing, 1, valid),
		Entry("consumer buy params version", PlatformIPhone, missing, 1, withOffer(platformVersionLookupOffer{BuyParams: "appExtVrsId=891116578"})),
	)

	DescribeTable("reports exhausted catalogs", func(empty platformVersionLookupResult, message string) {
		gomock.InOrder(expectLookup("enterprisestore", 200, empty, nil), expectLookup("iphone", 200, empty, nil), expectLookup("ipad", 200, empty, nil))
		version, err := store.lookupLatestExternalVersionID(account, app, PlatformIPhone)
		Expect(version).To(BeEmpty())
		Expect(err).To(MatchError(And(ContainSubstring(message), ContainSubstring("6472431552"), ContainSubstring("RU"), ContainSubstring("enterprisestore, iphone, ipad"))))
	}, Entry("missing apps", missing, "returned no app"), Entry("empty offers", noOffers, "returned no offers"))

	DescribeTable("does not fall back after a request or offer error", func(status int, data platformVersionLookupResult, sendErr error, message string) {
		expectLookup("enterprisestore", status, data, sendErr)
		_, err := store.lookupLatestExternalVersionID(account, app, PlatformIPhone)
		Expect(err).To(MatchError(ContainSubstring(message)))
		if sendErr != nil {
			Expect(errors.Is(err, sendErr)).To(BeTrue())
		}
	},
		Entry("transport failure", 0, missing, errors.New("connection reset"), "request failed"),
		Entry("decoding failure", 200, missing, errors.New("invalid JSON"), "request failed"),
		Entry("HTTP failure", 503, missing, nil, "request failed"),
		Entry("missing version", 200, withOffer(platformVersionLookupOffer{}), nil, "no external version id"),
		Entry("malformed buy params", 200, withOffer(platformVersionLookupOffer{BuyParams: "appExtVrsId=%zz"}), nil, "failed to parse buy params"),
	)

	It("stops on an HTTP error in a consumer catalog", func() {
		gomock.InOrder(expectLookup("enterprisestore", 200, missing, nil), expectLookup("iphone", 503, missing, nil))
		_, err := store.lookupLatestExternalVersionID(account, app, PlatformIPhone)
		Expect(err).To(MatchError(ContainSubstring("request failed")))
	})

	It("does not use iOS catalogs for Apple TV", func() {
		expectLookup("atv9", 200, missing, nil)
		_, err := store.lookupLatestExternalVersionID(account, app, PlatformAppleTV)
		Expect(err).To(MatchError(ContainSubstring("returned no app")))
	})
})
