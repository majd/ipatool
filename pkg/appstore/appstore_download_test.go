package appstore

import (
	zip "archive/zip"
	"errors"
	"fmt"
	"github.com/golang/mock/gomock"
	"github.com/majd/ipatool/pkg/http"
	"github.com/majd/ipatool/pkg/keychain"
	"github.com/majd/ipatool/pkg/log"
	"github.com/majd/ipatool/pkg/util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"howett.net/plist"
	"io"
	gohttp "net/http"
	"os"
	"strings"
)

var _ = Describe("AppStore (Download)", func() {
	var (
		ctrl               *gomock.Controller
		mockKeychain       *keychain.MockKeychain
		mockSearchClient   *http.MockClient[SearchResult]
		mockDownloadClient *http.MockClient[DownloadResult]
		mockPurchaseClient *http.MockClient[PurchaseResult]
		mockLoginClient    *http.MockClient[LoginResult]
		mockHTTPClient     *http.MockClient[interface{}]
		mockLogger         *log.MockLogger
		mockOS             *util.MockOperatingSystem
		mockMachine        *util.MockMachine
		as                 AppStore
		testErr            = errors.New("testErr")
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockKeychain = keychain.NewMockKeychain(ctrl)
		mockLogger = log.NewMockLogger(ctrl)
		mockSearchClient = http.NewMockClient[SearchResult](ctrl)
		mockDownloadClient = http.NewMockClient[DownloadResult](ctrl)
		mockLoginClient = http.NewMockClient[LoginResult](ctrl)
		mockPurchaseClient = http.NewMockClient[PurchaseResult](ctrl)
		mockHTTPClient = http.NewMockClient[interface{}](ctrl)
		mockOS = util.NewMockOperatingSystem(ctrl)
		mockMachine = util.NewMockMachine(ctrl)
		as = &appstore{
			keychain:       mockKeychain,
			loginClient:    mockLoginClient,
			searchClient:   mockSearchClient,
			purchaseClient: mockPurchaseClient,
			downloadClient: mockDownloadClient,
			httpClient:     mockHTTPClient,
			machine:        mockMachine,
			os:             mockOS,
			logger:         mockLogger,
			interactive:    true,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	When("not logged in", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte{}, testErr)
		})

		It("returns error", func() {
			err := as.Download("", "", false, false, false)
			Expect(err).To(MatchError(ContainSubstring(ErrGetAccount.Error())))
		})
	})

	When("fails to determine country code", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{\"storeFront\":\"\"}"), nil)
		})

		It("returns error", func() {
			err := as.Download("", "", false, false, false)
			Expect(err).To(MatchError(ContainSubstring(ErrInvalidCountryCode.Error())))
		})
	})

	When("fails to find app", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{\"storeFront\":\"143441\"}"), nil)

			mockSearchClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{}, testErr)
		})

		It("returns error", func() {
			err := as.Download("", "", false, false, false)
			Expect(err).To(MatchError(ContainSubstring(ErrAppLookup.Error())))
		})
	})

	When("fails to resolve output path", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{\"storeFront\":\"143441\"}"), nil)

			mockSearchClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{
					StatusCode: 200,
					Data: SearchResult{
						Count:   1,
						Results: []App{{}},
					},
				}, nil)

			mockOS.EXPECT().
				Stat(gomock.Any()).
				Return(nil, testErr)
		})

		It("returns error", func() {
			err := as.Download("", "test-out", false, false, false)
			Expect(err).To(MatchError(ContainSubstring("failed to resolve destination path")))
		})
	})

	When("fails to read MAC address", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{\"storeFront\":\"143441\"}"), nil)

			mockSearchClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{
					StatusCode: 200,
					Data: SearchResult{
						Count:   1,
						Results: []App{{}},
					},
				}, nil)

			mockOS.EXPECT().
				Getwd().
				Return("", nil)

			mockMachine.EXPECT().
				MacAddress().
				Return("", testErr)
		})

		It("returns error", func() {
			err := as.Download("", "", false, false, false)
			Expect(err).To(MatchError(ContainSubstring(ErrGetMAC.Error())))
		})
	})

	When("request fails", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{\"storeFront\":\"143441\"}"), nil)

			mockSearchClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{
					StatusCode: 200,
					Data: SearchResult{
						Count:   1,
						Results: []App{{}},
					},
				}, nil)

			mockOS.EXPECT().
				Getwd().
				Return("", nil)

			mockMachine.EXPECT().
				MacAddress().
				Return("", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[DownloadResult]{}, testErr)

			mockLogger.EXPECT().
				Verbose().
				Return(nil)
		})

		It("returns error", func() {
			err := as.Download("", "", false, false, false)
			Expect(err).To(MatchError(ContainSubstring(ErrRequest.Error())))
		})
	})

	When("password token is expired", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{\"storeFront\":\"143441\"}"), nil)

			mockSearchClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{
					StatusCode: 200,
					Data: SearchResult{
						Count:   1,
						Results: []App{{}},
					},
				}, nil)

			mockOS.EXPECT().
				Getwd().
				Return("", nil)

			mockMachine.EXPECT().
				MacAddress().
				Return("", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[DownloadResult]{
					Data: DownloadResult{
						FailureType: FailureTypePasswordTokenExpired,
					},
				}, nil)

			mockLogger.EXPECT().
				Verbose().
				Return(nil)
		})

		When("attempts to renew credentials", func() {
			BeforeEach(func() {
				mockLogger.EXPECT().
					Verbose().
					Return(nil).
					Times(2)
			})

			When("fails to renew credentials", func() {
				BeforeEach(func() {
					mockLoginClient.EXPECT().
						Send(gomock.Any()).
						Return(http.Result[LoginResult]{}, testErr)
				})

				It("returns error", func() {
					err := as.Download("", "", false, false, false)
					Expect(err).To(MatchError(ContainSubstring(ErrPasswordTokenExpired.Error())))
				})
			})

			When("successfully renews credentials", func() {
				BeforeEach(func() {
					mockLoginClient.EXPECT().
						Send(gomock.Any()).
						Return(http.Result[LoginResult]{
							Data: LoginResult{},
						}, nil)

					mockKeychain.EXPECT().
						Set("account", gomock.Any()).
						Return(nil)

					mockDownloadClient.EXPECT().
						Send(gomock.Any()).
						Return(http.Result[DownloadResult]{
							Data: DownloadResult{
								FailureType: FailureTypePasswordTokenExpired,
							},
						}, nil)
				})

				It("attempts to download app", func() {
					err := as.Download("", "", false, false, false)
					Expect(err).To(MatchError(ContainSubstring(ErrPasswordTokenExpired.Error())))
				})
			})
		})
	})

	When("license is missing", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{\"storeFront\":\"143441\"}"), nil)

			mockSearchClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{
					StatusCode: 200,
					Data: SearchResult{
						Count:   1,
						Results: []App{{}},
					},
				}, nil)

			mockOS.EXPECT().
				Getwd().
				Return("", nil)

			mockMachine.EXPECT().
				MacAddress().
				Return("", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[DownloadResult]{
					Data: DownloadResult{
						FailureType: FailureTypeLicenseNotFound,
					},
				}, nil)

			mockLogger.EXPECT().
				Verbose().
				Return(nil)
		})

		It("returns error", func() {
			err := as.Download("", "", false, false, false)
			Expect(err).To(MatchError(ContainSubstring(ErrLicenseRequired.Error())))
		})

		When("attempts to acquire license", func() {
			BeforeEach(func() {
				mockLogger.EXPECT().
					Verbose().
					Return(nil)
			})

			When("license is acquired", func() {
				BeforeEach(func() {
					mockKeychain.EXPECT().
						Get("account").
						Return([]byte("{\"storeFront\":\"143441\"}"), nil)

					mockPurchaseClient.EXPECT().
						Send(gomock.Any()).
						Return(http.Result[PurchaseResult]{
							StatusCode: 200,
							Data: PurchaseResult{
								JingleDocType: "purchaseSuccess",
								Status:        0,
							},
						}, nil)

					mockSearchClient.EXPECT().
						Send(gomock.Any()).
						Return(http.Result[SearchResult]{
							StatusCode: 200,
							Data: SearchResult{
								Count:   1,
								Results: []App{{}},
							},
						}, nil)

					mockDownloadClient.EXPECT().
						Send(gomock.Any()).
						Return(http.Result[DownloadResult]{}, testErr)
				})

				It("attempts to download app", func() {
					err := as.Download("", "", true, false, false)
					Expect(err).To(MatchError(ContainSubstring(testErr.Error())))
				})
			})

			When("fails to acquire license", func() {
				BeforeEach(func() {
					mockKeychain.EXPECT().
						Get("account").
						Return([]byte{}, testErr)
				})

				It("returns error", func() {
					err := as.Download("", "", true, false, false)
					Expect(err).To(MatchError(ContainSubstring(ErrPurchase.Error())))
				})
			})
		})
	})

	When("store API returns error", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{\"storeFront\":\"143441\"}"), nil)

			mockSearchClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{
					StatusCode: 200,
					Data: SearchResult{
						Count:   1,
						Results: []App{{}},
					},
				}, nil)

			mockOS.EXPECT().
				Getwd().
				Return("", nil)

			mockMachine.EXPECT().
				MacAddress().
				Return("", nil)

			mockLogger.EXPECT().
				Verbose().
				Return(nil).
				Times(2)
		})

		When("response contains customer message", func() {
			BeforeEach(func() {
				mockDownloadClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[DownloadResult]{
						Data: DownloadResult{
							FailureType:     "test-failure",
							CustomerMessage: testErr.Error(),
						},
					}, nil)
			})

			It("returns customer message as error", func() {
				err := as.Download("", "", true, false, false)
				Expect(err).To(MatchError(ContainSubstring(testErr.Error())))
			})
		})

		When("response does not contain customer message", func() {
			BeforeEach(func() {
				mockDownloadClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[DownloadResult]{
						Data: DownloadResult{
							FailureType: "test-failure",
						},
					}, nil)
			})

			It("returns generic error", func() {
				err := as.Download("", "", true, false, false)
				Expect(err).To(MatchError(ContainSubstring(ErrGeneric.Error())))
			})
		})
	})

	When("store API returns no items", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{\"storeFront\":\"143441\"}"), nil)

			mockSearchClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{
					StatusCode: 200,
					Data: SearchResult{
						Count:   1,
						Results: []App{{}},
					},
				}, nil)

			mockOS.EXPECT().
				Getwd().
				Return("", nil)

			mockMachine.EXPECT().
				MacAddress().
				Return("", nil)

			mockLogger.EXPECT().
				Verbose().
				Return(nil).
				Times(2)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[DownloadResult]{
					Data: DownloadResult{
						Items: []DownloadItemResult{},
					},
				}, nil)
		})

		It("returns error", func() {
			err := as.Download("", "", true, false, false)
			Expect(err).To(MatchError(ContainSubstring("received 0 items from the App Store")))
		})
	})

	When("fails to download file", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{\"storeFront\":\"143441\"}"), nil)

			mockSearchClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{
					StatusCode: 200,
					Data: SearchResult{
						Count:   1,
						Results: []App{{}},
					},
				}, nil)

			mockOS.EXPECT().
				Getwd().
				Return("", nil)

			mockMachine.EXPECT().
				MacAddress().
				Return("", nil)

			mockLogger.EXPECT().
				Verbose().
				Return(nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[DownloadResult]{
					Data: DownloadResult{
						Items: []DownloadItemResult{{}},
					},
				}, nil)
		})

		When("fails to create download request", func() {
			BeforeEach(func() {
				mockHTTPClient.EXPECT().
					NewRequest("GET", gomock.Any(), nil).
					Return(nil, testErr)
			})

			It("returns error", func() {
				err := as.Download("", "", true, false, false)
				Expect(err).To(MatchError(ContainSubstring(ErrCreateRequest.Error())))
			})
		})

		When("request fails", func() {
			BeforeEach(func() {
				mockHTTPClient.EXPECT().
					NewRequest("GET", gomock.Any(), nil).
					Return(nil, nil)

				mockHTTPClient.EXPECT().
					Do(gomock.Any()).
					Return(nil, testErr)
			})

			It("returns error", func() {
				err := as.Download("", "", true, false, false)
				Expect(err).To(MatchError(ContainSubstring(ErrRequest.Error())))
			})
		})

		When("fails to open file", func() {
			BeforeEach(func() {
				mockHTTPClient.EXPECT().
					NewRequest("GET", gomock.Any(), nil).
					Return(nil, nil)

				mockHTTPClient.EXPECT().
					Do(gomock.Any()).
					Return(&gohttp.Response{
						Body: gohttp.NoBody,
					}, nil)

				mockOS.EXPECT().
					OpenFile(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, testErr)
			})

			It("returns error", func() {
				err := as.Download("", "", true, false, false)
				Expect(err).To(MatchError(ContainSubstring("failed to open file")))
			})
		})

		When("fails to write data to file", func() {
			BeforeEach(func() {
				mockHTTPClient.EXPECT().
					NewRequest("GET", gomock.Any(), nil).
					Return(nil, nil)

				mockHTTPClient.EXPECT().
					Do(gomock.Any()).
					Return(&gohttp.Response{
						Body: io.NopCloser(strings.NewReader("ping")),
					}, nil)

				mockOS.EXPECT().
					OpenFile(gomock.Any(), gomock.Any(), gomock.Any()).
					Return(nil, nil)

				mockLogger.EXPECT().
					Verbose().
					Return(nil)
			})

			It("returns error", func() {
				err := as.Download("", "", true, false, false)
				Expect(err).To(MatchError(ContainSubstring(ErrFileWrite.Error())))
			})
		})
	})

	When("successfully downloads file", func() {
		var testFile *os.File

		BeforeEach(func() {
			var err error
			testFile, err = os.CreateTemp("", "test_file")
			Expect(err).ToNot(HaveOccurred())

			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{\"storeFront\":\"143441\"}"), nil)

			mockSearchClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{
					StatusCode: 200,
					Data: SearchResult{
						Count:   1,
						Results: []App{{}},
					},
				}, nil)

			mockMachine.EXPECT().
				MacAddress().
				Return("", nil)

			mockLogger.EXPECT().
				Verbose().
				Return(nil).
				Times(2)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[DownloadResult]{
					Data: DownloadResult{
						Items: []DownloadItemResult{
							{
								Metadata: map[string]interface{}{},
								Sinfs: []DownloadSinfResult{
									{
										ID:   0,
										Data: []byte("test-sinf-data"),
									},
								},
							},
						},
					},
				}, nil)

			mockOS.EXPECT().
				OpenFile(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(testFile, nil)

			mockHTTPClient.EXPECT().
				NewRequest("GET", gomock.Any(), nil).
				Return(nil, nil)

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

			mockOS.EXPECT().
				OpenFile(gomock.Any(), gomock.Any(), gomock.Any()).
				Return(nil, testErr)

			err := as.Download("", "", true, false, false)
			Expect(err).To(MatchError(ContainSubstring(ErrOpenFile.Error())))

			testData, err := os.ReadFile(testFile.Name())
			Expect(err).ToNot(HaveOccurred())
			Expect(string(testData)).To(Equal("ping"))
		})

		When("successfully applies patches", func() {
			var tmpFile *os.File
			var zipFile *zip.Writer
			var outputPath string

			BeforeEach(func() {
				var err error
				tmpFile, err = os.OpenFile(fmt.Sprintf("%s.tmp", testFile.Name()), os.O_CREATE|os.O_WRONLY, 0644)
				Expect(err).ToNot(HaveOccurred())

				outputPath = strings.TrimSuffix(tmpFile.Name(), ".tmp")

				mockOS.EXPECT().
					OpenFile(gomock.Any(), gomock.Any(), gomock.Any()).
					DoAndReturn(func(name string, flag int, perm os.FileMode) (*os.File, error) {
						return os.OpenFile(name, flag, perm)
					})

				mockLogger.EXPECT().
					Log().
					Return(nil)

				mockLogger.EXPECT().
					Verbose().
					Return(nil)

				mockOS.EXPECT().
					Stat(gomock.Any()).
					Return(nil, nil)

				mockOS.EXPECT().
					Remove(tmpFile.Name()).
					Return(nil)
			})

			AfterEach(func() {
				err := os.Remove(tmpFile.Name())
				Expect(err).ToNot(HaveOccurred())
			})

			When("app uses legacy FairPlay protection", func() {
				BeforeEach(func() {
					zipFile = zip.NewWriter(tmpFile)
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

					mockLogger.EXPECT().
						Verbose().
						Return(nil)
				})

				It("succeeds", func() {
					err := as.Download("", outputPath, true, false, false)
					Expect(err).ToNot(HaveOccurred())
				})
			})

			When("app uses modern FairPlay protection", func() {
				BeforeEach(func() {
					zipFile = zip.NewWriter(tmpFile)
					w, err := zipFile.Create("Payload/Test.app/SC_Info/Manifest.plist")
					Expect(err).ToNot(HaveOccurred())

					manifest, err := plist.Marshal(PackageManifest{
						SinfPaths: []string{
							"SC_Info/TestApp.sinf",
						},
					}, plist.BinaryFormat)
					Expect(err).ToNot(HaveOccurred())

					_, err = w.Write(manifest)
					Expect(err).ToNot(HaveOccurred())

					w, err = zipFile.Create("Payload/Test.app/Info.plist")
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

				It("succeeds", func() {
					outputPath := strings.TrimSuffix(tmpFile.Name(), ".tmp")
					err := as.Download("", outputPath, true, false, false)
					Expect(err).ToNot(HaveOccurred())
				})
			})

			When("app uses modern FairPlay protection and has Watch app", func() {
				BeforeEach(func() {
					zipFile = zip.NewWriter(tmpFile)
					w, err := zipFile.Create("Payload/Test.app/SC_Info/Manifest.plist")
					Expect(err).ToNot(HaveOccurred())

					manifest, err := plist.Marshal(PackageManifest{
						SinfPaths: []string{
							"SC_Info/TestApp.sinf",
						},
					}, plist.BinaryFormat)
					Expect(err).ToNot(HaveOccurred())

					_, err = w.Write(manifest)
					Expect(err).ToNot(HaveOccurred())

					w, err = zipFile.Create("Payload/Test.app/Info.plist")
					Expect(err).ToNot(HaveOccurred())

					info, err := plist.Marshal(map[string]interface{}{
						"CFBundleExecutable": "Test",
					}, plist.BinaryFormat)
					Expect(err).ToNot(HaveOccurred())

					_, err = w.Write(info)
					Expect(err).ToNot(HaveOccurred())

					w, err = zipFile.Create("Payload/Test.app/Watch/Test Watch App.app/Info.plist")
					Expect(err).ToNot(HaveOccurred())

					watchInfo, err := plist.Marshal(map[string]interface{}{
						"WKWatchKitApp": true,
					}, plist.BinaryFormat)
					Expect(err).ToNot(HaveOccurred())

					_, err = w.Write(watchInfo)
					Expect(err).ToNot(HaveOccurred())

					err = zipFile.Close()
					Expect(err).ToNot(HaveOccurred())
				})

				It("sinf is in the correct path", func() {
					outputPath := strings.TrimSuffix(tmpFile.Name(), ".tmp")
					err := as.Download("", outputPath, true, false, false)
					Expect(err).ToNot(HaveOccurred())

					r, err := zip.OpenReader(outputPath)
					Expect(err).ToNot(HaveOccurred())
					defer r.Close()

					found := false
					for _, f := range r.File {
						if f.Name == "Payload/Test Watch App.app/SC_Info/TestApp.sinf" {
							found = true
							break
						}
					}
					Expect(found).To(BeFalse())
				})
			})
		})
	})
})
