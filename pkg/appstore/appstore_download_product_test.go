package appstore

import (
	"errors"
	"fmt"
	gohttp "net/http"
	"net/url"
	"strconv"

	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

const (
	testRedownloadEndpoint = "https://downloaddispatch.itunes.apple.com/r/redownload"
	testUpdateEndpoint     = "https://downloaddispatch.itunes.apple.com/up/updateProduct"
)

var _ = Describe("AppStore (Download Product)", func() {
	const (
		testGUID      = "001122334455"
		testVersionID = "123456789"
	)

	var (
		ctrl               *gomock.Controller
		mockBagClient      *http.MockClient[bagResult]
		mockDownloadClient *http.MockClient[downloadResult]
		mockPlatformClient *http.MockClient[platformVersionLookupResult]
		store              *appstore
		account            Account
		app                App
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockBagClient = http.NewMockClient[bagResult](ctrl)
		mockDownloadClient = http.NewMockClient[downloadResult](ctrl)
		mockPlatformClient = http.NewMockClient[platformVersionLookupResult](ctrl)
		store = &appstore{
			bagClient:      mockBagClient,
			downloadClient: mockDownloadClient,
			platformClient: mockPlatformClient,
		}
		account = Account{
			DirectoryServicesID: "test-dsid",
			Pod:                 "42",
		}
		app = App{ID: 987654321}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("uses volumeStore as the primary endpoint", func() {
		expected := http.Result[downloadResult]{
			StatusCode: gohttp.StatusOK,
			Data: downloadResult{
				Items: []downloadItemResult{{URL: "https://example.com/app.ipa"}},
			},
		}

		mockDownloadClient.EXPECT().
			Send(gomock.Any()).
			Do(func(req http.Request) {
				Expect(req.URL).To(Equal("https://p42-buy.itunes.apple.com/WebObjects/MZFinance.woa/wa/volumeStoreDownloadProduct?guid=" + testGUID))
				Expect(req.Headers).To(HaveKeyWithValue("iCloud-DSID", account.DirectoryServicesID))

				payload, ok := req.Payload.(*http.XMLPayload)
				Expect(ok).To(BeTrue())
				Expect(payload.Content).To(HaveKeyWithValue("externalVersionId", testVersionID))
				Expect(payload.Content).To(HaveKeyWithValue("serialNumber", "0"))
				Expect(payload.Content).ToNot(HaveKey("appExtVrsId"))
			}).
			Return(expected, nil)

		actual, platform, err := store.sendDownloadProduct(account, app, testGUID, testVersionID, PlatformIPhone)
		Expect(err).ToNot(HaveOccurred())
		Expect(actual).To(Equal(expected))
		Expect(platform).To(Equal(PlatformIPhone))
	})

	It("does not fall back for an App Store error response", func() {
		expected := http.Result[downloadResult]{
			StatusCode: gohttp.StatusOK,
			Data: downloadResult{
				FailureType:     FailureTypeLicenseNotFound,
				CustomerMessage: "License not found",
			},
		}

		mockDownloadClient.EXPECT().
			Send(gomock.Any()).
			Return(expected, nil)

		actual, _, err := store.sendDownloadProduct(account, app, testGUID, testVersionID, PlatformIPhone)
		Expect(err).ToNot(HaveOccurred())
		Expect(actual).To(Equal(expected))
	})

	It("falls back to redownloadProduct without requiring SAP configuration", func() {
		primary := http.Result[downloadResult]{StatusCode: gohttp.StatusOK}
		expected := http.Result[downloadResult]{
			StatusCode: gohttp.StatusOK,
			Data: downloadResult{
				Items: []downloadItemResult{{URL: "https://example.com/redownload.ipa"}},
			},
		}
		bag := bagResult{}
		bag.URLBag.RedownloadEndpoint = testRedownloadEndpoint

		gomock.InOrder(
			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(primary, nil),
			mockBagClient.EXPECT().
				Send(gomock.Any()).
				Do(func(req http.Request) {
					Expect(req.URL).To(Equal("https://init.itunes.apple.com/bag.xml?guid=" + testGUID))
				}).
				Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: bag}, nil),
			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Do(func(req http.Request) {
					Expect(req.URL).To(Equal(testRedownloadEndpoint + "?guid=" + testGUID))

					payload, ok := req.Payload.(*http.XMLPayload)
					Expect(ok).To(BeTrue())
					Expect(payload.Content).To(HaveKeyWithValue("appExtVrsId", testVersionID))
					Expect(payload.Content).To(HaveKeyWithValue("serialNumber", "0"))
					Expect(payload.Content).ToNot(HaveKey("externalVersionId"))
				}).
				Return(expected, nil),
		)

		actual, _, err := store.sendDownloadProduct(account, app, testGUID, testVersionID, PlatformIPhone)
		Expect(err).ToNot(HaveOccurred())
		Expect(actual).To(Equal(expected))
	})

	It("preserves the empty response when the bag has no redownload endpoint", func() {
		expected := http.Result[downloadResult]{StatusCode: gohttp.StatusOK}

		mockDownloadClient.EXPECT().
			Send(gomock.Any()).
			Return(expected, nil)
		mockBagClient.EXPECT().
			Send(gomock.Any()).
			Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: validBagResult()}, nil)

		actual, _, err := store.sendDownloadProduct(account, app, testGUID, testVersionID, PlatformIPhone)
		Expect(err).ToNot(HaveOccurred())
		Expect(actual).To(Equal(expected))
	})

	It("returns an error when the fallback bag request fails", func() {
		mockDownloadClient.EXPECT().
			Send(gomock.Any()).
			Return(http.Result[downloadResult]{StatusCode: gohttp.StatusOK}, nil)
		mockBagClient.EXPECT().
			Send(gomock.Any()).
			Return(http.Result[bagResult]{}, errors.New("bag request failed"))

		_, _, err := store.sendDownloadProduct(account, app, testGUID, testVersionID, PlatformIPhone)
		Expect(err).To(MatchError(ContainSubstring("failed to get bag for redownload fallback")))
	})

	It("returns an error when the redownload request fails", func() {
		bag := validBagResult()
		bag.URLBag.RedownloadEndpoint = testRedownloadEndpoint

		gomock.InOrder(
			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{StatusCode: gohttp.StatusOK}, nil),
			mockBagClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: bag}, nil),
			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{}, errors.New("redownload failed")),
		)

		_, _, err := store.sendDownloadProduct(account, app, testGUID, testVersionID, PlatformIPhone)
		Expect(err).To(MatchError(ContainSubstring("failed to send redownload request")))
	})

	DescribeTable("preserves platform selection on the normal volumeStore path",
		func(platform Platform) {
			expected := http.Result[downloadResult]{
				StatusCode: gohttp.StatusOK,
				Data:       downloadResult{Items: []downloadItemResult{{URL: "https://example.com/app.ipa"}}},
			}
			mockDownloadClient.EXPECT().Send(gomock.Any()).Return(expected, nil)

			actual, resolvedPlatform, err := store.sendDownloadProduct(account, app, testGUID, testVersionID, platform)
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).To(Equal(expected))
			Expect(resolvedPlatform).To(Equal(platform))
		},
		Entry("unspecified", Platform("")),
		Entry("iPhone", PlatformIPhone),
		Entry("iPad", PlatformIPad),
		Entry("macOS", PlatformMacOS),
	)

	Describe("version-pinned redownload", func() {
		var latestVersion platformVersionLookupResult

		BeforeEach(func() {
			account.StoreFront = "143441-1,34"
			latestVersion = platformVersionLookupResult{
				Results: map[string]platformVersionLookupItem{
					strconv.FormatInt(app.ID, 10): {
						Offers: []platformVersionLookupOffer{
							{Version: platformVersionLookupVersion{ExternalID: platformVersionExternalID(testVersionID)}},
						},
					},
				},
			}
			bag := validBagResult()
			bag.URLBag.RedownloadEndpoint = testRedownloadEndpoint
			first := mockDownloadClient.EXPECT().Send(gomock.Any()).
				Return(http.Result[downloadResult]{StatusCode: gohttp.StatusOK}, nil)
			mockBagClient.EXPECT().Send(gomock.Any()).After(first).
				Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: bag}, nil)
		})

		DescribeTable("pins the latest iOS version before the first redownload",
			func(platform Platform, storefront, country string) {
				account.StoreFront = storefront
				expected := http.Result[downloadResult]{
					StatusCode: gohttp.StatusOK,
					Data: downloadResult{Items: []downloadItemResult{{
						URL: "https://example.com/ios.ipa",
						Metadata: map[string]interface{}{
							"softwareVersionExternalIdentifiers": []interface{}{"123", testVersionID},
						},
					}}},
				}
				gomock.InOrder(
					mockPlatformClient.EXPECT().Send(gomock.Any()).
						Do(func(req http.Request) {
							u, err := url.Parse(req.URL)
							Expect(err).ToNot(HaveOccurred())
							Expect(u.Host).To(Equal("uclient-api.itunes.apple.com"))
							Expect(u.Query().Get("id")).To(Equal(strconv.FormatInt(app.ID, 10)))
							Expect(u.Query().Get("cc")).To(Equal(country))
							Expect(u.Query().Get("platform")).To(Equal("enterprisestore"))
						}).
						Return(http.Result[platformVersionLookupResult]{StatusCode: gohttp.StatusOK, Data: latestVersion}, nil),
					mockDownloadClient.EXPECT().Send(gomock.Any()).
						Do(func(req http.Request) {
							Expect(req.URL).To(Equal(testRedownloadEndpoint + "?guid=" + testGUID))
							Expect(req.Method).To(Equal(http.MethodPOST))
							payload := req.Payload.(*http.XMLPayload).Content
							Expect(payload).To(HaveKeyWithValue("salableAdamId", app.ID))
							Expect(payload).To(HaveKeyWithValue("guid", testGUID))
							Expect(payload).To(HaveKeyWithValue("appExtVrsId", testVersionID))
							Expect(payload).ToNot(HaveKey("externalVersionId"))
							Expect(payload).ToNot(HaveKey("pricingParameters"))
						}).Return(expected, nil),
				)
				actual, resolvedPlatform, err := store.sendDownloadProduct(account, app, testGUID, "", platform)
				Expect(err).ToNot(HaveOccurred())
				Expect(actual).To(Equal(expected))
				if platform == "" {
					Expect(resolvedPlatform).To(Equal(PlatformIPhone))
				} else {
					Expect(resolvedPlatform).To(Equal(platform))
				}
			},
			Entry("unspecified platform", Platform(""), "143441-1,34", "us"),
			Entry("iPhone", PlatformIPhone, "143441-1,34", "us"),
			Entry("iPad", PlatformIPad, "143441-1,34", "us"),
			Entry("account storefront", PlatformIPhone, "143444-2,29", "gb"),
		)

		DescribeTable("does not retry or remove the version pin after a redownload error",
			func(original error) {
				gomock.InOrder(
					mockPlatformClient.EXPECT().Send(gomock.Any()).
						Return(http.Result[platformVersionLookupResult]{StatusCode: gohttp.StatusOK, Data: latestVersion}, nil),
					mockDownloadClient.EXPECT().Send(gomock.Any()).
						Do(func(req http.Request) {
							Expect(req.Payload.(*http.XMLPayload).Content).To(HaveKeyWithValue("appExtVrsId", testVersionID))
						}).Return(http.Result[downloadResult]{}, original),
				)
				_, _, err := store.sendDownloadProduct(account, app, testGUID, "", PlatformIPhone)
				Expect(errors.Is(err, original)).To(BeTrue())
			},
			Entry("empty HTTP 500", fmt.Errorf("wrapped: %w", &http.UnexpectedResponseError{StatusCode: gohttp.StatusInternalServerError})),
			Entry("authentication", &http.UnexpectedResponseError{StatusCode: gohttp.StatusForbidden}),
			Entry("rate limit", &http.UnexpectedResponseError{StatusCode: gohttp.StatusTooManyRequests}),
			Entry("service unavailable", &http.UnexpectedResponseError{StatusCode: gohttp.StatusServiceUnavailable}),
			Entry("nonempty 500 message", &http.UnexpectedResponseError{StatusCode: gohttp.StatusInternalServerError, Snippet: "maintenance"}),
			Entry("network failure", errors.New("connection reset")),
		)

		DescribeTable("does not resolve an iOS version for pinned or other-platform requests",
			func(versionID string, platform Platform) {
				expected := http.Result[downloadResult]{
					StatusCode: gohttp.StatusOK,
					Data:       downloadResult{Items: []downloadItemResult{{URL: "https://example.com/app.ipa"}}},
				}
				mockDownloadClient.EXPECT().Send(gomock.Any()).
					Do(func(req http.Request) {
						payload := req.Payload.(*http.XMLPayload).Content
						if versionID == "" {
							Expect(payload).ToNot(HaveKey("appExtVrsId"))
						} else {
							Expect(payload).To(HaveKeyWithValue("appExtVrsId", versionID))
						}
					}).Return(expected, nil)
				actual, resolvedPlatform, err := store.sendDownloadProduct(account, app, testGUID, versionID, platform)
				Expect(err).ToNot(HaveOccurred())
				Expect(actual).To(Equal(expected))
				Expect(resolvedPlatform).To(Equal(platform))
			},
			Entry("explicit iOS version", testVersionID, PlatformIPhone),
			Entry("explicit version without a platform", "818970197", Platform("")),
			Entry("explicit tvOS version", "818970197", PlatformAppleTV),
			Entry("explicit macOS version", testVersionID, PlatformMacOS),
			Entry("tvOS", "", PlatformAppleTV),
			Entry("visionOS", "", PlatformVisionOS),
		)

		It("preserves an actionable failure from the pinned request", func() {
			expected := http.Result[downloadResult]{StatusCode: gohttp.StatusOK,
				Data: downloadResult{FailureType: FailureTypeLicenseNotFound}}
			gomock.InOrder(
				mockPlatformClient.EXPECT().Send(gomock.Any()).
					Return(http.Result[platformVersionLookupResult]{StatusCode: gohttp.StatusOK, Data: latestVersion}, nil),
				mockDownloadClient.EXPECT().Send(gomock.Any()).Return(expected, nil),
			)
			actual, _, err := store.sendDownloadProduct(account, app, testGUID, "", PlatformIPhone)
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).To(Equal(expected))
		})

		It("stops before redownload when catalog lookup fails", func() {
			lookupErr := errors.New("catalog unavailable")
			mockPlatformClient.EXPECT().Send(gomock.Any()).Return(http.Result[platformVersionLookupResult]{}, lookupErr)

			actual, _, err := store.sendDownloadProduct(account, app, testGUID, "", PlatformIPhone)
			Expect(isEmptyDownloadProductResponse(actual)).To(BeTrue())
			Expect(errors.Is(err, lookupErr)).To(BeTrue())
			Expect(err).To(MatchError(ContainSubstring("failed to resolve latest iOS version for redownload")))
		})

		It("does not send an unpinned request when the app is absent from the catalog", func() {
			mockPlatformClient.EXPECT().Send(gomock.Any()).
				Return(http.Result[platformVersionLookupResult]{StatusCode: gohttp.StatusOK}, nil)

			_, _, err := store.sendDownloadProduct(account, app, testGUID, "", PlatformIPhone)
			Expect(err).To(MatchError(ContainSubstring("platform version lookup returned no app")))
		})

		It("does not send an unpinned request when the catalog version is missing", func() {
			latestVersion.Results[strconv.FormatInt(app.ID, 10)] = platformVersionLookupItem{
				Offers: []platformVersionLookupOffer{{}},
			}
			mockPlatformClient.EXPECT().Send(gomock.Any()).
				Return(http.Result[platformVersionLookupResult]{StatusCode: gohttp.StatusOK, Data: latestVersion}, nil)

			_, _, err := store.sendDownloadProduct(account, app, testGUID, "", PlatformIPhone)
			Expect(err).To(MatchError(ContainSubstring("platform version lookup returned no external version id")))
		})

		It("does not retry a plist error response", func() {
			expected := http.Result[downloadResult]{StatusCode: gohttp.StatusInternalServerError}
			gomock.InOrder(
				mockPlatformClient.EXPECT().Send(gomock.Any()).
					Return(http.Result[platformVersionLookupResult]{StatusCode: gohttp.StatusOK, Data: latestVersion}, nil),
				mockDownloadClient.EXPECT().Send(gomock.Any()).Return(expected, nil),
			)
			actual, _, err := store.sendDownloadProduct(account, app, testGUID, "", PlatformIPhone)
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).To(Equal(expected))
		})
	})

})

