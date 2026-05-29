package appstore

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	gohttp "net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/keychain"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	"github.com/majd/ipatool/v2/pkg/util/operatingsystem"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"howett.net/plist"
)

type dummyFileInfo struct{}

func (d *dummyFileInfo) Name() string       { return "dummy" }
func (d *dummyFileInfo) Size() int64        { return 0 }
func (d *dummyFileInfo) Mode() fs.FileMode  { return 0 }
func (d *dummyFileInfo) ModTime() time.Time { return time.Time{} }
func (d *dummyFileInfo) IsDir() bool        { return false }
func (d *dummyFileInfo) Sys() interface{}   { return nil }

var _ = Describe("AppStore (Download)", func() {
	var (
		ctrl               *gomock.Controller
		mockKeychain       *keychain.MockKeychain
		mockDownloadClient *http.MockClient[downloadResult]
		mockPlatformClient *http.MockClient[platformVersionLookupResult]
		mockPurchaseClient *http.MockClient[purchaseResult]
		mockLoginClient    *http.MockClient[loginResult]
		mockHTTPClient     *http.MockClient[interface{}]
		mockOS             *operatingsystem.MockOperatingSystem
		mockMachine        *machine.MockMachine
		as                 AppStore
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockKeychain = keychain.NewMockKeychain(ctrl)
		mockDownloadClient = http.NewMockClient[downloadResult](ctrl)
		mockPlatformClient = http.NewMockClient[platformVersionLookupResult](ctrl)
		mockLoginClient = http.NewMockClient[loginResult](ctrl)
		mockPurchaseClient = http.NewMockClient[purchaseResult](ctrl)
		mockHTTPClient = http.NewMockClient[interface{}](ctrl)
		mockOS = operatingsystem.NewMockOperatingSystem(ctrl)
		mockMachine = machine.NewMockMachine(ctrl)
		as = &appstore{
			keychain:       mockKeychain,
			loginClient:    mockLoginClient,
			purchaseClient: mockPurchaseClient,
			downloadClient: mockDownloadClient,
			platformClient: mockPlatformClient,
			httpClient:     mockHTTPClient,
			machine:        mockMachine,
			os:             mockOS,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	When("fails to read MAC address", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("", errors.New(""))
		})

		It("returns error", func() {
			_, err := as.Download(DownloadInput{})
			Expect(err).To(HaveOccurred())
		})
	})

	When("request fails", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{}, errors.New(""))
		})

		It("returns error", func() {
			_, err := as.Download(DownloadInput{})
			Expect(err).To(HaveOccurred())
		})
	})

	When("request uses a custom pod", func() {
		const (
			testPod  = "42"
			testGUID = "001122334455"
		)

		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Do(func(req http.Request) {
					expectedURL := "https://p" + testPod + "-" + PrivateAppStoreAPIDomain + PrivateAppStoreAPIPathDownload + "?guid=" + testGUID
					Expect(req.URL).To(Equal(expectedURL))
				}).
				Return(http.Result[downloadResult]{}, errors.New(""))
		})

		It("sends the download request to the pod-specific host", func() {
			_, err := as.Download(DownloadInput{
				Account: Account{
					Pod: testPod,
				},
			})
			Expect(err).To(HaveOccurred())
		})
	})

	When("platform is AppleTV", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)

			mockPlatformClient.EXPECT().
				Send(gomock.Any()).
				Do(func(req http.Request) {
					parsedURL, err := url.Parse(req.URL)
					Expect(err).ToNot(HaveOccurred())
					Expect(parsedURL.Host).To(Equal("uclient-api.itunes.apple.com"))
					Expect(parsedURL.Query().Get("platform")).To(Equal("atv9"))
					Expect(parsedURL.Query().Get("cc")).To(Equal("us"))
				}).
				Return(http.Result[platformVersionLookupResult]{
					StatusCode: 200,
					Data: platformVersionLookupResult{
						Results: map[string]platformVersionLookupItem{
							"42": {
								Offers: []platformVersionLookupOffer{
									{
										Version: platformVersionLookupVersion{
											ExternalID: platformVersionExternalID("123456"),
										},
									},
								},
							},
						},
					},
				}, nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Do(func(req http.Request) {
					payload, ok := req.Payload.(*http.XMLPayload)
					Expect(ok).To(BeTrue())
					Expect(payload.Content["externalVersionId"]).To(Equal("123456"))
				}).
				Return(http.Result[downloadResult]{}, errors.New("request error"))
		})

		It("resolves and sends the tvOS external version id", func() {
			_, err := as.Download(DownloadInput{
				Account: Account{
					StoreFront: "143441",
				},
				App: App{
					ID: 42,
				},
				Platform: PlatformAppleTV,
			})
			Expect(err).To(HaveOccurred())
		})
	})

	When("a licensed tvOS app falls back to the redownload endpoint", func() {
		const testRedownload = "https://downloaddispatch.itunes.apple.com/r/redownload"

		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)

			mockPlatformClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[platformVersionLookupResult]{
					StatusCode: 200,
					Data: platformVersionLookupResult{
						Results: map[string]platformVersionLookupItem{
							"42": {
								Offers: []platformVersionLookupOffer{
									{Version: platformVersionLookupVersion{ExternalID: platformVersionExternalID("123456")}},
								},
							},
						},
					},
				}, nil)

			gomock.InOrder(
				mockDownloadClient.EXPECT().
					Send(gomock.Any()).
					Do(func(req http.Request) {
						Expect(req.URL).To(ContainSubstring(PrivateAppStoreAPIPathDownload))
						payload := req.Payload.(*http.XMLPayload)
						Expect(payload.Content).To(HaveKeyWithValue(downloadVersionKeyVolumeStore, "123456"))
						Expect(payload.Content).ToNot(HaveKey(downloadVersionKeyRedownload))
					}).
					Return(http.Result[downloadResult]{Data: downloadResult{FailureType: FailureTypeLicenseAlreadyExists}}, nil),
				mockDownloadClient.EXPECT().
					Send(gomock.Any()).
					Do(func(req http.Request) {
						Expect(req.URL).To(HavePrefix(testRedownload))
						payload := req.Payload.(*http.XMLPayload)
						Expect(payload.Content).To(HaveKeyWithValue(downloadVersionKeyRedownload, "123456"))
						Expect(payload.Content).ToNot(HaveKey(downloadVersionKeyVolumeStore))
					}).
					Return(http.Result[downloadResult]{}, errors.New("stop after fallback")),
			)
		})

		It("retries on redownload translating externalVersionId to appExtVrsId", func() {
			_, err := as.Download(DownloadInput{
				Account:            Account{StoreFront: "143441"},
				App:                App{ID: 42},
				Platform:           PlatformAppleTV,
				RedownloadEndpoint: testRedownload,
			})
			Expect(err).To(HaveOccurred())
		})
	})

	DescribeTable("platform uses the standard download request",
		func(platform Platform) {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Do(func(req http.Request) {
					payload, ok := req.Payload.(*http.XMLPayload)
					Expect(ok).To(BeTrue())
					Expect(payload.Content).ToNot(HaveKey("externalVersionId"))
				}).
				Return(http.Result[downloadResult]{}, errors.New("request error"))

			_, err := as.Download(DownloadInput{
				Account: Account{
					StoreFront: "143441",
				},
				App: App{
					ID: 42,
				},
				Platform: platform,
			})
			Expect(err).To(HaveOccurred())
		},
		Entry("iPhone", PlatformIPhone),
		Entry("iPad", PlatformIPad),
	)

	When("password token is expired", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						FailureType: FailureTypePasswordTokenExpired,
					},
				}, nil)
		})

		It("returns error", func() {
			_, err := as.Download(DownloadInput{})
			Expect(err).To(HaveOccurred())
		})
	})

	When("Sign In to the iTunes Store", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						FailureType: FailureTypeSignInRequired,
					},
				}, nil)
		})

		It("returns error", func() {
			_, err := as.Download(DownloadInput{})
			Expect(err).To(HaveOccurred())
		})
	})

	When("license is missing", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						FailureType: FailureTypeLicenseNotFound,
					},
				}, nil)
		})

		It("returns error", func() {
			_, err := as.Download(DownloadInput{})
			Expect(err).To(HaveOccurred())
		})
	})

	When("store API returns error", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("", nil)
		})

		When("response contains customer message", func() {
			BeforeEach(func() {
				mockDownloadClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[downloadResult]{
						Data: downloadResult{
							FailureType:     "test-failure",
							CustomerMessage: errors.New("").Error(),
						},
					}, nil)
			})

			It("returns customer message as error", func() {
				_, err := as.Download(DownloadInput{})
				Expect(err).To(HaveOccurred())
			})
		})

		When("response does not contain customer message", func() {
			BeforeEach(func() {
				mockDownloadClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[downloadResult]{
						Data: downloadResult{
							FailureType: "test-failure",
						},
					}, nil)
			})

			It("returns generic error", func() {
				_, err := as.Download(DownloadInput{})
				Expect(err).To(HaveOccurred())
			})
		})
	})

	When("store API returns no items", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						Items: []downloadItemResult{},
					},
				}, nil)
		})

		It("returns error", func() {
			_, err := as.Download(DownloadInput{})
			Expect(err).To(HaveOccurred())
		})
	})

	When("fails to resolve output path", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						Items: []downloadItemResult{{}},
					},
				}, nil)

			mockOS.EXPECT().
				Stat(gomock.Any()).
				Return(nil, errors.New(""))
		})

		It("returns error", func() {
			_, err := as.Download(DownloadInput{
				OutputPath: "test-out",
			})
			Expect(err).To(HaveOccurred())
		})
	})

	When("fails to download file", func() {
		BeforeEach(func() {

			mockOS.EXPECT().
				Getwd().
				Return("", nil)

			mockMachine.EXPECT().
				MacAddress().
				Return("", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						Items: []downloadItemResult{{}},
					},
				}, nil)
		})

		When("fails to create download request", func() {
			BeforeEach(func() {
				mockHTTPClient.EXPECT().
					NewRequest("GET", gomock.Any(), nil).
					Return(nil, errors.New(""))
			})

			It("returns error", func() {
				_, err := as.Download(DownloadInput{})
				Expect(err).To(HaveOccurred())
			})
		})

		When("fails to open file", func() {
			BeforeEach(func() {
				mockHTTPClient.EXPECT().
					NewRequest("GET", gomock.Any(), nil).
					Return(nil, nil)

				mockOS.EXPECT().
					OpenFile(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, errors.New(""))
			})

			It("returns error", func() {
				_, err := as.Download(DownloadInput{})
				Expect(err).To(HaveOccurred())
			})
		})

		When("fails to get file info", func() {
			BeforeEach(func() {
				mockHTTPClient.EXPECT().
					NewRequest("GET", gomock.Any(), nil).
					Return(nil, nil)

				mockOS.EXPECT().
					OpenFile(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, nil)

				mockOS.EXPECT().
					Stat(gomock.Any()).
					Return(&dummyFileInfo{}, errors.New(""))

			})

			It("returns error", func() {
				_, err := as.Download(DownloadInput{})
				Expect(err).To(HaveOccurred())
			})
		})

		When("request fails", func() {
			BeforeEach(func() {
				mockHTTPClient.EXPECT().
					NewRequest("GET", gomock.Any(), nil).
					Return(&gohttp.Request{Header: map[string][]string{}}, nil)

				mockOS.EXPECT().
					OpenFile(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, nil)

				mockOS.EXPECT().
					Stat(gomock.Any()).
					Return(&dummyFileInfo{}, nil)

				mockHTTPClient.EXPECT().
					Do(gomock.Any()).
					Return(&gohttp.Response{Body: io.NopCloser(strings.NewReader(""))}, errors.New(""))
			})

			It("returns error", func() {
				_, err := as.Download(DownloadInput{})
				Expect(err).To(HaveOccurred())
			})
		})

		When("fails to write data to file", func() {
			BeforeEach(func() {
				mockHTTPClient.EXPECT().
					NewRequest("GET", gomock.Any(), nil).
					Return(&gohttp.Request{Header: map[string][]string{}}, nil)

				mockOS.EXPECT().
					OpenFile(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, nil)

				mockOS.EXPECT().
					Stat(gomock.Any()).
					Return(&dummyFileInfo{}, nil)

				mockHTTPClient.EXPECT().
					Do(gomock.Any()).
					Return(&gohttp.Response{
						Body: io.NopCloser(strings.NewReader("ping")),
					}, nil)

			})

			It("returns error", func() {
				_, err := as.Download(DownloadInput{})
				Expect(err).To(HaveOccurred())
			})
		})

	})

	When("successfully downloads file", func() {
		var testFile *os.File

		BeforeEach(func() {
			var err error
			testFile, err = os.CreateTemp("", "test_file")
			Expect(err).ToNot(HaveOccurred())

			mockMachine.EXPECT().
				MacAddress().
				Return("", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						Items: []downloadItemResult{
							{
								Metadata: map[string]interface{}{
									"bundleShortVersionString": "xyz",
								},
								Sinfs: []Sinf{
									{
										ID:   0,
										Data: []byte("test-sinf-data"),
									},
								},
							},
						},
					},
				}, nil)

			mockHTTPClient.EXPECT().
				NewRequest("GET", gomock.Any(), nil).
				Return(&gohttp.Request{Header: map[string][]string{}}, nil)

			mockOS.EXPECT().
				OpenFile(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(testFile, nil)

			mockOS.EXPECT().
				Stat(gomock.Any()).
				Return(&dummyFileInfo{}, nil)

			mockHTTPClient.EXPECT().
				Do(gomock.Any()).
				Return(&gohttp.Response{
					Body: io.NopCloser(strings.NewReader("ping")),
				}, nil)
		})

		AfterEach(func() {
			err := os.Remove(testFile.Name())
			Expect(err).ToNot(HaveOccurred())
		})

		It("writes data to file", func() {
			mockOS.EXPECT().
				Getwd().
				Return("", nil)

			_, err := as.Download(DownloadInput{})
			Expect(err).To(HaveOccurred())

			testData, err := os.ReadFile(testFile.Name())
			Expect(err).ToNot(HaveOccurred())
			Expect(string(testData)).To(Equal("ping"))
		})

		When("successfully applies patches", func() {
			var (
				tmpFile    *os.File
				outputPath string
			)

			BeforeEach(func() {

				var err error
				tmpFile, err = os.OpenFile(fmt.Sprintf("%s.tmp", testFile.Name()), os.O_CREATE|os.O_WRONLY, 0644)
				Expect(err).ToNot(HaveOccurred())

				outputPath = strings.TrimSuffix(tmpFile.Name(), ".tmp")

				mockOS.EXPECT().
					OpenFile(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(os.OpenFile)

				mockOS.EXPECT().
					Stat(gomock.Any()).
					Return(nil, nil)

				mockOS.EXPECT().
					Remove(tmpFile.Name()).
					Return(nil)

				zipFile := zip.NewWriter(tmpFile)
				w, err := zipFile.Create("Payload/Test.app/Info.plist")
				Expect(err).ToNot(HaveOccurred())

				info, err := plist.Marshal(map[string]interface{}{
					"CFBundleExecutable": "Test",
				}, plist.BinaryFormat)
				Expect(err).ToNot(HaveOccurred())

				_, err = w.Write(info)
				Expect(err).ToNot(HaveOccurred())

				err = zipFile.Close()
				Expect(err).ToNot(HaveOccurred())
			})

			AfterEach(func() {
				err := os.Remove(tmpFile.Name())
				Expect(err).ToNot(HaveOccurred())
			})

			It("succeeds", func() {
				out, err := as.Download(DownloadInput{
					OutputPath: outputPath,
				})
				Expect(err).ToNot(HaveOccurred())
				Expect(out.DestinationPath).ToNot(BeEmpty())
			})
		})
	})

	Describe("package platform validation", func() {
		writePackage := func(platforms []string) string {
			file, err := os.CreateTemp("", "ipatool-platform-*.ipa")
			Expect(err).ToNot(HaveOccurred())
			defer file.Close()

			zipFile := zip.NewWriter(file)
			w, err := zipFile.Create("Payload/Test.app/Info.plist")
			Expect(err).ToNot(HaveOccurred())

			info, err := plist.Marshal(map[string]interface{}{
				"CFBundleSupportedPlatforms": platforms,
			}, plist.BinaryFormat)
			Expect(err).ToNot(HaveOccurred())

			_, err = w.Write(info)
			Expect(err).ToNot(HaveOccurred())
			Expect(zipFile.Close()).To(Succeed())

			return file.Name()
		}

		It("accepts AppleTVOS packages", func() {
			path := writePackage([]string{"AppleTVOS"})
			defer os.Remove(path)

			err := (&appstore{}).validatePackagePlatform(path, PlatformAppleTV)
			Expect(err).ToNot(HaveOccurred())
		})

		It("returns an error for packages without AppleTVOS support", func() {
			path := writePackage([]string{"iPhoneOS"})
			defer os.Remove(path)

			err := (&appstore{}).validatePackagePlatform(path, PlatformAppleTV)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("AppleTVOS"))
		})
	})
})
