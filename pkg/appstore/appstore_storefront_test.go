package appstore

import (
	"errors"
	"fmt"
	"strings"

	"github.com/majd/ipatool/v2/pkg/http"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

func testStorefrontCountryCodes() map[string]string {
	return map[string]string{
		"143441": "US",
		"143443": "DE",
		"143444": "GB",
		"143465": "CN",
		"143467": "IN",
		"143469": "RU",
	}
}

func availableStorefrontsFixture() []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
		<plist version="1.0"><dict><key>storefronts</key><array>
			<dict><key>country-code</key><string>US</string><key>id</key><integer>143441</integer></dict>
			<dict><key>country-code</key><string>DE</string><key>id</key><integer>143443</integer></dict>
		</array></dict></plist>`)
}

func storefrontGenreFixture(identifier, countryCode string) []byte {
	return []byte(fmt.Sprintf(`{"storePlatformData":{"genre":{"meta":{"storefront":{"cc":%q,"id":%q}}}}}`, countryCode, identifier))
}

func storefrontSearchItemFixture(kind, adamID, platform, salableAdamID, externalVersionID string) string {
	return fmt.Sprintf(`{
		"$kind":%q,
		"lockup":{"adamId":%q,"bundleId":%q,"title":%q},
		"offer":{"purchaseConfiguration":{
			"metricsPlatformDisplayStyle":%q,
			"appPlatforms":[%q],
			"buyParams":%q
		}}
	}`,
		kind,
		adamID,
		"bundle."+adamID,
		"App "+adamID,
		platform,
		platform,
		"salableAdamId="+salableAdamID+"&appExtVrsId="+externalVersionID,
	)
}

func storefrontPageFixture(items ...string) []byte {
	return []byte(`<html><head></head><body><script data-test="value" id='serialized-server-data' type="application/json">{
		"data":[{"data":{"shelves":[{"items":[` +
		strings.Join(items, ",") +
		`] }]}}]}
	</script></body></html>`)
}

var _ = Describe("App Store storefront data", func() {
	DescribeTable("resolves storefront response suffixes", func(storefront, country string) {
		Expect(countryCodeFromStoreFront(storefront, testStorefrontCountryCodes())).To(Equal(country))
	},
		Entry("comma suffix", "143441,34", "US"),
		Entry("dash and comma suffixes", "143441-1,34", "US"),
		Entry("bare storefront", "143443", "DE"),
	)

	Describe("available storefront discovery", func() {
		It("fetches and caches the storefront country codes", func() {
			client := http.NewMockClient[[]byte](gomock.NewController(GinkgoT()))
			store := &appstore{storefrontClient: client}
			client.EXPECT().Send(http.Request{
				URL:            availableStorefrontsURL,
				Method:         http.MethodGET,
				ResponseFormat: http.ResponseFormatRaw,
			}).Return(http.Result[[]byte]{StatusCode: 200, Data: availableStorefrontsFixture()}, nil).Times(1)

			Expect(store.countryCodeFromStoreFront("143441-1,34")).To(Equal("US"))
			Expect(store.countryCodeFromStoreFront("143443")).To(Equal("DE"))
		})

		It("retries discovery after a request failure", func() {
			client := http.NewMockClient[[]byte](gomock.NewController(GinkgoT()))
			store := &appstore{storefrontClient: client}
			requestErr := errors.New("connection reset")
			gomock.InOrder(
				client.EXPECT().Send(gomock.Any()).Return(http.Result[[]byte]{}, requestErr),
				client.EXPECT().Send(gomock.Any()).Return(http.Result[[]byte]{StatusCode: 200, Data: availableStorefrontsFixture()}, nil),
			)

			_, err := store.countryCodeFromStoreFront("143441")
			Expect(err).To(MatchError(ContainSubstring("available storefronts request failed")))
			Expect(errors.Is(err, requestErr)).To(BeTrue())
			Expect(store.countryCodeFromStoreFront("143441")).To(Equal("US"))
		})

		DescribeTable("resolves storefronts omitted from the available list", func(identifier, countryCode string) {
			client := http.NewMockClient[[]byte](gomock.NewController(GinkgoT()))
			store := &appstore{storefrontClient: client}
			gomock.InOrder(
				client.EXPECT().Send(gomock.Any()).Return(http.Result[[]byte]{StatusCode: 200, Data: availableStorefrontsFixture()}, nil),
				client.EXPECT().Send(http.Request{
					URL:            storefrontGenreURL,
					Method:         http.MethodGET,
					ResponseFormat: http.ResponseFormatRaw,
					NoRedirects:    true,
					Headers:        map[string]string{"X-Apple-Store-Front": identifier + "-2,29"},
				}).Return(http.Result[[]byte]{StatusCode: 200, Data: storefrontGenreFixture(identifier, countryCode)}, nil),
			)

			Expect(store.countryCodeFromStoreFront(identifier + "-19,29")).To(Equal(countryCode))
			Expect(store.countryCodeFromStoreFront(identifier)).To(Equal(countryCode))
		},
			Entry("China", "143465", "CN"),
		)

		DescribeTable("rejects inactive legacy storefronts", func(identifier string) {
			client := http.NewMockClient[[]byte](gomock.NewController(GinkgoT()))
			store := &appstore{storefrontClient: client}
			gomock.InOrder(
				client.EXPECT().Send(gomock.Any()).Return(http.Result[[]byte]{StatusCode: 200, Data: availableStorefrontsFixture()}, nil),
				client.EXPECT().Send(gomock.Any()).Return(http.Result[[]byte]{StatusCode: 302}, nil),
			)

			_, err := store.countryCodeFromStoreFront(identifier)

			Expect(err).To(MatchError(fmt.Sprintf("country code mapping for store front (%s) was not found", identifier)))
		},
			Entry("Bangladesh", "143490"),
			Entry("Liechtenstein", "143522"),
		)

		It("discovers a missing storefront ID for phone login", func() {
			client := http.NewMockClient[[]byte](gomock.NewController(GinkgoT()))
			store := &appstore{
				storefrontClient: client,
				storefrontCountryCodes: map[string]string{
					"143464": "SG",
					"143466": "KR",
				},
			}
			client.EXPECT().Send(gomock.Any()).Do(func(request http.Request) {
				Expect(request.URL).To(Equal(storefrontGenreURL))
				Expect(request.Headers).To(HaveKeyWithValue("X-Apple-Store-Front", "143465-2,29"))
			}).Return(http.Result[[]byte]{StatusCode: 200, Data: storefrontGenreFixture("143465", "CN")}, nil)

			Expect(store.discoverStorefrontID("CN")).To(Equal("143465"))
			Expect(store.storefrontID("CN")).To(Equal("143465"))
		})

		It("rejects an unknown ID when Apple defaults it to a known storefront", func() {
			client := http.NewMockClient[[]byte](gomock.NewController(GinkgoT()))
			store := &appstore{storefrontClient: client}
			gomock.InOrder(
				client.EXPECT().Send(gomock.Any()).Return(http.Result[[]byte]{StatusCode: 200, Data: availableStorefrontsFixture()}, nil),
				client.EXPECT().Send(gomock.Any()).Return(http.Result[[]byte]{StatusCode: 200, Data: storefrontGenreFixture("143441", "US")}, nil),
			)

			_, err := store.countryCodeFromStoreFront("999999")

			Expect(err).To(MatchError("country code mapping for store front (999999) was not found"))
		})

		DescribeTable("rejects unusable discovery responses", func(status int, body []byte, message string) {
			client := http.NewMockClient[[]byte](gomock.NewController(GinkgoT()))
			store := &appstore{storefrontClient: client}
			client.EXPECT().Send(gomock.Any()).Return(http.Result[[]byte]{StatusCode: status, Data: body}, nil)

			_, err := store.countryCodeFromStoreFront("143441")

			Expect(err).To(MatchError(ContainSubstring(message)))
		},
			Entry("HTTP error", 503, nil, "available storefronts request failed"),
			Entry("malformed plist", 200, []byte("not a plist"), "failed to decode available storefronts"),
			Entry("empty storefront list", 200, []byte(`<plist><dict><key>storefronts</key><array/></dict></plist>`), "contains no storefronts"),
		)
	})

	Describe("storefrontVisionApps", func() {
		It("keeps unique native visionOS apps in storefront order and applies the limit", func() {
			body := storefrontPageFixture(
				storefrontSearchItemFixture("EditorialItem", "900", "vision", "900", "9000"),
				storefrontSearchItemFixture("AppSearchResult", "200", "vision", "200", "2000"),
				storefrontSearchItemFixture("AppSearchResult", "300", "ios", "300", "3000"),
				storefrontSearchItemFixture("AppSearchResult", "not-an-id", "vision", "0", "4000"),
				storefrontSearchItemFixture("AppSearchResult", "200", "vision", "200", "2000"),
				storefrontSearchItemFixture("AppSearchResult", "100", "vision", "999", "1000"),
				storefrontSearchItemFixture("AppSearchResult", "100", "vision", "100", "1000"),
				storefrontSearchItemFixture("AppSearchResult", "400", "vision", "400", "4000"),
			)

			apps, err := storefrontVisionApps(body, 2)

			Expect(err).ToNot(HaveOccurred())
			Expect(apps).To(Equal([]App{
				{ID: 200, BundleID: "bundle.200", Name: "App 200"},
				{ID: 100, BundleID: "bundle.100", Name: "App 100"},
			}))
		})

		It("returns an empty slice for a zero limit", func() {
			body := storefrontPageFixture(
				storefrontSearchItemFixture("AppSearchResult", "100", "vision", "100", "1000"),
			)

			apps, err := storefrontVisionApps(body, 0)

			Expect(err).ToNot(HaveOccurred())
			Expect(apps).To(BeEmpty())
		})

		It("caps oversized limits to the storefront result maximum", func() {
			items := make([]string, 0, maxVisionOSSearchResults+1)
			for index := 1; index <= maxVisionOSSearchResults+1; index++ {
				appID := fmt.Sprintf("%d", index)
				items = append(items, storefrontSearchItemFixture("AppSearchResult", appID, "vision", appID, appID+"0"))
			}

			apps, err := storefrontVisionApps(storefrontPageFixture(items...), 1<<62)

			Expect(err).ToNot(HaveOccurred())
			Expect(apps).To(HaveLen(maxVisionOSSearchResults))
		})

		It("returns an error when serialized data is absent", func() {
			_, err := storefrontVisionApps([]byte("<html></html>"), 5)

			Expect(err).To(MatchError("serialized server data was not found"))
		})

		It("returns an error when serialized data is invalid JSON", func() {
			_, err := storefrontVisionApps([]byte(`<script id="serialized-server-data">not-json</script>`), 5)

			Expect(err).To(MatchError(ContainSubstring("failed to decode serialized server data")))
		})
	})

	Describe("visionExternalVersionID", func() {
		It("selects the matching vision purchase configuration", func() {
			body := []byte(`<script id="serialized-server-data">{
				"data":[
					{"purchaseConfiguration":{"metricsPlatformDisplayStyle":"ios","appPlatforms":["iphone"],"buyParams":"salableAdamId=100&appExtVrsId=ios-version"}},
					{"nested":{"purchaseConfiguration":{"metricsPlatformDisplayStyle":"vision","appPlatforms":["vision"],"buyParams":"salableAdamId=999&appExtVrsId=wrong-app"}}},
					{"nested":{"purchaseConfiguration":{"metricsPlatformDisplayStyle":"vision","appPlatforms":["vision"],"buyParams":"salableAdamId=100&appExtVrsId=vision-version"}}}
				]}
			</script>`)

			externalVersionID, err := visionExternalVersionID(body, 100)

			Expect(err).ToNot(HaveOccurred())
			Expect(externalVersionID).To(Equal("vision-version"))
		})

		It("returns an error when no matching vision purchase configuration exists", func() {
			body := []byte(`<script id="serialized-server-data">{
				"purchaseConfiguration":{"metricsPlatformDisplayStyle":"ios","appPlatforms":["iphone"],"buyParams":"salableAdamId=100&appExtVrsId=ios-version"}
			}</script>`)

			_, err := visionExternalVersionID(body, 100)

			Expect(err).To(MatchError("visionOS purchase configuration was not found"))
		})

		It("distinguishes a matching configuration with no external version id", func() {
			body := []byte(`<script id="serialized-server-data">{
				"purchaseConfiguration":{"metricsPlatformDisplayStyle":"vision","appPlatforms":["vision"],"buyParams":"salableAdamId=100"}
			}</script>`)

			_, err := visionExternalVersionID(body, 100)

			Expect(err).To(MatchError("visionOS purchase configuration has no external version id"))
		})

		It("prefers a populated matching configuration over an empty duplicate", func() {
			body := []byte(`<script id="serialized-server-data">{
				"data":[
					{"purchaseConfiguration":{"metricsPlatformDisplayStyle":"vision","appPlatforms":["vision"],"buyParams":"salableAdamId=100"}},
					{"purchaseConfiguration":{"metricsPlatformDisplayStyle":"vision","appPlatforms":["vision"],"buyParams":"salableAdamId=100&appExtVrsId=vision-version"}}
				]
			}</script>`)

			externalVersionID, err := visionExternalVersionID(body, 100)

			Expect(err).ToNot(HaveOccurred())
			Expect(externalVersionID).To(Equal("vision-version"))
		})

		It("returns an error when the serialized script is not closed", func() {
			_, err := visionExternalVersionID([]byte(`<script id="serialized-server-data">{}`), 100)

			Expect(err).To(MatchError("serialized server data script is not closed"))
		})
	})
})
