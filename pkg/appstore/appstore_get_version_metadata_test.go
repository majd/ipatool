package appstore

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	gohttp "net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"howett.net/plist"
)

func testIPA(displayVersion string, releaseDate interface{}, modified time.Time) []byte {
	buffer := new(bytes.Buffer)
	zipWriter := zip.NewWriter(buffer)

	fillerHeader := &zip.FileHeader{
		Name:   "Payload/Test.app/Filler.bin",
		Method: zip.Store,
	}
	filler, err := zipWriter.CreateHeader(fillerHeader)
	Expect(err).ToNot(HaveOccurred())

	_, err = filler.Write(make([]byte, 1024*1024))
	Expect(err).ToNot(HaveOccurred())

	infoHeader := &zip.FileHeader{
		Name:     "Payload/Test.app/Info.plist",
		Method:   zip.Deflate,
		Modified: modified,
	}

	infoFile, err := zipWriter.CreateHeader(infoHeader)
	Expect(err).ToNot(HaveOccurred())

	info := map[string]interface{}{
		"CFBundleExecutable":         "Test",
		"CFBundleShortVersionString": displayVersion,
	}
	if releaseDate != nil {
		info["releaseDate"] = releaseDate
	}

	infoData, err := plist.Marshal(info, plist.BinaryFormat)
	Expect(err).ToNot(HaveOccurred())

	_, err = infoFile.Write(infoData)
	Expect(err).ToNot(HaveOccurred())

	err = zipWriter.Close()
	Expect(err).ToNot(HaveOccurred())

	return buffer.Bytes()
}

func testIPAServer(data []byte) (*httptest.Server, *int64, *int64) {
	return testIPAServerWithRangeLog(data, nil)
}

func testIPAServerWithRangeLog(data []byte, rangeLog *[]string) (*httptest.Server, *int64, *int64) {
	var (
		servedBytes   int64
		wholeGetCount int64
	)

	server := httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
		if r.Method != gohttp.MethodGet {
			w.WriteHeader(gohttp.StatusMethodNotAllowed)

			return
		}

		rangeHeader := r.Header.Get("Range")
		if rangeLog != nil {
			*rangeLog = append(*rangeLog, rangeHeader)
		}

		if rangeHeader == "" {
			atomic.AddInt64(&wholeGetCount, 1)
			w.WriteHeader(gohttp.StatusOK)
			_, _ = w.Write(data)

			return
		}

		start, end, err := testRangeBounds(rangeHeader, len(data))
		if err != nil {
			w.WriteHeader(gohttp.StatusRequestedRangeNotSatisfiable)

			return
		}

		w.Header().Set("Accept-Ranges", "bytes")
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(data)))
		w.Header().Set("Content-Length", strconv.Itoa(end-start+1))
		w.WriteHeader(gohttp.StatusPartialContent)

		n, _ := w.Write(data[start : end+1])
		atomic.AddInt64(&servedBytes, int64(n))
	}))

	return server, &servedBytes, &wholeGetCount
}

func testRangeBounds(header string, size int) (int, int, error) {
	if !strings.HasPrefix(header, "bytes=") {
		return 0, 0, fmt.Errorf("invalid range header: %s", header)
	}

	parts := strings.Split(strings.TrimPrefix(header, "bytes="), "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid range header: %s", header)
	}

	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("failed to parse range start: %w", err)
	}

	end := size - 1
	if parts[1] != "" {
		end, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, fmt.Errorf("failed to parse range end: %w", err)
		}
	}

	if start < 0 || start >= size || end < start {
		return 0, 0, fmt.Errorf("invalid range bounds: %s", header)
	}

	if end >= size {
		end = size - 1
	}

	return start, end, nil
}

var _ = Describe("HTTPRangeReaderAt", func() {
	It("clamps reads that cross EOF", func() {
		data := []byte("abcdef")
		rangeLog := []string{}
		server, _, _ := testIPAServerWithRangeLog(data, &rangeLog)
		defer server.Close()

		reader, size, err := newHTTPRangeReaderAt(http.NewClient[interface{}](http.Args{}), server.URL)
		Expect(err).NotTo(HaveOccurred())
		Expect(size).To(Equal(int64(len(data))))

		buf := make([]byte, 4)
		n, err := reader.ReadAt(buf, 4)
		Expect(n).To(Equal(2))
		Expect(err).To(Equal(io.EOF))
		Expect(string(buf[:n])).To(Equal("ef"))
		Expect(rangeLog).To(ContainElement("bytes=4-5"))
	})
})

