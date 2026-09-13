package appstore

import (
	"errors"
	"fmt"
	gohttp "net/http"
	"strconv"

	"github.com/majd/ipatool/v2/pkg/http"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("AppStore (Update Product)", func() {
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
		bag                bagResult
		expected           http.Result[downloadResult]
		redownloadErr      error
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockBagClient = http.NewMockClient[bagResult](ctrl)
		mockDownloadClient = http.NewMockClient[downloadResult](ctrl)
		mockPlatformClient = http.NewMockClient[platformVersionLookupResult](ctrl)
		store = &appstore{bagClient: mockBagClient, downloadClient: mockDownloadClient, platformClient: mockPlatformClient}
		account = Account{DirectoryServicesID: "test-dsid", Pod: "42", StoreFront: "143441-1,34"}
		app = App{ID: 987654321, BundleID: "com.example.app"}
		bag = bagResult{}
		bag.URLBag.RedownloadEndpoint = testRedownloadEndpoint
		bag.URLBag.UpdateEndpoint = testUpdateEndpoint
		redownloadErr = fmt.Errorf("wrapped: %w", &http.UnexpectedResponseError{StatusCode: gohttp.StatusInternalServerError})
		expected = http.Result[downloadResult]{
			StatusCode: gohttp.StatusOK,
			Data: downloadResult{Items: []downloadItemResult{{
				URL: "https://example.com/ios.ipa",
				Metadata: map[string]interface{}{
					"itemId":                            uint64(app.ID),
					"softwareVersionExternalIdentifier": uint64(123456789),
					"softwareVersionBundleId":           app.BundleID,
				},
			}}},
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	expectPrimary := func() *gomock.Call {
		first := mockDownloadClient.EXPECT().Send(gomock.Any()).
			Return(http.Result[downloadResult]{StatusCode: gohttp.StatusOK}, nil)

		return mockBagClient.EXPECT().Send(gomock.Any()).After(first).
			Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: bag}, nil)
	}

	DescribeTable("retries once with the same pinned request",
		func(versionID string, platform Platform) {
			previous := expectPrimary()
			if versionID == "" {
				previous = mockPlatformClient.EXPECT().Send(gomock.Any()).After(previous).
					Return(http.Result[platformVersionLookupResult]{
						StatusCode: gohttp.StatusOK,
						Data: platformVersionLookupResult{Results: map[string]platformVersionLookupItem{
							strconv.FormatInt(app.ID, 10): {Offers: []platformVersionLookupOffer{
								{Version: platformVersionLookupVersion{ExternalID: platformVersionExternalID(testVersionID)}},
							}},
						}},
					}, nil)
			}

			var redownloadRequest http.Request

			gomock.InOrder(
				mockDownloadClient.EXPECT().Send(gomock.Any()).After(previous).
					Do(func(req http.Request) {
						Expect(req.URL).To(Equal(testRedownloadEndpoint + "?guid=" + testGUID))
						redownloadRequest = req
					}).Return(http.Result[downloadResult]{}, redownloadErr),
				mockDownloadClient.EXPECT().Send(gomock.Any()).
					Do(func(req http.Request) {
						Expect(req.URL).To(Equal(testUpdateEndpoint + "?guid=" + testGUID))
						Expect(req.Method).To(Equal(http.MethodPOST))
						Expect(req.Headers).To(Equal(redownloadRequest.Headers))
						Expect(req.Headers).To(HaveKeyWithValue("Content-Type", "application/x-apple-plist"))
						Expect(req.Payload).To(Equal(redownloadRequest.Payload))
						Expect(req.Payload.(*http.XMLPayload).Content).To(HaveKeyWithValue("appExtVrsId", testVersionID))
					}).Return(expected, nil),
			)

			actual, resolvedPlatform, err := store.sendDownloadProduct(account, app, testGUID, versionID, platform)
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).To(Equal(expected))
			if platform == "" {
				Expect(resolvedPlatform).To(Equal(PlatformIPhone))
			} else {
				Expect(resolvedPlatform).To(Equal(platform))
			}
		},
		Entry("default iOS", "", Platform("")),
		Entry("latest iPhone", "", PlatformIPhone),
		Entry("latest iPad", "", PlatformIPad),
		Entry("explicit historical version", testVersionID, PlatformIPhone),
		Entry("explicit macOS version", testVersionID, PlatformMacOS),
		Entry("explicit version without a platform", testVersionID, Platform("")),
	)

	DescribeTable("tries update once for a message-only availability response",
		func(primaryUnavailable, updateUnavailable bool) {
			unavailable := http.Result[downloadResult]{StatusCode: gohttp.StatusOK,
				Data: downloadResult{CustomerMessage: "“Messenger” No Longer Available"}}
			primary := http.Result[downloadResult]{StatusCode: gohttp.StatusOK}
			if primaryUnavailable {
				primary = unavailable
			}
			update := expected
			if updateUnavailable {
				update = unavailable
			}

			gomock.InOrder(
				mockDownloadClient.EXPECT().Send(gomock.Any()).Return(primary, nil),
				mockBagClient.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: bag}, nil),
				mockDownloadClient.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
					Expect(req.URL).To(Equal(testRedownloadEndpoint + "?guid=" + testGUID))
					Expect(req.Payload.(*http.XMLPayload).Content).To(HaveKeyWithValue("appExtVrsId", testVersionID))
				}).Return(unavailable, nil),
				mockDownloadClient.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
					Expect(req.URL).To(Equal(testUpdateEndpoint + "?guid=" + testGUID))
					Expect(req.Payload.(*http.XMLPayload).Content).To(HaveKeyWithValue("appExtVrsId", testVersionID))
				}).Return(update, nil),
			)

			actual, _, err := store.sendDownloadProduct(account, app, testGUID, testVersionID, PlatformIPhone)
			Expect(actual).To(Equal(update))
			if updateUnavailable {
				Expect(err).To(MatchError(ContainSubstring(unavailable.Data.CustomerMessage)))
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
		},
		Entry("redownload unavailable", false, false),
		Entry("both initial endpoints unavailable", true, false),
		Entry("update also unavailable", false, true),
	)

	DescribeTable("keeps TestFlight's Mac offer pinned through update fallback",
		func(versionID string, emptyRedownload bool, updateUnavailable bool) {
			app = App{ID: 899247664, BundleID: "com.apple.TestFlight"}
			expected.Data.Items[0].URL = "https://example.com/TestFlight.pkg"
			expected.Data.Items[0].Metadata["itemId"] = uint64(app.ID)
			expected.Data.Items[0].Metadata["softwareVersionBundleId"] = app.BundleID
			expected.Data.Items[0].Metadata["software-platform"] = "macos"
			if versionID == "" {
				pages := http.NewMockClient[[]byte](ctrl)
				store.storefrontClient = pages
				pages.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
					Expect(req.URL).To(Equal("https://apps.apple.com/us/app/id899247664?platform=mac"))
				}).Return(http.Result[[]byte]{StatusCode: gohttp.StatusOK, Data: macVersionPage(
					`{"purchaseConfiguration":{"bundleId":"com.apple.TestFlight","appPlatforms":["mac"],"buyParams":"salableAdamId=899247664&appExtVrsId=123456789"}}`,
				)}, nil)
			}
			unavailable := http.Result[downloadResult]{StatusCode: gohttp.StatusOK,
				Data: downloadResult{CustomerMessage: "“TestFlight” No Longer Available"}}
			redownload := unavailable
			var requestErr error
			if emptyRedownload {
				redownload = http.Result[downloadResult]{}
				requestErr = redownloadErr
			}
			update := expected
			if updateUnavailable {
				update = unavailable
			}
			var redownloadRequest http.Request
			gomock.InOrder(
				mockDownloadClient.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
					Expect(req.Payload.(*http.XMLPayload).Content).To(HaveKeyWithValue("externalVersionId", testVersionID))
				}).Return(unavailable, nil),
				mockBagClient.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: bag}, nil),
				mockDownloadClient.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
					Expect(req.URL).To(Equal(testRedownloadEndpoint + "?guid=" + testGUID))
					redownloadRequest = req
				}).Return(redownload, requestErr),
				mockDownloadClient.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
					Expect(req.URL).To(Equal(testUpdateEndpoint + "?guid=" + testGUID))
					Expect(req.Headers).To(Equal(redownloadRequest.Headers))
					Expect(req.Payload).To(Equal(redownloadRequest.Payload))
					Expect(req.Payload.(*http.XMLPayload).Content).To(HaveKeyWithValue("appExtVrsId", testVersionID))
				}).Return(update, nil),
			)
			actual, platform, err := store.sendDownloadProduct(account, app, testGUID, versionID, PlatformMacOS)
			Expect(platform).To(Equal(PlatformMacOS))
			Expect(actual).To(Equal(update))
			if updateUnavailable {
				Expect(err).To(MatchError(ContainSubstring(unavailable.Data.CustomerMessage)))
			} else {
				Expect(err).ToNot(HaveOccurred())
			}
		},
		Entry("latest Mac offer after availability errors", "", false, false),
		Entry("explicit Mac version after availability errors", testVersionID, false, false),
		Entry("latest Mac offer after empty HTTP 500", "", true, false),
		Entry("update also unavailable", "", false, true),
	)

	DescribeTable("preserves availability responses when update is ineligible",
		func(platform Platform, updateEndpoint, failureType, message string, status int) {
			bag.URLBag.UpdateEndpoint = updateEndpoint
			previous := expectPrimary()
			response := http.Result[downloadResult]{StatusCode: status,
				Data: downloadResult{FailureType: failureType, CustomerMessage: message}}
			mockDownloadClient.EXPECT().Send(gomock.Any()).After(previous).Return(response, nil)

			actual, _, err := store.sendDownloadProduct(account, app, testGUID, testVersionID, platform)
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).To(Equal(response))
		},
		Entry("missing endpoint", PlatformIPhone, "", "", "“Messenger” No Longer Available", 200),
		Entry("tvOS", PlatformAppleTV, testUpdateEndpoint, "", "“Messenger” No Longer Available", 200),
		Entry("visionOS", PlatformVisionOS, testUpdateEndpoint, "", "“Messenger” No Longer Available", 200),
		Entry("license failure", PlatformIPhone, testUpdateEndpoint, FailureTypeLicenseNotFound, "“Messenger” No Longer Available", 200),
		Entry("other message", PlatformIPhone, testUpdateEndpoint, "", "Sign in required", 200),
		Entry("HTTP failure", PlatformIPhone, testUpdateEndpoint, "", "“Messenger” No Longer Available", 403),
	)

	DescribeTable("preserves other redownload errors without retrying",
		func(original error) {
			previous := expectPrimary()
			mockDownloadClient.EXPECT().Send(gomock.Any()).After(previous).
				Return(http.Result[downloadResult]{}, original)

			_, _, err := store.sendDownloadProduct(account, app, testGUID, testVersionID, PlatformIPhone)
			Expect(errors.Is(err, original)).To(BeTrue())
		},
		Entry("authentication", &http.UnexpectedResponseError{StatusCode: gohttp.StatusForbidden}),
		Entry("rate limit", &http.UnexpectedResponseError{StatusCode: gohttp.StatusTooManyRequests}),
		Entry("service unavailable", &http.UnexpectedResponseError{StatusCode: gohttp.StatusServiceUnavailable}),
		Entry("nonempty 500", &http.UnexpectedResponseError{StatusCode: gohttp.StatusInternalServerError, Snippet: "maintenance"}),
		Entry("transport failure", errors.New("connection reset")),
	)

	DescribeTable("does not retry a decoded redownload response",
		func(response http.Result[downloadResult]) {
			previous := expectPrimary()
			mockDownloadClient.EXPECT().Send(gomock.Any()).After(previous).Return(response, nil)

			actual, _, err := store.sendDownloadProduct(account, app, testGUID, testVersionID, PlatformIPhone)
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).To(Equal(response))
		},
		Entry("successful download", http.Result[downloadResult]{StatusCode: gohttp.StatusOK,
			Data: downloadResult{Items: []downloadItemResult{{URL: "https://example.com/app.ipa"}}}}),
		Entry("license error", http.Result[downloadResult]{StatusCode: gohttp.StatusOK,
			Data: downloadResult{FailureType: FailureTypeLicenseNotFound}}),
		Entry("empty HTTP 200", http.Result[downloadResult]{StatusCode: gohttp.StatusOK}),
		Entry("decoded HTTP 500", http.Result[downloadResult]{StatusCode: gohttp.StatusInternalServerError}),
	)

	It("preserves the redownload error when the update endpoint is missing", func() {
		bag.URLBag.UpdateEndpoint = ""
		previous := expectPrimary()
		mockDownloadClient.EXPECT().Send(gomock.Any()).After(previous).
			Return(http.Result[downloadResult]{}, redownloadErr)

		_, _, err := store.sendDownloadProduct(account, app, testGUID, testVersionID, PlatformIPhone)
		Expect(errors.Is(err, redownloadErr)).To(BeTrue())
	})

	It("rejects an invalid bag endpoint before sending an update request", func() {
		bag.URLBag.UpdateEndpoint = "https://example.com/up/updateProduct"
		previous := expectPrimary()
		mockDownloadClient.EXPECT().Send(gomock.Any()).After(previous).
			Return(http.Result[downloadResult]{}, redownloadErr)

		_, _, err := store.sendDownloadProduct(account, app, testGUID, testVersionID, PlatformIPhone)
		Expect(err).To(MatchError("invalid download endpoint in bag"))
	})

	DescribeTable("does not extend the fallback to unverified platforms",
		func(versionID string, platform Platform) {
			previous := expectPrimary()
			mockDownloadClient.EXPECT().Send(gomock.Any()).After(previous).
				Return(http.Result[downloadResult]{}, redownloadErr)

			_, resolvedPlatform, err := store.sendDownloadProduct(account, app, testGUID, versionID, platform)
			Expect(errors.Is(err, redownloadErr)).To(BeTrue())
			Expect(resolvedPlatform).To(Equal(platform))
		},
		Entry("pinned tvOS", testVersionID, PlatformAppleTV),
		Entry("pinned visionOS", testVersionID, PlatformVisionOS),
	)

	It("propagates an update error without retrying again", func() {
		previous := expectPrimary()
		gomock.InOrder(
			mockDownloadClient.EXPECT().Send(gomock.Any()).After(previous).
				Return(http.Result[downloadResult]{}, redownloadErr),
			mockDownloadClient.EXPECT().Send(gomock.Any()).
				Return(http.Result[downloadResult]{}, redownloadErr),
		)

		_, _, err := store.sendDownloadProduct(account, app, testGUID, testVersionID, PlatformIPhone)
		Expect(errors.Is(err, redownloadErr)).To(BeTrue())
		Expect(err).To(MatchError(ContainSubstring("failed to send update request")))
	})

	DescribeTable("preserves structured update failures for the caller",
		func(failure string) {
			expected.Data = downloadResult{FailureType: failure, CustomerMessage: "App Store error"}
			mockDownloadClient.EXPECT().Send(gomock.Any()).Return(expected, nil)

			actual, err := store.sendUpdateProduct(testUpdateEndpoint, account, app, testGUID, testVersionID)
			Expect(err).ToNot(HaveOccurred())
			Expect(actual).To(Equal(expected))
		},
		Entry("license required", FailureTypeLicenseNotFound),
		Entry("expired session", FailureTypePasswordTokenExpired),
		Entry("sign in required", FailureTypeSignInRequired),
	)

	It("rejects a non-200 update response", func() {
		expected.StatusCode = gohttp.StatusInternalServerError
		mockDownloadClient.EXPECT().Send(gomock.Any()).Return(expected, nil)

		_, err := store.sendUpdateProduct(testUpdateEndpoint, account, app, testGUID, testVersionID)
		Expect(err).To(MatchError("received unexpected update status code: 500"))
	})

	It("does not accept items when an update response contains only a customer error", func() {
		expected.Data.CustomerMessage = "Unable to process your request"
		mockDownloadClient.EXPECT().Send(gomock.Any()).Return(expected, nil)

		_, err := store.sendUpdateProduct(testUpdateEndpoint, account, app, testGUID, testVersionID)
		Expect(err).To(MatchError("received update error: Unable to process your request"))
	})

	DescribeTable("requires exactly one update item",
		func(count int) {
			expected.Data.Items = make([]downloadItemResult, count)
			mockDownloadClient.EXPECT().Send(gomock.Any()).Return(expected, nil)

			_, err := store.sendUpdateProduct(testUpdateEndpoint, account, app, testGUID, testVersionID)
			Expect(err).To(MatchError("update response must contain exactly one item"))
		},
		Entry("empty", 0),
		Entry("ambiguous", 2),
	)

	DescribeTable("rejects mismatched or missing update identity",
		func(field string, value interface{}) {
			expected.Data.Items[0].Metadata[field] = value
			mockDownloadClient.EXPECT().Send(gomock.Any()).Return(expected, nil)

			_, err := store.sendUpdateProduct(testUpdateEndpoint, account, app, testGUID, testVersionID)
			Expect(err).To(MatchError(ContainSubstring("update response does not match")))
		},
		Entry("wrong app", "itemId", uint64(42)),
		Entry("missing app", "itemId", nil),
		Entry("different version", "softwareVersionExternalIdentifier", uint64(987654321)),
		Entry("missing version", "softwareVersionExternalIdentifier", nil),
		Entry("wrong bundle", "softwareVersionBundleId", "com.example.other"),
		Entry("missing bundle", "softwareVersionBundleId", nil),
		Entry("empty bundle", "softwareVersionBundleId", ""),
	)

	It("accepts string identifiers for an ID-only caller", func() {
		app.BundleID = ""
		expected.Data.Items[0].Metadata["itemId"] = strconv.FormatInt(app.ID, 10)
		expected.Data.Items[0].Metadata["softwareVersionExternalIdentifier"] = testVersionID
		mockDownloadClient.EXPECT().Send(gomock.Any()).Return(expected, nil)

		actual, err := store.sendUpdateProduct(testUpdateEndpoint, account, app, testGUID, testVersionID)
		Expect(err).ToNot(HaveOccurred())
		Expect(actual).To(Equal(expected))
	})

})
