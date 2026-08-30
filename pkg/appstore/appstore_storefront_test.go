package appstore

import (
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

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
