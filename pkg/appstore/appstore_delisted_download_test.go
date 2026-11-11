package appstore

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	gohttp "net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"

	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	"github.com/majd/ipatool/v2/pkg/util/operatingsystem"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"howett.net/plist"
)

var _ = Describe("Delisted app kbsync recovery", func() {
	const guid = "001122334455"
	var (
		store      *appstore
		downloads  *http.MockClient[downloadResult]
		account    Account
		app        App
		redownload downloadProductEndpoint
		success    http.Result[downloadResult]
		forbidden  error
		generated  int
	)

	BeforeEach(func() {
		downloads = http.NewMockClient[downloadResult](gomock.NewController(GinkgoT()))
		generated = 0
		store = &appstore{
			downloadClient: downloads,
			kbsyncGenerator: func(ctx context.Context, hardwareID []byte, dsid uint64) ([]byte, error) {
				Expect(ctx.Err()).NotTo(HaveOccurred())
				deadline, ok := ctx.Deadline()
				Expect(ok).To(BeTrue())
				Expect(time.Until(deadline)).To(BeNumerically("<=", 2*time.Minute))
				Expect(hardwareID).To(Equal([]byte{0, 0x11, 0x22, 0x33, 0x44, 0x55}))
				Expect(dsid).To(Equal(uint64(1234)))
				generated++

				return []byte("kbsync blob"), nil
			},
		}
		account = Account{DirectoryServicesID: "1234", PasswordToken: "token", StoreFront: "143469-2,29"}
		app = App{ID: 42, BundleID: "com.example.app"}
		var err error
		redownload, err = newDownloadEndpoint(testRedownloadEndpoint, redownloadProductPath)
		Expect(err).NotTo(HaveOccurred())
		success = http.Result[downloadResult]{StatusCode: gohttp.StatusOK, Data: downloadResult{
			Items: []downloadItemResult{{URL: "https://example.test/app.ipa", Metadata: map[string]interface{}{
				"itemId": app.ID, "softwareVersionBundleId": app.BundleID,
				"softwareVersionExternalIdentifier": uint64(1001),
			}}},
		}}
		forbidden = &http.UnexpectedResponseError{StatusCode: gohttp.StatusForbidden}
	})

	expectRedownload := func(response http.Result[downloadResult], err error) *gomock.Call {
		return downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
			Expect(req.URL).To(Equal(testRedownloadEndpoint + "?guid=" + guid))
			Expect(req.Payload.(*http.XMLPayload).Content).NotTo(HaveKey("appExtVrsId"))
		}).Return(response, err)
	}

	expectCatalog404Recovery := func() *gomock.Call {
		ctrl := gomock.NewController(GinkgoT())
		bags := http.NewMockClient[bagResult](ctrl)
		catalogs := http.NewMockClient[platformVersionLookupResult](ctrl)
		store.bagClient = bags
		store.platformClient = catalogs
		store.storefrontCountryCodes = testStorefrontCountryCodes()
		last := bags.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{
			StatusCode: gohttp.StatusOK, Data: bagResult{URLBag: urlBag{
				EntDownloadEndpoint: testEntDownloadEndpoint, RedownloadEndpoint: testRedownloadEndpoint,
			}},
		}, nil)
		// The preferred request and legacy fallback both discover the missing offer.
		for attempt := 0; attempt < 2; attempt++ {
			for _, catalog := range []string{"enterprisestore", "iphone", "ipad"} {
				last = catalogs.EXPECT().Send(store.platformVersionLookupRequest(app.ID, "RU", catalog)).After(last).
					Return(http.Result[platformVersionLookupResult]{StatusCode: gohttp.StatusOK}, nil)
			}
			last = catalogs.EXPECT().Send(http.Request{
				URL:    "https://apps.apple.com/api/apps/v1/catalog/ru/apps/42?platform=iphone",
				Method: http.MethodGET, ResponseFormat: http.ResponseFormatJSON,
			}).After(last).Return(http.Result[platformVersionLookupResult]{StatusCode: gohttp.StatusNotFound}, nil)
			if attempt == 0 {
				last = downloads.EXPECT().Send(store.downloadProductRequest(store.volumeStoreEndpoint(account), account, app, guid, "")).After(last).
					Return(http.Result[downloadResult]{StatusCode: gohttp.StatusOK}, nil)
			}
		}

		return expectRedownload(http.Result[downloadResult]{}, forbidden).After(last)
	}

	It("recovers an opaque redownload 403 without consulting the catalog or pinning a version", func() {
		first := expectRedownload(http.Result[downloadResult]{}, forbidden)
		downloads.EXPECT().Send(gomock.Any()).After(first).Do(func(req http.Request) {
			Expect(req.URL).To(Equal(testEntDownloadEndpoint + "?guid=" + guid))
			Expect(req.NoRedirects).To(BeTrue())
			Expect(req.Headers).To(HaveKeyWithValue("X-Token", account.PasswordToken))
			Expect(req.Headers).To(HaveKeyWithValue("X-Apple-Store-Front", account.StoreFront))
			payload := req.Payload.(*http.XMLPayload).Content
			Expect(payload).To(HaveKey("kbsync"))
			Expect(payload).NotTo(HaveKey("externalVersionId"))
		}).Return(success, nil)

		actual, err := store.sendDelistedDownload(context.Background(), redownload, testEntDownloadEndpoint, account, app, guid)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual).To(Equal(success))
		Expect(generated).To(Equal(1))
	})

	It("keeps a successful redownload without generating kbsync", func() {
		expectRedownload(success, nil)
		actual, err := store.sendDelistedDownload(context.Background(), redownload, testEntDownloadEndpoint, account, app, guid)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual).To(Equal(success))
		Expect(generated).To(BeZero())
	})

	It("lists versions through the complete catalog-404 and redownload-403 recovery chain", func() {
		device := machine.NewMockMachine(gomock.NewController(GinkgoT()))
		device.EXPECT().MacAddress().Return("00:11:22:33:44:55", nil)
		store.machine = device
		success.Data.Items[0].Metadata["softwareVersionExternalIdentifiers"] = []interface{}{"1000", "1001"}
		last := expectCatalog404Recovery()
		downloads.EXPECT().Send(gomock.Any()).After(last).Return(success, nil)
		out, err := store.ListVersions(ListVersionsInput{Account: account, App: app})
		Expect(err).NotTo(HaveOccurred())
		Expect(out.ExternalVersionIdentifiers).To(Equal([]string{"1000", "1001"}))
		Expect(out.LatestExternalVersionID).To(Equal("1001"))
		Expect(generated).To(Equal(1))
	})

	DescribeTable("validates recovered packages before replacing the destination", func(supportedPlatform string) {
		device := machine.NewMockMachine(gomock.NewController(GinkgoT()))
		device.EXPECT().MacAddress().Return("00:11:22:33:44:55", nil)
		store.machine = device
		store.os = operatingsystem.New()
		store.httpClient = newDownloadTestClient()
		packageBuffer := new(bytes.Buffer)
		writer := zip.NewWriter(packageBuffer)
		infoWriter, err := writer.Create("Payload/Test.app/Info.plist")
		Expect(err).NotTo(HaveOccurred())
		info, err := plist.Marshal(map[string]interface{}{
			"CFBundleSupportedPlatforms": []string{supportedPlatform},
		}, plist.BinaryFormat)
		Expect(err).NotTo(HaveOccurred())
		_, err = infoWriter.Write(info)
		Expect(err).NotTo(HaveOccurred())
		Expect(writer.Close()).To(Succeed())
		server := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, _ *gohttp.Request) {
			_, _ = w.Write(packageBuffer.Bytes())
		}))
		DeferCleanup(server.Close)
		success.Data.Items[0].URL = server.URL + "/app.ipa"
		last := expectCatalog404Recovery()
		downloads.EXPECT().Send(gomock.Any()).After(last).Return(success, nil)
		destination := filepath.Join(GinkgoT().TempDir(), "app.ipa")
		Expect(os.WriteFile(destination, []byte("previous package"), 0600)).To(Succeed())
		out, err := store.Download(DownloadInput{Account: account, App: app, OutputPath: destination})
		if supportedPlatform == "iPhoneOS" {
			Expect(err).NotTo(HaveOccurred())
			Expect(out.DestinationPath).To(Equal(destination))
			Expect(store.validatePackagePlatform(destination, PlatformIPhone)).To(Succeed())
		} else {
			Expect(err).To(MatchError(ContainSubstring("does not declare iPhoneOS support")))
			Expect(os.ReadFile(destination)).To(Equal([]byte("previous package")))
		}
		Expect(destination + ".tmp").NotTo(BeAnExistingFile())
	},
		Entry("iOS", "iPhoneOS"),
		Entry("tvOS", "AppleTVOS"),
	)

	It("preserves authentication recovery without trying another endpoint", func() {
		expectRedownload(http.Result[downloadResult]{StatusCode: gohttp.StatusOK, Data: downloadResult{
			Dialog:  map[string]interface{}{"kind": "authorization"},
			Metrics: map[string]interface{}{"messageCode": uint64(2042)},
		}}, nil)
		actual, err := store.sendDelistedDownload(context.Background(), redownload, testEntDownloadEndpoint, account, app, guid)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual.Data.FailureType).To(Equal(FailureTypePasswordTokenExpired))
		Expect(generated).To(BeZero())
	})

	DescribeTable("preserves actionable errors from ent/download", func(failure, expected string) {
		first := expectRedownload(http.Result[downloadResult]{}, forbidden)
		downloads.EXPECT().Send(gomock.Any()).After(first).Return(http.Result[downloadResult]{
			StatusCode: gohttp.StatusOK, Data: downloadResult{FailureType: failure},
		}, nil)
		actual, err := store.sendDelistedDownload(context.Background(), redownload, testEntDownloadEndpoint, account, app, guid)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual.Data.FailureType).To(Equal(expected))
	},
		Entry("expired token", FailureTypePasswordTokenExpired, FailureTypePasswordTokenExpired),
		Entry("sign-in required", FailureTypeSignInRequired, FailureTypePasswordTokenExpired),
		Entry("missing license", FailureTypeLicenseNotFound, FailureTypeLicenseNotFound),
	)

	It("tries ent/download after an unavailable dialog and retains the missing-license result if it fails", func() {
		expectRedownload(http.Result[downloadResult]{StatusCode: gohttp.StatusOK, Data: downloadResult{
			CustomerMessage: "Redownload Unavailable with This Apple Account",
		}}, nil)
		downloads.EXPECT().Send(gomock.Any()).Return(http.Result[downloadResult]{}, forbidden)
		actual, err := store.sendDelistedDownload(context.Background(), redownload, testEntDownloadEndpoint, account, app, guid)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual.Data.FailureType).To(Equal(FailureTypeLicenseNotFound))
	})

	DescribeTable("retains the original error when kbsync recovery is unavailable", func(generatorAvailable bool, endpoint string) {
		if !generatorAvailable {
			store.kbsyncGenerator = nil
		}
		expectRedownload(http.Result[downloadResult]{}, forbidden)
		_, err := store.sendDelistedDownload(context.Background(), redownload, endpoint, account, app, guid)
		Expect(errors.Is(err, forbidden)).To(BeTrue())
		Expect(generated).To(BeZero())
	},
		Entry("no generator", false, testEntDownloadEndpoint),
		Entry("no endpoint", true, ""),
	)

	It("reports both endpoint failures and the missing catalog offer", func() {
		failed := errors.New("kbsync endpoint failed")
		first := expectRedownload(http.Result[downloadResult]{}, forbidden)
		downloads.EXPECT().Send(gomock.Any()).After(first).Return(http.Result[downloadResult]{}, failed)
		_, err := store.sendDelistedDownload(context.Background(), redownload, testEntDownloadEndpoint, account, app, guid)
		Expect(err).To(MatchError(ContainSubstring("app 42 has no offer")))
		Expect(errors.Is(err, forbidden)).To(BeTrue())
		Expect(errors.Is(err, failed)).To(BeTrue())
	})

	It("rejects an invalid ent/download destination before generating or sending credentials", func() {
		expectRedownload(http.Result[downloadResult]{}, forbidden)
		_, err := store.sendDelistedDownload(context.Background(), redownload, "https://example.test/ent/download", account, app, guid)
		Expect(err).To(MatchError(ContainSubstring("invalid download endpoint in bag")))
		Expect(generated).To(BeZero())
	})

	It("does not start kbsync recovery after cancellation", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		expectRedownload(http.Result[downloadResult]{}, forbidden).Do(func(http.Request) { cancel() })
		_, err := store.sendDelistedDownload(ctx, redownload, testEntDownloadEndpoint, account, app, guid)
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		Expect(generated).To(BeZero())
	})

	DescribeTable("validates the unpinned ent/download response", func(field string, value interface{}) {
		success.Data.Items[0].Metadata[field] = value
		first := expectRedownload(http.Result[downloadResult]{}, forbidden)
		downloads.EXPECT().Send(gomock.Any()).After(first).Return(success, nil)
		_, err := store.sendDelistedDownload(context.Background(), redownload, testEntDownloadEndpoint, account, app, guid)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring("does not match the requested")))
	},
		Entry("wrong app", "itemId", int64(43)),
		Entry("wrong bundle", "softwareVersionBundleId", "com.other.app"),
		Entry("missing bundle", "softwareVersionBundleId", nil),
	)
})

