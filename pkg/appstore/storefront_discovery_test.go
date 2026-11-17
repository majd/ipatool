package appstore

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/majd/ipatool/v2/pkg/http"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"howett.net/plist"
)

const discoveredStorefrontListURL = "https://itunes.apple.com/discovered/storefronts?revision=fixture"

func scopedStorefrontBag(identifier any, country string) http.Result[storefrontBagResult] {
	return http.Result[storefrontBagResult]{StatusCode: 200, Data: storefrontBagResult{StorefrontID: identifier, CountryCode: country, AvailableStorefrontsURL: discoveredStorefrontListURL}}
}

var _ = Describe("Dynamic storefront discovery", func() {
	var store *appstore
	var bags *http.MockClient[storefrontBagResult]
	var lists *http.MockClient[[]byte]
	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		bags = http.NewMockClient[storefrontBagResult](ctrl)
		lists = http.NewMockClient[[]byte](ctrl)
		store = &appstore{storefrontBagClient: bags, storefrontClient: lists}
	})

	DescribeTable("resolves and caches a scoped bag without requesting the unstable global list", func(identifier, country, suffix string) {
		bags.EXPECT().Send(gomock.Any()).DoAndReturn(func(request http.Request) (http.Result[storefrontBagResult], error) {
			Expect(request.URL).To(Equal("https://init.itunes.apple.com/bag.xml?ix=6"))
			Expect(request.Headers["User-Agent"]).To(Equal(storeAuthenticationUserAgent))
			Expect(request.Headers["X-Apple-Store-Front"]).To(Equal(identifier + suffix))
			Expect(request.ResponseFormat).To(Equal(http.ResponseFormatXML))

			return scopedStorefrontBag(identifier, country), nil
		}).Times(1)
		lists.EXPECT().Send(gomock.Any()).Times(0)
		Expect(store.countryCodeFromStoreFront(identifier + suffix)).To(Equal(country))
		Expect(store.countryCodeFromStoreFront(identifier)).To(Equal(country))
		Expect(store.discoverStorefrontID(country)).To(Equal(identifier))
	}, Entry("Vietnam, issue 670", "143471", "VN", "-1,34"), Entry("Japan", "143462", "JP", "-1,34"), Entry("bare ID", "143469", "RU", ""), Entry("new server-provided country mapping", "987654", "ZZ", "-1,34"))

	It("resolves a phone account's country directly, including countries omitted from the global list", func() {
		bags.EXPECT().Send(gomock.Any()).DoAndReturn(func(request http.Request) (http.Result[storefrontBagResult], error) {
			endpoint, err := url.Parse(request.URL)
			Expect(err).NotTo(HaveOccurred())
			Expect(endpoint.Query().Get("cc")).To(Equal("CN"))
			Expect(request.Headers).NotTo(HaveKey("X-Apple-Store-Front"))

			return scopedStorefrontBag("143465", "CN"), nil
		}).Times(1)
		Expect(store.discoverStorefrontID(" cn ")).To(Equal("143465"))
		Expect(store.countryCodeFromStoreFront("143465-19,29")).To(Equal("CN"))
	})

	DescribeTable("uses the endpoint advertised by bags without storefront metadata", func(nested bool) {
		bag := scopedStorefrontBag(nil, "")
		if nested {
			bag.Data.URLBag.AvailableStorefrontsURL = discoveredStorefrontListURL + "&nested=true"
		}
		endpoint := discoveredStorefrontListURL
		if nested {
			endpoint += "&nested=true"
		}
		bags.EXPECT().Send(gomock.Any()).Return(bag, nil).Times(1)
		lists.EXPECT().Send(gomock.Any()).DoAndReturn(func(request http.Request) (http.Result[[]byte], error) {
			Expect(request.URL).To(Equal(endpoint))
			Expect(request.Headers["User-Agent"]).To(Equal(storeAuthenticationUserAgent))

			return http.Result[[]byte]{StatusCode: 200, Data: availableStorefrontsFixture()}, nil
		}).Times(1)
		Expect(store.countryCodeFromStoreFront("143441-1,34")).To(Equal("US"))
		Expect(store.countryCodeFromStoreFront("143443")).To(Equal("DE"))
	}, Entry("top-level bag", false), Entry("nested URL bag", true))

	It("does not mistake a partially populated cache for the complete list", func() {
		gomock.InOrder(bags.EXPECT().Send(gomock.Any()).Return(scopedStorefrontBag("143471", "VN"), nil), bags.EXPECT().Send(gomock.Any()).Return(scopedStorefrontBag(nil, ""), nil))
		lists.EXPECT().Send(gomock.Any()).Return(http.Result[[]byte]{StatusCode: 200, Data: availableStorefrontsFixture()}, nil).Times(1)
		Expect(store.countryCodeFromStoreFront("143471")).To(Equal("VN"))
		Expect(store.countryCodeFromStoreFront("143443")).To(Equal("DE"))
		Expect(store.countryCodeFromStoreFront("143471")).To(Equal("VN"))
		Expect(store.countryCodeFromStoreFront("143441")).To(Equal("US"))
	})

	DescribeTable("does not accept Apple's default storefront for an unmatched request", func(byCountry bool) {
		bags.EXPECT().Send(gomock.Any()).Return(scopedStorefrontBag("143441", "US"), nil)
		lists.EXPECT().Send(gomock.Any()).Return(http.Result[[]byte]{StatusCode: 200, Data: availableStorefrontsFixture()}, nil)
		if byCountry {
			_, err := store.discoverStorefrontID("ZZ")
			Expect(err).To(MatchError(ContainSubstring("mapping for country code (ZZ) was not found")))
		} else {
			_, err := store.countryCodeFromStoreFront("999999")
			Expect(err).To(MatchError("country code mapping for store front (999999) was not found"))
			Expect(store.storefrontCountryCodes).NotTo(HaveKey("999999"))
		}
	}, Entry("unknown ID", false), Entry("unknown country", true))

	It("does not poison discovery after a transient list 404", func() {
		gomock.InOrder(bags.EXPECT().Send(gomock.Any()).Return(scopedStorefrontBag(nil, ""), nil), bags.EXPECT().Send(gomock.Any()).Return(scopedStorefrontBag("143471", "VN"), nil))
		lists.EXPECT().Send(gomock.Any()).Return(http.Result[[]byte]{StatusCode: 404}, nil).Times(1)
		_, err := store.countryCodeFromStoreFront("143471")
		Expect(err).To(MatchError(ContainSubstring("available storefronts request failed")))
		Expect(store.storefrontListLoaded).To(BeFalse())
		Expect(store.countryCodeFromStoreFront("143471")).To(Equal("VN"))
	})

	It("retries bag discovery on a later call after a transport failure", func() {
		failure := errors.New("connection reset")
		gomock.InOrder(bags.EXPECT().Send(gomock.Any()).Return(http.Result[storefrontBagResult]{}, failure), bags.EXPECT().Send(gomock.Any()).Return(scopedStorefrontBag("143471", "VN"), nil))
		_, err := store.countryCodeFromStoreFront("143471")
		Expect(errors.Is(err, failure)).To(BeTrue())
		Expect(store.countryCodeFromStoreFront("143471")).To(Equal("VN"))
	})

	DescribeTable("rejects unusable list responses without caching them", func(status int, body []byte, message string) {
		bags.EXPECT().Send(gomock.Any()).Return(scopedStorefrontBag(nil, ""), nil)
		lists.EXPECT().Send(gomock.Any()).Return(http.Result[[]byte]{StatusCode: status, Data: body}, nil)
		_, err := store.countryCodeFromStoreFront("143441")
		Expect(err).To(MatchError(ContainSubstring(message)))
		Expect(store.storefrontListLoaded).To(BeFalse())
	}, Entry("HTTP error", 503, nil, "available storefronts request failed"), Entry("invalid plist", 200, []byte("not plist"), "failed to decode available storefronts"), Entry("empty list", 200, []byte(`<plist><dict><key>storefronts</key><array/></dict></plist>`), "contains no storefronts"), Entry("invalid countries", 200, []byte(`<plist><dict><key>storefronts</key><array><dict><key>id</key><integer>1</integer><key>country-code</key><string>bad</string></dict></array></dict></plist>`), "contains no storefronts"))

	DescribeTable("rejects untrusted list endpoints without contacting them", func(endpoint string) {
		bag := scopedStorefrontBag(nil, "")
		bag.Data.AvailableStorefrontsURL = endpoint
		bags.EXPECT().Send(gomock.Any()).Return(bag, nil)
		_, err := store.countryCodeFromStoreFront("143441")
		Expect(err).To(MatchError("invalid available-storefronts endpoint in bag"))
	}, Entry("absent", ""), Entry("HTTP", "http://itunes.apple.com/storefronts"), Entry("credentials", "https://user@itunes.apple.com/storefronts"), Entry("foreign host", "https://apple.com.example.com/storefronts"), Entry("fragment", "https://itunes.apple.com/storefronts#other"), Entry("custom port", "https://itunes.apple.com:8080/storefronts"))

	DescribeTable("rejects malformed storefront identifiers before network access", func(value string) {
		_, err := store.countryCodeFromStoreFront(value)
		Expect(err).To(HaveOccurred())
	}, Entry("empty", ""), Entry("zero", "0"), Entry("negative", "-1"), Entry("text", "invalid"), Entry("overflow", "99999999999999999999"))

	DescribeTable("decodes both numeric and string bag identifiers", func(encoded string) {
		var bag storefrontBagResult
		_, err := plist.Unmarshal([]byte(fmt.Sprintf(`<plist><dict><key>storeFrontId</key>%s<key>currentStorefrontCountryCodeISO2A</key><string>VN</string></dict></plist>`, encoded)), &bag)
		Expect(err).NotTo(HaveOccurred())
		bags.EXPECT().Send(gomock.Any()).Return(http.Result[storefrontBagResult]{StatusCode: 200, Data: bag}, nil)
		Expect(store.countryCodeFromStoreFront("143471")).To(Equal("VN"))
	}, Entry("string", "<string>143471</string>"), Entry("integer", "<integer>143471</integer>"))
})