var _ = Describe("AppStore (GetVersionMetadata)", func() {
	var (
		ctrl               *gomock.Controller
		mockMachine        *machine.MockMachine
		mockDownloadClient *http.MockClient[downloadResult]
		as                 AppStore
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockMachine = machine.NewMockMachine(ctrl)
		mockDownloadClient = http.NewMockClient[downloadResult](ctrl)
		as = &appstore{
			machine:        mockMachine,
			downloadClient: mockDownloadClient,
			httpClient:     http.NewClient[interface{}](http.Args{}),
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	When("fails to get MAC address", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("", errors.New("mac error"))
		})

		It("returns error", func() {
			_, err := as.GetVersionMetadata(GetVersionMetadataInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get mac address"))
		})
	})

	When("request fails", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{}, errors.New("request error"))
		})

		It("returns error", func() {
			_, err := as.GetVersionMetadata(GetVersionMetadataInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to send http request"))
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
				Return(http.Result[downloadResult]{}, errors.New("request error"))
		})

		It("sends the request to the pod-specific host", func() {
			_, err := as.GetVersionMetadata(GetVersionMetadataInput{
				Account: Account{
					Pod: testPod,
				},
			})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to send http request"))
		})
	})

	When("password token is expired", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						FailureType: FailureTypePasswordTokenExpired,
					},
				}, nil)
		})

		It("returns error", func() {
			_, err := as.GetVersionMetadata(GetVersionMetadataInput{})
			Expect(err).To(Equal(ErrPasswordTokenExpired))
		})
	})

	When("Sign In to the iTunes Store", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						FailureType: FailureTypeSignInRequired,
					},
				}, nil)
		})

		It("returns error", func() {
			_, err := as.GetVersionMetadata(GetVersionMetadataInput{})
			Expect(err).To(Equal(ErrPasswordTokenExpired))
		})
	})

	When("license is missing", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						FailureType: FailureTypeLicenseNotFound,
					},
				}, nil)
		})

		It("returns error", func() {
			_, err := as.GetVersionMetadata(GetVersionMetadataInput{})
			Expect(err).To(Equal(ErrLicenseRequired))
		})
	})

	When("store API returns error", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)
		})

		When("response contains customer message", func() {
			BeforeEach(func() {
				mockDownloadClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[downloadResult]{
						Data: downloadResult{
							FailureType:     "SOME_ERROR",
							CustomerMessage: "Customer error message",
						},
					}, nil)
			})

			It("returns customer message as error", func() {
				_, err := as.GetVersionMetadata(GetVersionMetadataInput{})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("Customer error message"))
			})
		})

		When("response does not contain customer message", func() {
			BeforeEach(func() {
				mockDownloadClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[downloadResult]{
						Data: downloadResult{
							FailureType: "SOME_ERROR",
						},
					}, nil)
			})

			It("returns generic error", func() {
				_, err := as.GetVersionMetadata(GetVersionMetadataInput{})
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("SOME_ERROR"))
			})
		})
	})

	When("store API returns no items", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						Items: []downloadItemResult{},
					},
				}, nil)
		})

		It("returns error", func() {
			_, err := as.GetVersionMetadata(GetVersionMetadataInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("invalid response"))
		})
	})

	When("fails to parse release date", func() {
		var server *httptest.Server

		BeforeEach(func() {
			ipa := testIPA("1.0.0", "invalid-date", time.Date(2024, 3, 19, 12, 0, 0, 0, time.UTC))
			server, _, _ = testIPAServer(ipa)

			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						Items: []downloadItemResult{
							{
								URL: server.URL,
								Metadata: map[string]interface{}{
									"bundleShortVersionString": "1.0.0",
									"releaseDate":              "invalid-date",
								},
							},
						},
					},
				}, nil)
		})

		AfterEach(func() {
			server.Close()
		})

		It("returns error", func() {
			_, err := as.GetVersionMetadata(GetVersionMetadataInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse release date"))
		})
	})

	When("IPA metadata cannot be read", func() {
		var server *httptest.Server

		BeforeEach(func() {
			server = httptest.NewServer(gohttp.HandlerFunc(func(w gohttp.ResponseWriter, r *gohttp.Request) {
				w.WriteHeader(gohttp.StatusOK)
			}))

			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						Items: []downloadItemResult{
							{
								URL: server.URL,
								Metadata: map[string]interface{}{
									"bundleShortVersionString": "1.0.0",
									"releaseDate":              "2024-03-20T12:00:00Z",
								},
							},
						},
					},
				}, nil)
		})

		AfterEach(func() {
			server.Close()
		})

		It("returns error instead of falling back to API metadata", func() {
			_, err := as.GetVersionMetadata(GetVersionMetadataInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to read version metadata"))
		})
	})

	When("successfully gets version metadata", func() {
		var (
			server         *httptest.Server
			ipa            []byte
			servedBytes    *int64
			wholeGetCount  *int64
			releaseDate    time.Time
			displayVersion string
		)

		BeforeEach(func() {
			releaseDate = time.Date(2024, 4, 2, 12, 0, 0, 0, time.UTC)
			displayVersion = "2.0.0"
			ipa = testIPA(displayVersion, fmt.Sprintf(" \n%s\t", releaseDate.Format(time.RFC3339)), time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
			server, servedBytes, wholeGetCount = testIPAServer(ipa)

			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						Items: []downloadItemResult{
							{
								URL: server.URL,
								Metadata: map[string]interface{}{
									"releaseDate":              "2020-01-01T00:00:00Z",
									"bundleShortVersionString": "1.0.0",
								},
							},
						},
					},
				}, nil)
		})

		AfterEach(func() {
			server.Close()
		})

		It("returns version metadata", func() {
			output, err := as.GetVersionMetadata(GetVersionMetadataInput{
				Account: Account{
					DirectoryServicesID: "test-dsid",
				},
				App: App{
					ID: 1234567890,
				},
				VersionID: "test-version",
			})

			Expect(err).NotTo(HaveOccurred())
			Expect(output.DisplayVersion).To(Equal(displayVersion))
			Expect(output.ReleaseDate).To(Equal(releaseDate))
			Expect(atomic.LoadInt64(wholeGetCount)).To(BeZero())
			Expect(atomic.LoadInt64(servedBytes)).To(BeNumerically("<", int64(len(ipa)/2)))
		})
	})
})