var _ = Describe("Download failure dialogs", func() {
	It("does not reinterpret successful items using metric codes", func() {
		result := downloadResult{
			Items:   []downloadItemResult{{URL: "https://example.test/app.ipa"}},
			Metrics: map[string]interface{}{"messageCode": uint64(0)},
		}
		Expect(normalizeDownloadFailure(result).FailureType).To(BeEmpty())
	})

	It("keeps authentication recovery when an error response also contains items", func() {
		result := downloadResult{FailureType: FailureTypeDeviceVerificationFailed, Items: []downloadItemResult{{}}}
		Expect(normalizeDownloadFailure(result).FailureType).To(Equal(FailureTypePasswordTokenExpired))
	})

	DescribeTable("normalizes plist dialogs and metric codes", func(body, expected string) {
		var result downloadResult
		_, err := plist.Unmarshal([]byte(`<plist version="1.0"><dict>`+body+`</dict></plist>`), &result)
		Expect(err).NotTo(HaveOccurred())
		Expect(normalizeDownloadFailure(result).FailureType).To(Equal(expected))
	},
		Entry("numeric sign-in metric", `<key>metrics</key><dict><key>messageCode</key><integer>2042</integer></dict>`, FailureTypePasswordTokenExpired),
		Entry("string expired-token metric", `<key>metrics</key><dict><key>messageCode</key><string>2034</string></dict>`, FailureTypePasswordTokenExpired),
		Entry("existing failure", `<key>failureType</key><string>9610</string><key>metrics</key><dict><key>messageCode</key><integer>2042</integer></dict>`, FailureTypeLicenseNotFound),
		Entry("sign-in message", `<key>customerMessage</key><string>Sign in to the iTunes Store</string>`, FailureTypePasswordTokenExpired),
		Entry("unavailable dialog with Apple spaces", `<key>dialog</key><dict><key>message</key><string>Redownload Unavailable with This Apple Account</string><key>explanation</key><string>This redownload is not available for this Apple Account either because it was bought by a different user or the item was refunded or cancelled.</string></dict>`, FailureTypeLicenseNotFound),
		Entry("unavailable with a metric code", `<key>customerMessage</key><string>Redownload Unavailable with This Apple Account</string><key>metrics</key><dict><key>messageCode</key><integer>2059</integer></dict>`, FailureTypeLicenseNotFound),
	)
})
