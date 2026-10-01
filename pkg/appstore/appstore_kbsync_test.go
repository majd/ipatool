package appstore

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	gohttp "net/http"
	"time"

	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/keychain"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"howett.net/plist"
)

const testEntDownloadEndpoint = "https://downloaddispatch.itunes.apple.com/WebObjects/DownloadDispatch.woa/wa/ent/download"

var _ = Describe("AppStore (preferred kbsync download)", func() {
	const guid = "001122334455"
	const versionID = "123456789"
	var (
		store                       *appstore
		downloads                   *http.MockClient[downloadResult]
		bags                        *http.MockClient[bagResult]
		platforms                   *http.MockClient[platformVersionLookupResult]
		account                     Account
		app                         App
		bag                         bagResult
		empty, unavailable, success http.Result[downloadResult]
		generated                   int
	)
	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		downloads = http.NewMockClient[downloadResult](ctrl)
		bags = http.NewMockClient[bagResult](ctrl)
		platforms = http.NewMockClient[platformVersionLookupResult](ctrl)
		generated = 0
		store = &appstore{
			downloadClient: downloads, bagClient: bags, platformClient: platforms,
			kbsyncGenerator: func(ctx context.Context, hardwareID []byte, dsid uint64) ([]byte, error) {
				Expect(ctx.Err()).NotTo(HaveOccurred())
				deadline, ok := ctx.Deadline()
				Expect(ok).To(BeTrue())
				Expect(time.Until(deadline)).To(BeNumerically("<=", 2*time.Minute))
				Expect(hardwareID).To(Equal([]byte{0, 0x11, 0x22, 0x33, 0x44, 0x55}))
				Expect(dsid).To(Equal(uint64(123456789012)))
				generated++

				return []byte("kbsync blob"), nil
			},
		}
		account = Account{DirectoryServicesID: "123456789012", PasswordToken: "test-token", StoreFront: "143441-1,29"}
		app = App{ID: 42, BundleID: "com.example.app"}
		bag = bagResult{URLBag: urlBag{EntDownloadEndpoint: testEntDownloadEndpoint}}
		empty = http.Result[downloadResult]{StatusCode: gohttp.StatusOK}
		unavailable = http.Result[downloadResult]{StatusCode: gohttp.StatusOK, Data: downloadResult{CustomerMessage: "App No Longer Available"}}
		success = http.Result[downloadResult]{StatusCode: gohttp.StatusOK, Data: downloadResult{Items: []downloadItemResult{{
			URL:      "https://example.com/app.ipa",
			Metadata: map[string]interface{}{"itemId": int64(42), "softwareVersionExternalIdentifier": versionID, "softwareVersionBundleId": app.BundleID},
		}}}}
	})

	It("sends kbsync to the full ent endpoint advertised in Apple's bag", func() {
		var decoded bagResult
		_, err := plist.Unmarshal([]byte(`<?xml version="1.0"?><plist version="1.0"><dict><key>urlBag</key><dict><key>volumeStoreDownloadProduct</key><string>`+testEntDownloadEndpoint+`</string></dict></dict></plist>`), &decoded)
		Expect(err).NotTo(HaveOccurred())
		Expect(decoded.URLBag.EntDownloadEndpoint).To(Equal(testEntDownloadEndpoint))
		gomock.InOrder(
			bags.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: decoded}, nil),
			downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
				Expect(req.URL).To(Equal(testEntDownloadEndpoint + "?guid=" + guid))
				Expect(req.Payload.(*http.XMLPayload).Content).To(HaveKeyWithValue("kbsync", base64.StdEncoding.EncodeToString([]byte("kbsync blob"))))
			}).Return(success, nil),
		)
		actual, _, err := store.sendDownloadProduct(context.Background(), account, app, guid, versionID, PlatformIPhone)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual).To(Equal(success))
		Expect(generated).To(Equal(1))
	})

	It("enables kbsync generation for normal clients", func() {
		Expect(NewAppStore(Args{}).(*appstore).kbsyncGenerator).NotTo(BeNil())
	})

	It("uses volumeStore when the bag omits ent/download", func() {
		bag.URLBag.EntDownloadEndpoint = ""
		gomock.InOrder(
			bags.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: bag}, nil),
			downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
				Expect(req.URL).To(ContainSubstring("volumeStoreDownloadProduct"))
			}).Return(success, nil),
		)
		actual, _, err := store.sendDownloadProduct(context.Background(), account, app, guid, versionID, PlatformIPhone)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual).To(Equal(success))
		Expect(generated).To(BeZero())
	})

	It("keeps volumeStore usable when the initial bag request fails", func() {
		gomock.InOrder(
			bags.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{}, errors.New("bag unavailable")),
			downloads.EXPECT().Send(gomock.Any()).Return(success, nil),
		)
		actual, _, err := store.sendDownloadProduct(context.Background(), account, app, guid, versionID, PlatformIPhone)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual).To(Equal(success))
		Expect(generated).To(BeZero())
	})

	It("falls back when generation is unavailable", func() {
		store.kbsyncGenerator = func(context.Context, []byte, uint64) ([]byte, error) {
			return nil, errors.New("guest unavailable")
		}
		gomock.InOrder(
			bags.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: bag}, nil),
			downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
				Expect(req.URL).To(ContainSubstring("volumeStoreDownloadProduct"))
			}).Return(success, nil),
		)
		actual, _, err := store.sendDownloadProduct(context.Background(), account, app, guid, versionID, PlatformIPhone)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual).To(Equal(success))
	})

	DescribeTable("falls back from unusable ent/download responses", func(outcome string) {
		response := empty
		var responseErr error
		switch outcome {
		case "unavailable":
			response = unavailable
		case "HTML 403":
			responseErr = &http.UnexpectedResponseError{StatusCode: gohttp.StatusForbidden}
		case "HTTP 500":
			responseErr = &http.UnexpectedResponseError{StatusCode: gohttp.StatusInternalServerError}
		case "timeout":
			responseErr = &http.TransportError{Err: context.DeadlineExceeded}
		case "wrong app", "wrong version", "missing URL":
			response = http.Result[downloadResult]{StatusCode: gohttp.StatusOK, Data: downloadResult{Items: []downloadItemResult{{
				URL: "https://example.com/wrong.ipa", Metadata: map[string]interface{}{
					"itemId": int64(42), "softwareVersionExternalIdentifier": versionID, "softwareVersionBundleId": app.BundleID,
				},
			}}}}
			switch outcome {
			case "wrong app":
				response.Data.Items[0].Metadata["itemId"] = int64(99)
			case "wrong version":
				response.Data.Items[0].Metadata["softwareVersionExternalIdentifier"] = "other"
			default:
				response.Data.Items[0].URL = ""
			}
		}

		gomock.InOrder(
			bags.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: bag}, nil),
			downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
				Expect(req.URL).To(Equal(testEntDownloadEndpoint + "?guid=" + guid))
			}).Return(response, responseErr),
			downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
				Expect(req.URL).To(ContainSubstring("volumeStoreDownloadProduct"))
				Expect(req.Payload.(*http.XMLPayload).Content).To(HaveKeyWithValue("externalVersionId", versionID))
			}).Return(success, nil),
		)
		actual, _, err := store.sendDownloadProduct(context.Background(), account, app, guid, versionID, PlatformIPhone)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual).To(Equal(success))
		Expect(generated).To(Equal(1))
	},
		Entry("empty song list", "empty"), Entry("unavailable", "unavailable"), Entry("HTML 403", "HTML 403"),
		Entry("HTTP 500", "HTTP 500"), Entry("timeout", "timeout"), Entry("wrong app", "wrong app"),
		Entry("wrong version", "wrong version"), Entry("missing package URL", "missing URL"),
	)

	It("retains the complete legacy chain and reuses the bag after an ent/download miss", func() {
		bag.URLBag.RedownloadEndpoint = testRedownloadEndpoint
		bag.URLBag.UpdateEndpoint = testUpdateEndpoint
		gomock.InOrder(
			bags.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: bag}, nil),
			downloads.EXPECT().Send(gomock.Any()).Return(unavailable, nil),
			downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
				Expect(req.URL).To(ContainSubstring("volumeStoreDownloadProduct"))
			}).Return(empty, nil),
			downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
				Expect(req.URL).To(Equal(testRedownloadEndpoint + "?guid=" + guid))
			}).Return(unavailable, nil),
			downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
				Expect(req.URL).To(Equal(testUpdateEndpoint + "?guid=" + guid))
			}).Return(success, nil),
		)
		actual, _, err := store.sendDownloadProduct(context.Background(), account, app, guid, versionID, PlatformIPhone)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual).To(Equal(success))
		Expect(generated).To(Equal(1))
	})

	DescribeTable("tries ent/download first for a pinned version",
		func(platform Platform) {
			fetched := bags.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: bag}, nil)
			downloads.EXPECT().Send(gomock.Any()).After(fetched).Do(func(req http.Request) {
				Expect(generated).To(Equal(1))
				Expect(req.URL).To(Equal(testEntDownloadEndpoint + "?guid=" + guid))
				Expect(req.Method).To(Equal(http.MethodPOST))
				Expect(req.NoRedirects).To(BeTrue())
				Expect(req.Context.Err()).NotTo(HaveOccurred())
				deadline, ok := req.Context.Deadline()
				Expect(ok).To(BeTrue())
				Expect(time.Until(deadline)).To(BeNumerically("<=", 30*time.Second))
				Expect(req.Headers).To(HaveKeyWithValue("X-Token", account.PasswordToken))
				Expect(req.Headers).To(HaveKeyWithValue("X-Dsid", account.DirectoryServicesID))
				Expect(req.Headers).To(HaveKeyWithValue("iCloud-DSID", account.DirectoryServicesID))
				Expect(req.Headers).To(HaveKeyWithValue("X-Apple-Store-Front", account.StoreFront))
				Expect(req.Headers).To(HaveKeyWithValue("Content-Type", "application/x-www-form-urlencoded; charset=utf-8"))
				Expect(req.Headers["User-Agent"]).To(HavePrefix("Configurator/2.18 "))
				Expect(req.Payload.(*http.XMLPayload).Content).To(Equal(map[string]interface{}{
					"creditDisplay": "", "guid": guid, "salableAdamId": "42", "externalVersionId": versionID,
					"serialNumber": base64.StdEncoding.EncodeToString([]byte{0x54, 0xc8, 0xb0, 0xa9, 0x88, 0x22, 0x33, 0x44, 0x55}),
					"kbsync":       base64.StdEncoding.EncodeToString([]byte("kbsync blob")),
				}))
			}).Return(success, nil)
			actual, resolved, err := store.sendDownloadProduct(context.Background(), account, app, guid, versionID, platform)
			Expect(err).NotTo(HaveOccurred())
			Expect(actual).To(Equal(success))
			Expect(resolved).To(Equal(platform))
		},
		Entry("iPhone", PlatformIPhone), Entry("iPad", PlatformIPad), Entry("macOS", PlatformMacOS),
		Entry("tvOS", PlatformAppleTV), Entry("visionOS", PlatformVisionOS),
	)

	It("pins the iOS offer before trying ent/download", func() {
		gomock.InOrder(
			bags.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: bag}, nil),
			platforms.EXPECT().Send(gomock.Any()).Return(http.Result[platformVersionLookupResult]{StatusCode: gohttp.StatusOK, Data: platformVersionLookupResult{
				Results: map[string]platformVersionLookupItem{"42": {Offers: []platformVersionLookupOffer{{Version: platformVersionLookupVersion{ExternalID: platformVersionExternalID(versionID)}}}}},
			}}, nil),
			downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
				Expect(req.Payload.(*http.XMLPayload).Content).To(HaveKeyWithValue("externalVersionId", versionID))
			}).Return(success, nil),
		)
		actual, platform, err := store.sendDownloadProduct(context.Background(), account, app, guid, "", "")
		Expect(err).NotTo(HaveOccurred())
		Expect(actual).To(Equal(success))
		Expect(platform).To(Equal(PlatformIPhone))
	})

	It("pins the native Mac offer before trying ent/download for a universal app", func() {
		pages := http.NewMockClient[[]byte](gomock.NewController(GinkgoT()))
		store.storefrontClient = pages
		account.StoreFront = "143443-2,34"
		app = App{ID: 6472431552, BundleID: "com.nebula.karing"}
		success.Data.Items[0].Metadata = map[string]interface{}{
			"itemId": app.ID, "softwareVersionExternalIdentifier": "876660716", "softwareVersionBundleId": app.BundleID,
		}
		gomock.InOrder(
			pages.EXPECT().Send(gomock.Any()).Return(http.Result[[]byte]{StatusCode: gohttp.StatusOK, Data: macVersionPage(karingMacConfiguration)}, nil),
			bags.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: bag}, nil),
			downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
				Expect(req.URL).To(Equal(testEntDownloadEndpoint + "?guid=" + guid))
				Expect(req.Payload.(*http.XMLPayload).Content).To(HaveKeyWithValue("externalVersionId", "876660716"))
			}).Return(success, nil),
		)
		actual, platform, err := store.sendDownloadProduct(context.Background(), account, app, guid, "", PlatformMacOS)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual).To(Equal(success))
		Expect(platform).To(Equal(PlatformMacOS))
	})

	It("uses ent/download when listing versions", func() {
		device := machine.NewMockMachine(gomock.NewController(GinkgoT()))
		store.machine = device
		device.EXPECT().MacAddress().Return("00:11:22:33:44:55", nil)
		success.Data.Items[0].Metadata["softwareVersionExternalIdentifiers"] = []interface{}{"123456788", versionID}
		gomock.InOrder(
			bags.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: bag}, nil),
			platforms.EXPECT().Send(gomock.Any()).Return(http.Result[platformVersionLookupResult]{StatusCode: gohttp.StatusOK, Data: platformVersionLookupResult{
				Results: map[string]platformVersionLookupItem{"42": {Offers: []platformVersionLookupOffer{{Version: platformVersionLookupVersion{ExternalID: platformVersionExternalID(versionID)}}}}},
			}}, nil),
			downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
				Expect(req.URL).To(Equal(testEntDownloadEndpoint + "?guid=" + guid))
			}).Return(success, nil),
		)
		result, err := store.ListVersions(ListVersionsInput{Account: account, App: app})
		Expect(err).NotTo(HaveOccurred())
		Expect(result.ExternalVersionIdentifiers).To(Equal([]string{"123456788", versionID}))
		Expect(result.LatestExternalVersionID).To(Equal(versionID))
	})

	DescribeTable("rejects invalid bag destinations before generating or sending credentials", func(endpoint string) {
		bag.URLBag.EntDownloadEndpoint = endpoint
		_, _, err := store.sendPreferredDownload(context.Background(), bag.URLBag.EntDownloadEndpoint, account, app, guid, versionID, PlatformIPhone)
		Expect(err).To(MatchError("invalid download endpoint in bag"))
		Expect(generated).To(BeZero())
	},
		Entry("HTTP", "http://downloaddispatch.itunes.apple.com/WebObjects/DownloadDispatch.woa/wa/ent/download"),
		Entry("foreign host", "https://example.com/WebObjects/DownloadDispatch.woa/wa/ent/download"),
		Entry("lookalike host", "https://downloaddispatch.itunes.apple.com.example.com/WebObjects/DownloadDispatch.woa/wa/ent/download"),
		Entry("credentials", "https://user@downloaddispatch.itunes.apple.com/WebObjects/DownloadDispatch.woa/wa/ent/download"),
		Entry("query", testEntDownloadEndpoint+"?guid=other"), Entry("fragment", testEntDownloadEndpoint+"#fragment"),
		Entry("wrong path", testUpdateEndpoint),
		Entry("abbreviated path", "https://downloaddispatch.itunes.apple.com/ent/download"),
		Entry("encoded path", "https://downloaddispatch.itunes.apple.com/WebObjects/DownloadDispatch.woa/wa/ent/%64ownload"),
	)

	DescribeTable("validates the app, version and bundle before accepting ent/download", func(field string, value interface{}, expected string) {
		success.Data.Items[0].Metadata[field] = value
		downloads.EXPECT().Send(gomock.Any()).Return(success, nil)
		_, _, err := store.sendPreferredDownload(context.Background(), bag.URLBag.EntDownloadEndpoint, account, app, guid, versionID, PlatformIPhone)
		Expect(err).To(MatchError(ContainSubstring(expected)))
	},
		Entry("wrong app", "itemId", int64(99), "requested app or version"),
		Entry("wrong version", "softwareVersionExternalIdentifier", "987654321", "requested app or version"),
		Entry("wrong bundle", "softwareVersionBundleId", "com.other.app", "requested bundle identifier"),
		Entry("missing bundle", "softwareVersionBundleId", nil, "requested bundle identifier"),
	)

	DescribeTable("preserves authentication and license failures from the fallback", func(failure string) {
		response := http.Result[downloadResult]{StatusCode: gohttp.StatusOK, Data: downloadResult{FailureType: failure}}
		bags.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: bag}, nil)
		gomock.InOrder(
			downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
				Expect(req.URL).To(Equal(testEntDownloadEndpoint + "?guid=" + guid))
			}).Return(response, nil),
			downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
				Expect(req.URL).To(ContainSubstring("volumeStoreDownloadProduct"))
			}).Return(response, nil),
		)
		actual, _, err := store.sendDownloadProduct(context.Background(), account, app, guid, versionID, PlatformIPhone)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual).To(Equal(response))
	}, Entry("expired token", FailureTypePasswordTokenExpired), Entry("missing license", FailureTypeLicenseNotFound))

	It("stops before generating when canceled", func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, _, err := store.sendDownloadProduct(ctx, account, app, guid, versionID, PlatformIPhone)
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		Expect(generated).To(BeZero())
	})

	It("reports generation failures without sending the token", func() {
		failure := errors.New("guest failed")
		store.kbsyncGenerator = func(context.Context, []byte, uint64) ([]byte, error) { return nil, failure }
		_, _, err := store.sendPreferredDownload(context.Background(), bag.URLBag.EntDownloadEndpoint, account, app, guid, versionID, PlatformIPhone)
		Expect(errors.Is(err, failure)).To(BeTrue())
	})

	It("does not start the legacy chain after cancellation during generation", func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		bags.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: bag}, nil)
		store.kbsyncGenerator = func(context.Context, []byte, uint64) ([]byte, error) {
			cancel()

			return nil, context.Canceled
		}
		_, _, err := store.sendDownloadProduct(ctx, account, app, guid, versionID, PlatformIPhone)
		Expect(errors.Is(err, context.Canceled)).To(BeTrue())
	})

	It("preserves unpinned legacy downloads when the preferred offer lookup fails", func() {
		gomock.InOrder(
			bags.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: bag}, nil),
			platforms.EXPECT().Send(gomock.Any()).Return(http.Result[platformVersionLookupResult]{}, errors.New("catalog unavailable")),
			downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
				Expect(req.URL).To(ContainSubstring("volumeStoreDownloadProduct"))
				Expect(req.Payload.(*http.XMLPayload).Content).NotTo(HaveKey("externalVersionId"))
			}).Return(success, nil),
		)
		actual, _, err := store.sendDownloadProduct(context.Background(), account, app, guid, "", PlatformIPhone)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual).To(Equal(success))
		Expect(generated).To(BeZero())
	})

	It("does not send credentials to an invalid preferred endpoint and falls back", func() {
		bag.URLBag.EntDownloadEndpoint = "https://example.com/WebObjects/DownloadDispatch.woa/wa/ent/download"
		gomock.InOrder(
			bags.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: bag}, nil),
			downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
				Expect(req.URL).To(ContainSubstring("volumeStoreDownloadProduct"))
			}).Return(success, nil),
		)
		actual, _, err := store.sendDownloadProduct(context.Background(), account, app, guid, versionID, PlatformIPhone)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual).To(Equal(success))
		Expect(generated).To(BeZero())
	})

	Describe("retrying cached kbsync", func() {
		const dsid = uint64(123456789012)
		var (
			stored                []byte
			cachedBlob, freshBlob string
			removed, saved        int
			removeErr             error
			request               func(string) *gomock.Call
			failure               func(string) (http.Result[downloadResult], error)
		)
		BeforeEach(func() {
			cachedBlob = base64.StdEncoding.EncodeToString([]byte("cached blob"))
			freshBlob = base64.StdEncoding.EncodeToString([]byte("kbsync blob"))
			var err error
			stored, err = json.Marshal(kbsyncCacheEntry{DSID: dsid, GUID: guid, Data: cachedBlob})
			Expect(err).NotTo(HaveOccurred())
			removed, saved = 0, 0
			removeErr = nil
			chain := keychain.NewMockKeychain(gomock.NewController(GinkgoT()))
			store.keychain = chain
			chain.EXPECT().Get(kbsyncCacheKey).DoAndReturn(func(string) ([]byte, error) {
				return bytes.Clone(stored), nil
			}).AnyTimes()
			chain.EXPECT().Remove(kbsyncCacheKey).DoAndReturn(func(string) error {
				removed++
				if removeErr != nil {
					return removeErr
				}
				stored = nil

				return nil
			}).AnyTimes()
			chain.EXPECT().Set(kbsyncCacheKey, gomock.Any()).DoAndReturn(func(_ string, data []byte) error {
				saved++
				stored = bytes.Clone(data)

				return nil
			}).AnyTimes()
			bags.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: bag}, nil).AnyTimes()
			request = func(blob string) *gomock.Call {
				return downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
					Expect(req.URL).To(Equal(testEntDownloadEndpoint + "?guid=" + guid))
					Expect(req.NoRedirects).To(BeTrue())
					Expect(req.Context.Err()).NotTo(HaveOccurred())
					Expect(req.Headers).To(HaveKeyWithValue("X-Token", account.PasswordToken))
					payload := req.Payload.(*http.XMLPayload).Content
					Expect(payload).To(HaveKeyWithValue("kbsync", blob))
					Expect(payload).To(HaveKeyWithValue("externalVersionId", versionID))
				})
			}
			failure = func(outcome string) (http.Result[downloadResult], error) {
				switch outcome {
				case "HTTP 500":
					return http.Result[downloadResult]{StatusCode: gohttp.StatusInternalServerError}, nil
				case "HTML 403":
					return http.Result[downloadResult]{}, &http.UnexpectedResponseError{StatusCode: gohttp.StatusForbidden}
				case "timeout":
					return http.Result[downloadResult]{}, &http.TransportError{Err: context.DeadlineExceeded}
				case "unavailable":
					return unavailable, nil
				case "structured failure":
					return http.Result[downloadResult]{StatusCode: gohttp.StatusOK, Data: downloadResult{FailureType: FailureTypePasswordTokenExpired}}, nil
				case "missing URL", "wrong version":
					response := http.Result[downloadResult]{StatusCode: gohttp.StatusOK, Data: downloadResult{Items: []downloadItemResult{{
						Metadata: map[string]interface{}{"itemId": app.ID, "softwareVersionExternalIdentifier": versionID, "softwareVersionBundleId": app.BundleID},
					}}}}
					if outcome == "wrong version" {
						response.Data.Items[0].URL = "https://example.com/wrong.ipa"
						response.Data.Items[0].Metadata["softwareVersionExternalIdentifier"] = "other"
					}

					return response, nil
				default:
					return empty, nil
				}
			}
		})

		It("uses a working persistent blob without generating or rewriting it", func() {
			request(cachedBlob).Return(success, nil)
			actual, _, err := store.sendDownloadProduct(context.Background(), account, app, guid, versionID, PlatformIPhone)
			Expect(err).NotTo(HaveOccurred())
			Expect(actual).To(Equal(success))
			Expect(generated).To(BeZero())
			Expect(removed).To(BeZero())
			Expect(saved).To(BeZero())
		})

		It("saves an uncached blob only after success and reuses it in a new client", func() {
			stored = nil
			request(freshBlob).Do(func(http.Request) {
				Expect(stored).To(BeNil())
				Expect(saved).To(BeZero())
			}).Return(success, nil)
			_, _, err := store.sendDownloadProduct(context.Background(), account, app, guid, versionID, PlatformIPhone)
			Expect(err).NotTo(HaveOccurred())
			Expect(saved).To(Equal(1))
			restarted := &appstore{
				keychain: store.keychain, downloadClient: downloads, bagClient: bags,
				kbsyncGenerator: func(context.Context, []byte, uint64) ([]byte, error) {
					Fail("the new client must reuse the successful blob")

					return nil, nil
				},
			}
			request(freshBlob).Return(success, nil)
			_, _, err = restarted.sendDownloadProduct(context.Background(), account, app, guid, versionID, PlatformIPhone)
			Expect(err).NotTo(HaveOccurred())
			Expect(generated).To(Equal(1))
			Expect(saved).To(Equal(1))
		})

		DescribeTable("retries a failed cached blob and saves a successful replacement", func(outcome string) {
			response, responseErr := failure(outcome)
			var firstContext context.Context
			gomock.InOrder(
				request(cachedBlob).Do(func(req http.Request) { firstContext = req.Context }).Return(response, responseErr),
				request(freshBlob).Do(func(req http.Request) {
					Expect(removed).To(Equal(1))
					Expect(stored).To(BeNil())
					Expect(saved).To(BeZero())
					Expect(firstContext.Err()).To(HaveOccurred())
					Expect(req.Context).NotTo(Equal(firstContext))
					Expect(store.cachedKBSync(dsid, guid)).To(BeEmpty())
				}).Return(success, nil),
			)
			actual, _, err := store.sendDownloadProduct(context.Background(), account, app, guid, versionID, PlatformIPhone)
			Expect(err).NotTo(HaveOccurred())
			Expect(actual).To(Equal(success))
			Expect(generated).To(Equal(1))
			Expect(saved).To(Equal(1))
			var entry kbsyncCacheEntry
			Expect(json.Unmarshal(stored, &entry)).To(Succeed())
			Expect(entry).To(Equal(kbsyncCacheEntry{DSID: dsid, GUID: guid, Data: freshBlob}))
			Expect(store.cachedKBSync(dsid, guid)).To(Equal(freshBlob))
			request(freshBlob).Return(success, nil)
			_, _, err = store.sendDownloadProduct(context.Background(), account, app, guid, versionID, PlatformIPhone)
			Expect(err).NotTo(HaveOccurred())
			Expect(generated).To(Equal(1))
			Expect(saved).To(Equal(1))
		},
			Entry("server error", "HTTP 500"), Entry("non-plist error", "HTML 403"), Entry("request timeout", "timeout"),
			Entry("empty response", "empty"), Entry("availability error", "unavailable"), Entry("Apple failure", "structured failure"),
			Entry("missing URL", "missing URL"), Entry("wrong version", "wrong version"),
		)

		DescribeTable("falls back without caching or retrying a failed fresh blob", func(outcome string, hasCache bool) {
			if hasCache {
				request(cachedBlob).Return(empty, nil)
			} else {
				stored = nil
			}
			response, responseErr := failure(outcome)
			gomock.InOrder(
				request(freshBlob).Return(response, responseErr),
				downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
					Expect(req.URL).To(ContainSubstring("volumeStoreDownloadProduct"))
				}).Return(success, nil),
			)
			actual, _, err := store.sendDownloadProduct(context.Background(), account, app, guid, versionID, PlatformIPhone)
			Expect(err).NotTo(HaveOccurred())
			Expect(actual).To(Equal(success))
			Expect(generated).To(Equal(1))
			Expect(saved).To(BeZero())
			Expect(stored).To(BeNil())
			Expect(store.cachedKBSync(dsid, guid)).To(BeEmpty())
		},
			Entry("server error", "HTTP 500", true), Entry("non-plist error", "HTML 403", true), Entry("timeout", "timeout", true),
			Entry("empty response", "empty", true), Entry("availability error", "unavailable", true),
			Entry("Apple failure", "structured failure", true), Entry("missing URL", "missing URL", true), Entry("wrong version", "wrong version", true),
			Entry("no existing cache", "HTTP 500", false), Entry("uncached Apple failure", "structured failure", false),
		)

		It("falls back when regeneration fails", func() {
			store.kbsyncGenerator = func(context.Context, []byte, uint64) ([]byte, error) {
				Expect(removed).To(Equal(1))

				return nil, errors.New("guest failed")
			}
			gomock.InOrder(
				request(cachedBlob).Return(empty, nil),
				downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
					Expect(req.URL).To(ContainSubstring("volumeStoreDownloadProduct"))
				}).Return(success, nil),
			)
			actual, _, err := store.sendDownloadProduct(context.Background(), account, app, guid, versionID, PlatformIPhone)
			Expect(err).NotTo(HaveOccurred())
			Expect(actual).To(Equal(success))
			Expect(saved).To(BeZero())
			Expect(stored).To(BeNil())
		})

		It("bypasses the old blob even if its keychain entry cannot be removed", func() {
			removeErr = errors.New("keychain unavailable")
			gomock.InOrder(request(cachedBlob).Return(empty, nil), request(freshBlob).Return(success, nil))
			actual, _, err := store.sendDownloadProduct(context.Background(), account, app, guid, versionID, PlatformIPhone)
			Expect(err).NotTo(HaveOccurred())
			Expect(actual).To(Equal(success))
			Expect(generated).To(Equal(1))
			Expect(removed).To(Equal(1))
			Expect(saved).To(Equal(1))
			Expect(store.cachedKBSync(dsid, guid)).To(Equal(freshBlob))
		})

		It("does not regenerate or fall back when the cached request is canceled", func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			request(cachedBlob).Do(func(http.Request) { cancel() }).Return(http.Result[downloadResult]{}, context.Canceled)
			_, _, err := store.sendDownloadProduct(ctx, account, app, guid, versionID, PlatformIPhone)
			Expect(errors.Is(err, context.Canceled)).To(BeTrue())
			Expect(generated).To(BeZero())
			Expect(removed).To(BeZero())
			Expect(saved).To(BeZero())
		})

		It("does not use the cached blob when already canceled", func() {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, _, err := store.sendPreferredDownload(ctx, testEntDownloadEndpoint, account, app, guid, versionID, PlatformIPhone)
			Expect(errors.Is(err, context.Canceled)).To(BeTrue())
			Expect(generated).To(BeZero())
			Expect(removed).To(BeZero())
		})

		It("stops without saving or falling back when regeneration is canceled", func() {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			request(cachedBlob).Return(empty, nil)
			store.kbsyncGenerator = func(context.Context, []byte, uint64) ([]byte, error) {
				cancel()

				return []byte("canceled blob"), nil
			}
			_, _, err := store.sendDownloadProduct(ctx, account, app, guid, versionID, PlatformIPhone)
			Expect(errors.Is(err, context.Canceled)).To(BeTrue())
			Expect(removed).To(Equal(1))
			Expect(saved).To(BeZero())
			Expect(stored).To(BeNil())
		})
	})
})