var _ = Describe("AppStore (Download Customer Messages)", func() {
	DescribeTable("preserves Apple's message and response metadata when fallback is unavailable",
		func(call func(*appstore) error) {
			ctrl := gomock.NewController(GinkgoT())
			defer ctrl.Finish()
			mockMachine := machine.NewMockMachine(ctrl)
			mockDownloadClient := http.NewMockClient[downloadResult](ctrl)
			mockBagClient := http.NewMockClient[bagResult](ctrl)
			store := &appstore{machine: mockMachine, downloadClient: mockDownloadClient, bagClient: mockBagClient}
			response := http.Result[downloadResult]{StatusCode: gohttp.StatusOK,
				Data: downloadResult{CustomerMessage: "“Messenger” No Longer Available"}}

			mockMachine.EXPECT().MacAddress().Return("00:11:22:33:44:55", nil)
			mockDownloadClient.EXPECT().Send(gomock.Any()).Return(response, nil)
			mockBagClient.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{
				StatusCode: gohttp.StatusOK, Data: validBagResult(),
			}, nil)

			err := call(store)
			Expect(err).To(MatchError("received error: " + response.Data.CustomerMessage))
			Expect(err).To(BeAssignableToTypeOf(&Error{}))
			Expect(err.(*Error).Metadata).To(Equal(response))
		},
		Entry("download", func(store *appstore) error {
			_, err := store.Download(DownloadInput{})

			return err
		}),
		Entry("version list", func(store *appstore) error {
			_, err := store.ListVersions(ListVersionsInput{})

			return err
		}),
		Entry("version metadata", func(store *appstore) error {
			_, err := store.GetVersionMetadata(GetVersionMetadataInput{})

			return err
		}),
	)
})

