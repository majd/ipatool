package appstore

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	gohttp "net/http"
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
})
