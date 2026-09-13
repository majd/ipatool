package appstore

import (
	"encoding/json"
	"errors"
	"net/url"

	"github.com/majd/ipatool/v2/pkg/http"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("AppStore (Search)", func() {
	var (
		ctrl                 *gomock.Controller
		mockClient           *http.MockClient[searchResult]
		mockStorefrontClient *http.MockClient[[]byte]
		as                   AppStore
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockClient = http.NewMockClient[searchResult](ctrl)
		mockStorefrontClient = http.NewMockClient[[]byte](ctrl)
		as = &appstore{
			searchClient:     mockClient,
			storefrontClient: mockStorefrontClient,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	DescribeTable("decodes catalog platforms",
		func(metadata string, expected []Platform) {
			var result searchResult
			Expect(json.Unmarshal([]byte(`{"resultCount":1,"results":[{"trackId":42,`+metadata+`}]}`), &result)).To(Succeed())
			mockClient.EXPECT().Send(gomock.Any()).Return(http.Result[searchResult]{StatusCode: 200, Data: result}, nil)
			out, err := as.Search(SearchInput{Account: Account{StoreFront: "143441"}})
			Expect(err).ToNot(HaveOccurred())
			Expect(out.Count).To(Equal(1))
			Expect(out.Results).To(Equal([]App{{ID: 42, Platforms: expected}}))
		},
		Entry("universal app with duplicate devices", `"supportedDevices":["iPadAir-iPadAir","iPhone5s-iPhone5s","iPhone6-iPhone6","iPodTouchSixthGen-iPodTouchSixthGen"]`, []Platform{PlatformIPhone, PlatformIPad}),
		Entry("iPad only", `"supportedDevices":["iPadAir-iPadAir"]`, []Platform{PlatformIPad}),
		Entry("iPod", `"supportedDevices":["iPodTouchSixthGen-iPodTouchSixthGen"]`, []Platform{PlatformIPhone}),
		Entry("TV", `"supportedDevices":["AppleTV4-AppleTV4"]`, []Platform{PlatformAppleTV}),
		Entry("vision", `"supportedDevices":["RealityDevice-RealityDevice"]`, []Platform{PlatformVisionOS}),
		Entry("Mac", `"kind":"mac-software"`, []Platform{PlatformMacOS}),
		Entry("multiple families", `"supportedDevices":["MacDesktop-MacDesktop","RealityDevice-RealityDevice","iPadAir-iPadAir"]`, []Platform{PlatformIPad, PlatformVisionOS, PlatformMacOS}),
		Entry("missing metadata", `"kind":"software"`, []Platform{PlatformUnknown}),
		Entry("unrecognized devices", `"supportedDevices":["FutureDevice"]`, []Platform{PlatformUnknown}),
	)

	When("request is successful", func() {
		const (
			testID       = 0
			testBundleID = "test-bundle-id"
			testName     = "test-name"
			testVersion  = "test-version"
			testPrice    = 0.0
		)

		BeforeEach(func() {
			mockClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[searchResult]{
					StatusCode: 200,
					Data: searchResult{
						Count: 1,
						Results: []App{
							{
								ID:       testID,
								BundleID: testBundleID,
								Name:     testName,
								Version:  testVersion,
								Price:    testPrice,
							},
						},
					},
				}, nil)
		})

		It("returns output", func() {
			out, err := as.Search(SearchInput{
				Account: Account{
					StoreFront: "143441",
				},
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(out.Count).To(Equal(1))
			Expect(out.Results).To(HaveLen(1))
			Expect(out.Results[0]).To(Equal(App{
				ID:       testID,
				BundleID: testBundleID,
				Name:     testName,
				Version:  testVersion,
				Price:    testPrice,
			}))
		})
	})

	When("platform is macOS", func() {
		BeforeEach(func() {
			mockClient.EXPECT().
				Send(gomock.Any()).
				Do(func(req http.Request) {
					parsedURL, err := url.Parse(req.URL)
					Expect(err).ToNot(HaveOccurred())
					Expect(parsedURL.Query().Get("entity")).To(Equal("macSoftware"))
				}).
				Return(http.Result[searchResult]{}, errors.New("request error"))
		})

		It("uses the macOS search entity", func() {
			_, err := as.Search(SearchInput{
				Account: Account{
					StoreFront: "143441",
				},
				Platform: PlatformMacOS,
			})
			Expect(err).To(HaveOccurred())
		})
	})

	When("platform is AppleTV", func() {
		BeforeEach(func() {
			mockClient.EXPECT().
				Send(gomock.Any()).
				Do(func(req http.Request) {
					parsedURL, err := url.Parse(req.URL)
					Expect(err).ToNot(HaveOccurred())
					Expect(parsedURL.Query().Get("entity")).To(Equal("software,tvSoftware"))
				}).
				Return(http.Result[searchResult]{}, errors.New("request error"))
		})

		It("uses the tvOS search entity", func() {
			_, err := as.Search(SearchInput{
				Account: Account{
					StoreFront: "143441",
				},
				Platform: PlatformAppleTV,
			})
			Expect(err).To(HaveOccurred())
		})
	})

	When("platform is visionOS", func() {
		const searchPage = `<html><script type="application/json" id="serialized-server-data">{
			"data":[{"data":{"shelves":[{"items":[
				{"$kind":"EditorialItem","lockup":{"adamId":"999"},"purchaseConfiguration":{"metricsPlatformDisplayStyle":"vision","appPlatforms":["vision"],"buyParams":"salableAdamId=999&appExtVrsId=9990"}},
				{"$kind":"AppSearchResult","lockup":{"adamId":"200","bundleId":"fallback.two","title":"Fallback Two"},"purchaseConfiguration":{"metricsPlatformDisplayStyle":"vision","appPlatforms":["vision"],"buyParams":"salableAdamId=200&appExtVrsId=2000"}},
				{"$kind":"AppSearchResult","lockup":{"adamId":"300","bundleId":"wrong.platform","title":"Wrong Platform"},"purchaseConfiguration":{"metricsPlatformDisplayStyle":"ios","appPlatforms":["iphone"],"buyParams":"salableAdamId=300&appExtVrsId=3000"}},
				{"$kind":"AppSearchResult","lockup":{"adamId":"100","bundleId":"fallback.one","title":"Fallback One"},"purchaseConfiguration":{"metricsPlatformDisplayStyle":"vision","appPlatforms":["vision"],"buyParams":"salableAdamId=100&appExtVrsId=1000"}},
				{"$kind":"AppSearchResult","lockup":{"adamId":"400","bundleId":"limited.out","title":"Limited Out"},"purchaseConfiguration":{"metricsPlatformDisplayStyle":"vision","appPlatforms":["vision"],"buyParams":"salableAdamId=400&appExtVrsId=4000"}}
			] }]}}]}
		</script></html>`

		BeforeEach(func() {
			mockStorefrontClient.EXPECT().
				Send(gomock.Any()).
				Do(func(req http.Request) {
					parsedURL, err := url.Parse(req.URL)
					Expect(err).ToNot(HaveOccurred())
					Expect(parsedURL.Host).To(Equal("apps.apple.com"))
					Expect(parsedURL.Path).To(Equal("/us/vision/search"))
					Expect(parsedURL.Query().Get("term")).To(Equal("spatial notes"))
					Expect(req.Method).To(Equal(http.MethodGET))
					Expect(req.ResponseFormat).To(Equal(http.ResponseFormatRaw))
				}).
				Return(http.Result[[]byte]{StatusCode: 200, Data: []byte(searchPage)}, nil)

			mockClient.EXPECT().
				Send(gomock.Any()).
				Do(func(req http.Request) {
					parsedURL, err := url.Parse(req.URL)
					Expect(err).ToNot(HaveOccurred())
					Expect(parsedURL.Path).To(Equal("/lookup"))
					Expect(parsedURL.Query().Get("country")).To(Equal("US"))
					Expect(parsedURL.Query().Get("entity")).To(Equal("xrosSoftware"))
					Expect(parsedURL.Query().Get("id")).To(Equal("200,100"))
				}).
				Return(http.Result[searchResult]{
					StatusCode: 200,
					Data: searchResult{
						Count: 2,
						Results: []App{
							{ID: 100, BundleID: "hydrated.one", Name: "Hydrated One", Version: "1.0"},
							{ID: 200, BundleID: "hydrated.two", Name: "Hydrated Two", Version: "2.0"},
						},
					},
				}, nil)
		})

		It("filters native results, applies the limit, hydrates metadata, and preserves storefront order", func() {
			out, err := as.Search(SearchInput{
				Account:  Account{StoreFront: "143441"},
				Term:     "spatial notes",
				Limit:    2,
				Platform: PlatformVisionOS,
			})

			Expect(err).ToNot(HaveOccurred())
			Expect(out.Count).To(Equal(2))
			Expect(out.Results).To(Equal([]App{
				{ID: 200, BundleID: "hydrated.two", Name: "Hydrated Two", Version: "2.0", Platforms: []Platform{PlatformVisionOS}},
				{ID: 100, BundleID: "hydrated.one", Name: "Hydrated One", Version: "1.0", Platforms: []Platform{PlatformVisionOS}},
			}))
		})
	})

	DescribeTable("preserves confirmed visionOS support",
		func(metadata []App, expected []Platform) {
			page := `<script type="application/json" id="serialized-server-data">{"data":[{"data":{"shelves":[{"items":[{"$kind":"AppSearchResult","lockup":{"adamId":"42"},"purchaseConfiguration":{"metricsPlatformDisplayStyle":"vision","appPlatforms":["vision"],"buyParams":"salableAdamId=42&appExtVrsId=420"}}]}]}}]}</script>`
			mockStorefrontClient.EXPECT().Send(gomock.Any()).Return(http.Result[[]byte]{StatusCode: 200, Data: []byte(page)}, nil)
			mockClient.EXPECT().Send(gomock.Any()).Return(http.Result[searchResult]{StatusCode: 200, Data: searchResult{Results: metadata}}, nil)
			out, err := as.Search(SearchInput{Account: Account{StoreFront: "143441"}, Platform: PlatformVisionOS, Limit: 1})
			Expect(err).ToNot(HaveOccurred())
			Expect(out.Results).To(HaveLen(1))
			Expect(out.Results[0].Platforms).To(Equal(expected))
		},
		Entry("missing lookup result", nil, []Platform{PlatformVisionOS}),
		Entry("unknown lookup platforms", []App{{ID: 42, Platforms: []Platform{PlatformUnknown}}}, []Platform{PlatformVisionOS}),
		Entry("additional lookup platforms", []App{{ID: 42, Platforms: []Platform{PlatformIPhone, PlatformIPad}}}, []Platform{PlatformIPhone, PlatformIPad, PlatformVisionOS}),
		Entry("vision already present", []App{{ID: 42, Platforms: []Platform{PlatformVisionOS}}}, []Platform{PlatformVisionOS}),
	)

	When("store front is invalid", func() {
		It("returns error", func() {
			_, err := as.Search(SearchInput{
				Account: Account{
					StoreFront: "xyz",
				},
			})
			Expect(err).To(HaveOccurred())
		})
	})

	When("request fails", func() {
		BeforeEach(func() {
			mockClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[searchResult]{}, errors.New(""))
		})

		It("returns error", func() {
			_, err := as.Search(SearchInput{
				Account: Account{
					StoreFront: "143441",
				},
			})
			Expect(err).To(HaveOccurred())
		})
	})

	When("request returns bad status code", func() {
		BeforeEach(func() {
			mockClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[searchResult]{
					StatusCode: 400,
				}, nil)
		})

		It("returns error", func() {
			_, err := as.Search(SearchInput{
				Account: Account{
					StoreFront: "143441",
				},
			})
			Expect(err).To(HaveOccurred())
		})
	})
})