var _ = Describe("AppStore (Download Endpoints)", func() {
	for _, path := range []string{redownloadProductPath, updateProductPath} {
		endpoint := "https://downloaddispatch.itunes.apple.com" + path
		DescribeTable(path,
			func(value string, valid bool) {
				_, err := newDownloadEndpoint(value, path)
				if valid {
					Expect(err).ToNot(HaveOccurred())
				} else {
					Expect(err).To(MatchError("invalid download endpoint in bag"))
				}
			},
			Entry("Apple endpoint", endpoint, true),
			Entry("empty", "", false),
			Entry("non-HTTPS", "http://downloaddispatch.itunes.apple.com"+path, false),
			Entry("foreign host", "https://example.com"+path, false),
			Entry("wrong path", endpoint+"/other", false),
			Entry("userinfo", "https://user@downloaddispatch.itunes.apple.com"+path, false),
			Entry("explicit port", "https://downloaddispatch.itunes.apple.com:443"+path, false),
			Entry("query", endpoint+"?guid=other", false),
			Entry("empty query", endpoint+"?", false),
			Entry("fragment", endpoint+"#fragment", false),
			Entry("escaped path", fmt.Sprintf("https://downloaddispatch.itunes.apple.com/%%%02x%s", path[1], path[2:]), false),
		)
	}
})
