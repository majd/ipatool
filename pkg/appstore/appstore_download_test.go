package appstore

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	gohttp "net/http"
	"net/url"
	"os"
	"path/filepath"
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
		ctrl                 *gomock.Controller
		mockKeychain         *keychain.MockKeychain
		mockDownloadClient   *http.MockClient[downloadResult]
		mockPlatformClient   *http.MockClient[platformVersionLookupResult]
		mockStorefrontClient *http.MockClient[[]byte]
		mockPurchaseClient   *http.MockClient[purchaseResult]
		mockLoginClient      *http.MockClient[loginResult]
		mockHTTPClient       *http.MockClient[interface{}]
		mockOS               *operatingsystem.MockOperatingSystem
		mockMachine          *machine.MockMachine
		as                   AppStore
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockKeychain = keychain.NewMockKeychain(ctrl)
		mockDownloadClient = http.NewMockClient[downloadResult](ctrl)
		mockPlatformClient = http.NewMockClient[platformVersionLookupResult](ctrl)
		mockStorefrontClient = http.NewMockClient[[]byte](ctrl)
		mockLoginClient = http.NewMockClient[loginResult](ctrl)
		mockPurchaseClient = http.NewMockClient[purchaseResult](ctrl)
		mockHTTPClient = http.NewMockClient[interface{}](ctrl)
		mockOS = operatingsystem.NewMockOperatingSystem(ctrl)
		mockMachine = machine.NewMockMachine(ctrl)
		as = &appstore{
			keychain:         mockKeychain,
			loginClient:      mockLoginClient,
			purchaseClient:   mockPurchaseClient,
			downloadClient:   mockDownloadClient,
			platformClient:   mockPlatformClient,
			storefrontClient: mockStorefrontClient,
			httpClient:       mockHTTPClient,
			machine:          mockMachine,
			os:               mockOS,
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

	When("platform is visionOS", func() {
		It("resolves and sends the visionOS external version id", func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)

			mockStorefrontClient.EXPECT().
				Send(gomock.Any()).
				Do(func(req http.Request) {
					parsedURL, err := url.Parse(req.URL)
					Expect(err).ToNot(HaveOccurred())
					Expect(parsedURL.Host).To(Equal("apps.apple.com"))
					Expect(parsedURL.Path).To(Equal("/us/app/id42"))
					Expect(parsedURL.Query().Get("platform")).To(Equal("vision"))
					Expect(req.Method).To(Equal(http.MethodGET))
					Expect(req.ResponseFormat).To(Equal(http.ResponseFormatRaw))
				}).
				Return(http.Result[[]byte]{
					StatusCode: 200,
					Data:       []byte(`<script type="application/json" id="serialized-server-data">[{"data":{"app":{"purchaseConfiguration":{"metricsPlatformDisplayStyle":"vision","appPlatforms":["vision"],"buyParams":"salableAdamId=42&appExtVrsId=987654"}}}}]</script>`),
				}, nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Do(func(req http.Request) {
					payload, ok := req.Payload.(*http.XMLPayload)
					Expect(ok).To(BeTrue())
					Expect(payload.Content["externalVersionId"]).To(Equal("987654"))
				}).
				Return(http.Result[downloadResult]{}, errors.New("request error"))

			_, err := as.Download(DownloadInput{
				Account: Account{
					StoreFront: "143441",
				},
				App: App{
					ID: 42,
				},
				Platform: PlatformVisionOS,
			})
			Expect(err).To(HaveOccurred())
		})

		It("uses an explicit external version id without a storefront lookup", func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Do(func(req http.Request) {
					payload, ok := req.Payload.(*http.XMLPayload)
					Expect(ok).To(BeTrue())
					Expect(payload.Content["externalVersionId"]).To(Equal("123456"))
				}).
				Return(http.Result[downloadResult]{}, errors.New("request error"))

			_, err := as.Download(DownloadInput{
				App: App{
					ID: 42,
				},
				Platform:          PlatformVisionOS,
				ExternalVersionID: "123456",
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

				err = tmpFile.Close()
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

	Describe("macOS packages", func() {
		It("decrypts the downloaded package with in-memory machine identity and dpInfo", func() {
			tempDir := GinkgoT().TempDir()
			requestedPath := filepath.Join(tempDir, "custom-output.pkg")
			packageData := []byte("encrypted package")
			decryptedData := makeTestXAR([]byte("decrypted payload"), false)
			dpInfo := bytes.Repeat([]byte{0x42}, 88)
			decrypter := &fakeMacPackageDecrypter{output: decryptedData}

			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:aa:bb:cc", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Do(func(req http.Request) {
					payload := req.Payload.(*http.XMLPayload)
					Expect(payload.Content["guid"]).To(Equal("001122AABBCC"))
				}).
				Return(http.Result[downloadResult]{
					StatusCode: 200,
					Data: downloadResult{
						Items: []downloadItemResult{{
							URL:   "https://example.test/app.pkg",
							Sinfs: []Sinf{{DPInfo: dpInfo}},
							Metadata: map[string]interface{}{
								"bundleShortVersionString": "1.2.3",
								"software-platform":        "macos",
								"product-type":             "mac-os-app",
							},
						}},
					},
				}, nil)

			mockHTTPClient.EXPECT().
				NewRequest("GET", "https://example.test/app.pkg", nil).
				Return(&gohttp.Request{Header: gohttp.Header{}}, nil)
			mockHTTPClient.EXPECT().
				Do(gomock.Any()).
				Return(&gohttp.Response{
					Body:          io.NopCloser(bytes.NewReader(packageData)),
					ContentLength: int64(len(packageData)),
				}, nil)

			store := &appstore{
				downloadClient: mockDownloadClient,
				httpClient:     mockHTTPClient,
				machine:        mockMachine,
				os:             operatingsystem.New(),
				macDecrypterFactory: func(ctx context.Context, hardwareID, gotDPInfo []byte) (macPackageDecrypter, error) {
					Expect(ctx).ToNot(BeNil())
					Expect(hardwareID).To(Equal([]byte{0x00, 0x11, 0x22, 0xaa, 0xbb, 0xcc}))
					Expect(gotDPInfo).To(Equal(dpInfo))

					return decrypter, nil
				},
			}
			out, err := store.Download(DownloadInput{
				Context:    context.Background(),
				App:        App{ID: 42, BundleID: "com.example.mac"},
				OutputPath: requestedPath,
				Platform:   PlatformMacOS,
			})

			Expect(err).ToNot(HaveOccurred())
			Expect(out).To(Equal(DownloadOutput{DestinationPath: requestedPath}))
			Expect(decrypter.input).To(Equal(packageData))
			Expect(os.ReadFile(requestedPath)).To(Equal(decryptedData))
			Expect(requestedPath + macDPInfoSuffix).ToNot(BeAnExistingFile())
			Expect(requestedPath + macHWInfoSuffix).ToNot(BeAnExistingFile())
		})

		It("downloads an iOS app available on macOS through the mobile package pipeline", func() {
			tempDir := GinkgoT().TempDir()
			requestedPath := filepath.Join(tempDir, "developer.apple.wwdc-Release_640199958_11.0.2.ipa")
			packageBuffer := new(bytes.Buffer)
			packageWriter := zip.NewWriter(packageBuffer)
			infoWriter, err := packageWriter.Create("Payload/Developer.app/Info.plist")
			Expect(err).ToNot(HaveOccurred())
			info, err := plist.Marshal(map[string]interface{}{
				"CFBundleExecutable":         "Developer",
				"CFBundleSupportedPlatforms": []string{"iPhoneOS"},
			}, plist.BinaryFormat)
			Expect(err).ToNot(HaveOccurred())
			_, err = infoWriter.Write(info)
			Expect(err).ToNot(HaveOccurred())
			Expect(packageWriter.Close()).To(Succeed())

			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:aa:bb:cc", nil)
			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					StatusCode: 200,
					Data: downloadResult{
						Items: []downloadItemResult{{
							URL: "https://example.test/developer.ipa",
							Sinfs: []Sinf{{
								ID:   0,
								Data: []byte("mobile sinf"),
							}},
							Metadata: map[string]interface{}{
								"bundleShortVersionString": "11.0.2",
								"software-platform":        "ios",
								"product-type":             "ios-app",
							},
						}},
					},
				}, nil)
			mockHTTPClient.EXPECT().
				NewRequest("GET", "https://example.test/developer.ipa", nil).
				Return(&gohttp.Request{Header: gohttp.Header{}}, nil)
			mockHTTPClient.EXPECT().
				Do(gomock.Any()).
				Return(&gohttp.Response{
					Body:          io.NopCloser(bytes.NewReader(packageBuffer.Bytes())),
					ContentLength: int64(packageBuffer.Len()),
				}, nil)

			store := &appstore{
				downloadClient: mockDownloadClient,
				httpClient:     mockHTTPClient,
				machine:        mockMachine,
				os:             operatingsystem.New(),
				macDecrypterFactory: func(context.Context, []byte, []byte) (macPackageDecrypter, error) {
					Fail("mobile packages must not initialize the macOS package decrypter")

					return nil, nil
				},
			}

			out, err := store.Download(DownloadInput{
				Context:    context.Background(),
				Account:    Account{Email: "test@example.com"},
				App:        App{ID: 640199958, BundleID: "developer.apple.wwdc-Release"},
				OutputPath: tempDir,
				Platform:   PlatformMacOS,
			})

			Expect(err).ToNot(HaveOccurred())
			Expect(out.DestinationPath).To(Equal(requestedPath))
			Expect(out.Sinfs).To(Equal([]Sinf{{ID: 0, Data: []byte("mobile sinf")}}))
			Expect(requestedPath).To(BeAnExistingFile())
			Expect(requestedPath + macEncryptedStageSuffix).ToNot(BeAnExistingFile())
			Expect(requestedPath + macDecryptedStageSuffix).ToNot(BeAnExistingFile())
		})

		It("uses a pkg name derived from the generated package name", func() {
			store := &appstore{os: operatingsystem.New()}
			tempDir := GinkgoT().TempDir()

			packagePath, err := store.resolveDestinationPath(
				App{ID: 42, BundleID: "com.example.mac"},
				"1.2.3",
				tempDir,
				PlatformMacOS,
			)
			Expect(err).ToNot(HaveOccurred())
			Expect(packagePath).To(Equal(filepath.Join(tempDir, "com.example.mac_42_1.2.3.pkg")))
		})

		It("preserves an explicit output path", func() {
			store := &appstore{os: operatingsystem.New()}
			tempDir := GinkgoT().TempDir()
			requestedPath := filepath.Join(tempDir, "custom-output")

			packagePath, err := store.resolveDestinationPath(App{}, "1.2.3", requestedPath, PlatformMacOS)
			Expect(err).ToNot(HaveOccurred())
			Expect(packagePath).To(Equal(requestedPath))
		})

		It("rejects a download response without dpInfo", func() {
			_, err := macDPInfo(nil)
			Expect(err).To(MatchError(ContainSubstring("dpInfo")))
		})

		It("rejects conflicting dpInfo values", func() {
			_, err := macDPInfo([]Sinf{{DPInfo: []byte("one")}, {DPInfo: []byte("two")}})
			Expect(err).To(MatchError(ContainSubstring("conflicting")))
		})
	})

	Describe("package platform validation", func() {
		writePackageWithInfoPlists := func(infoPlists map[string][]string) string {
			file, err := os.CreateTemp("", "ipatool-platform-*.ipa")
			Expect(err).ToNot(HaveOccurred())
			defer file.Close()

			zipFile := zip.NewWriter(file)
			for path, platforms := range infoPlists {
				w, err := zipFile.Create(path)
				Expect(err).ToNot(HaveOccurred())

				info, err := plist.Marshal(map[string]interface{}{
					"CFBundleSupportedPlatforms": platforms,
				}, plist.BinaryFormat)
				Expect(err).ToNot(HaveOccurred())

				_, err = w.Write(info)
				Expect(err).ToNot(HaveOccurred())
			}

			Expect(zipFile.Close()).To(Succeed())

			return file.Name()
		}
		writePackage := func(platforms []string) string {
			return writePackageWithInfoPlists(map[string][]string{
				"Payload/Test.app/Info.plist": platforms,
			})
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

		It("accepts XROS packages", func() {
			path := writePackage([]string{"XROS"})
			defer os.Remove(path)

			err := (&appstore{}).validatePackagePlatform(path, PlatformVisionOS)
			Expect(err).ToNot(HaveOccurred())
		})

		It("returns an error for packages without XROS support", func() {
			path := writePackage([]string{"iPhoneOS"})
			defer os.Remove(path)

			err := (&appstore{}).validatePackagePlatform(path, PlatformVisionOS)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("XROS"))
		})

		It("ignores supported platforms declared only by an embedded app", func() {
			path := writePackageWithInfoPlists(map[string][]string{
				"Payload/Test.app/Info.plist":                    {"iPhoneOS"},
				"Payload/Test.app/PlugIns/Vision.app/Info.plist": {"XROS"},
			})
			defer os.Remove(path)

			err := (&appstore{}).validatePackagePlatform(path, PlatformVisionOS)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("XROS"))
		})
	})
})
