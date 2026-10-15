package appstore

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	gohttp "net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	"github.com/majd/ipatool/v2/pkg/util/operatingsystem"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"howett.net/plist"
)

var _ = Describe("Redownloading delisted iOS apps", func() {
	const guid = "001122334455"
	var (
		store     *appstore
		catalogs  *http.MockClient[platformVersionLookupResult]
		downloads *http.MockClient[downloadResult]
		assets    *http.MockClient[interface{}]
		device    *machine.MockMachine
		input     DownloadInput
		response  http.Result[downloadResult]
		bagCall   *gomock.Call
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		catalogs = http.NewMockClient[platformVersionLookupResult](ctrl)
		downloads = http.NewMockClient[downloadResult](ctrl)
		assets = http.NewMockClient[interface{}](ctrl)
		device = machine.NewMockMachine(ctrl)
		bags := http.NewMockClient[bagResult](ctrl)
		store = &appstore{
			platformClient: catalogs, downloadClient: downloads, bagClient: bags,
			httpClient: assets, machine: device, os: operatingsystem.New(),
		}
		input = DownloadInput{
			Context: context.Background(),
			Account: Account{StoreFront: "143467-2,29", DirectoryServicesID: "1234", Pod: "42"},
			App:     App{ID: 42, BundleID: "com.example.arcade"}, Platform: PlatformIPhone,
			OutputPath: filepath.Join(GinkgoT().TempDir(), "app.ipa"),
		}
		response = http.Result[downloadResult]{StatusCode: gohttp.StatusOK, Data: downloadResult{
			Items: []downloadItemResult{{URL: "https://example.test/app.ipa", Metadata: map[string]interface{}{
				"itemId": uint64(42), "softwareVersionBundleId": input.App.BundleID,
				"softwareVersionExternalIdentifier": uint64(1001), "bundleShortVersionString": "1.0",
			}}},
		}}
		primary := downloads.EXPECT().Send(store.downloadProductRequest(store.volumeStoreEndpoint(input.Account), input.Account, input.App, guid, "")).
			Return(http.Result[downloadResult]{StatusCode: gohttp.StatusOK}, nil)
		bagCall = bags.EXPECT().Send(gomock.Any()).After(primary).Return(http.Result[bagResult]{
			StatusCode: gohttp.StatusOK,
			Data: bagResult{URLBag: urlBag{
				RedownloadEndpoint: testRedownloadEndpoint, UpdateEndpoint: testUpdateEndpoint,
			}},
		}, nil)
	})

	expectCatalogs := func(noOffers bool, final http.Result[platformVersionLookupResult], requestErr error) *gomock.Call {
		missing := platformVersionLookupResult{}
		if noOffers {
			missing.Results = map[string]platformVersionLookupItem{"42": {}}
		}
		previous := bagCall
		for _, catalog := range []string{"enterprisestore", "iphone", "ipad"} {
			previous = catalogs.EXPECT().Send(store.platformVersionLookupRequest(input.App.ID, "IN", catalog)).After(previous).
				Return(http.Result[platformVersionLookupResult]{StatusCode: gohttp.StatusOK, Data: missing}, nil)
		}
		platform := input.Platform
		if platform == "" {
			platform = PlatformIPhone
		}

		return catalogs.EXPECT().Send(http.Request{
			URL:    "https://apps.apple.com/api/apps/v1/catalog/in/apps/42?platform=" + string(platform),
			Method: http.MethodGET, ResponseFormat: http.ResponseFormatJSON,
		}).After(previous).Return(final, requestErr)
	}

	DescribeTable("tries one unpinned request in the same account after a catalog 404", func(platform Platform, noOffers, bundleKnown bool) {
		input.Platform = platform
		if !bundleKnown {
			input.App.BundleID = ""
		}
		last := expectCatalogs(noOffers, http.Result[platformVersionLookupResult]{StatusCode: gohttp.StatusNotFound}, nil)
		var requestContext context.Context
		downloads.EXPECT().Send(gomock.Any()).After(last).Do(func(req http.Request) {
			Expect(req.URL).To(Equal(testRedownloadEndpoint + "?guid=" + guid))
			Expect(req.Headers).To(Equal(store.downloadProductRequest(store.volumeStoreEndpoint(input.Account), input.Account, input.App, guid, "").Headers))
			Expect(req.Payload.(*http.XMLPayload).Content).To(Equal(map[string]interface{}{
				"salableAdamId": input.App.ID, "guid": guid, "serialNumber": "0", "creditDisplay": "",
			}))
			requestContext = req.Context
			Expect(requestContext).NotTo(BeNil())
			Expect(requestContext.Err()).NotTo(HaveOccurred())
			deadline, ok := requestContext.Deadline()
			Expect(ok).To(BeTrue())
			Expect(time.Until(deadline)).To(BeNumerically(">", 0))
			Expect(time.Until(deadline)).To(BeNumerically("<=", http.DefaultAuthenticationTimeout))
		}).Return(response, nil)

		actual, resolved, err := store.sendDownloadProduct(input.Context, input.Account, input.App, guid, "", input.Platform)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual).To(Equal(response))
		if platform == "" {
			Expect(resolved).To(Equal(PlatformIPhone))
		} else {
			Expect(resolved).To(Equal(platform))
		}
		Expect(requestContext.Err()).To(MatchError(context.Canceled))
	},
		Entry("default iOS with missing app", Platform(""), false, true),
		Entry("iPhone with missing app", PlatformIPhone, false, true),
		Entry("iPad with missing offers", PlatformIPad, true, true),
		Entry("numeric app ID only", PlatformIPhone, false, false),
	)

	DescribeTable("preserves public catalog errors without redownloading", func(status int, body string, requestErr error, message string) {
		expectCatalogs(false, http.Result[platformVersionLookupResult]{StatusCode: status, Data: decodeCatalogResponse(body)}, requestErr)
		_, _, err := store.sendDownloadProduct(input.Context, input.Account, input.App, guid, "", input.Platform)
		Expect(err).To(MatchError(ContainSubstring(message)))
		Expect(errors.Is(err, errPlatformAppNotFound)).To(BeTrue())
		Expect(errors.Is(err, errIOSCatalogAppNotFound)).To(BeFalse())
		if requestErr != nil {
			Expect(errors.Is(err, requestErr)).To(BeTrue())
		}
	},
		Entry("server error", 503, `{}`, nil, "HTTP 503"),
		Entry("network error", 0, `{}`, errors.New("connection reset"), "connection reset"),
		Entry("invalid JSON", 200, `{}`, errors.New("invalid JSON"), "invalid JSON"),
		Entry("unreadable 404", 404, `{}`, errors.New("invalid JSON"), "invalid JSON"),
		Entry("canceled request", 0, `{}`, context.Canceled, "context canceled"),
		Entry("empty metadata", 200, `{}`, nil, "exactly one app"),
		Entry("empty app list", 200, `{"data":[]}`, nil, "exactly one app"),
		Entry("wrong app", 200, strings.ReplaceAll(arcadeCatalogResponse, `"42"`, `"43"`), nil, "different app"),
		Entry("wrong bundle", 200, strings.ReplaceAll(arcadeCatalogResponse, "com.example.arcade", "other"), nil, "different bundle"),
		Entry("missing version", 200, strings.ReplaceAll(arcadeCatalogResponse, "1002", "null"), nil, "no valid external version id"),
		Entry("wrong platform", 200, strings.ReplaceAll(arcadeCatalogResponse, `"ios"`, `"tvos"`), nil, "requested platform"),
	)

	Context("after a confirmed catalog 404", func() {
		var catalogCall *gomock.Call
		BeforeEach(func() {
			catalogCall = expectCatalogs(false, http.Result[platformVersionLookupResult]{StatusCode: gohttp.StatusNotFound}, nil)
		})

		DescribeTable("rejects unusable responses without trying another endpoint", func(outcome, message string) {
			switch outcome {
			case "wrong app":
				response.Data.Items[0].Metadata["itemId"] = uint64(43)
			case "wrong bundle":
				response.Data.Items[0].Metadata["softwareVersionBundleId"] = "com.example.other"
			case "missing bundle":
				delete(response.Data.Items[0].Metadata, "softwareVersionBundleId")
			case "missing URL":
				response.Data.Items[0].URL = ""
			case "empty":
				response.Data.Items = nil
			case "multiple items":
				response.Data.Items = append(response.Data.Items, response.Data.Items[0])
			case "HTTP 500":
				response.StatusCode = gohttp.StatusInternalServerError
			case "unavailable":
				response.Data = downloadResult{CustomerMessage: "App No Longer Available"}
			}
			downloads.EXPECT().Send(gomock.Any()).After(catalogCall).Return(response, nil)
			_, _, err := store.sendDownloadProduct(input.Context, input.Account, input.App, guid, "", input.Platform)
			Expect(err).To(MatchError(ContainSubstring(message)))
		},
			Entry("wrong app", "wrong app", "requested app"),
			Entry("wrong bundle", "wrong bundle", "requested bundle identifier"),
			Entry("missing bundle", "missing bundle", "requested bundle identifier"),
			Entry("missing package URL", "missing URL", "no package URL"),
			Entry("empty response", "empty", "exactly one item"),
			Entry("multiple items", "multiple items", "exactly one item"),
			Entry("HTTP 500", "HTTP 500", "status code: 500"),
			Entry("unavailable", "unavailable", "App No Longer Available"),
		)

		DescribeTable("does not retry failed requests or try updateProduct", func(requestErr error) {
			downloads.EXPECT().Send(gomock.Any()).After(catalogCall).Return(http.Result[downloadResult]{}, requestErr)
			_, _, err := store.sendDownloadProduct(input.Context, input.Account, input.App, guid, "", input.Platform)
			Expect(errors.Is(err, requestErr)).To(BeTrue())
		},
			Entry("empty HTTP 500", &http.UnexpectedResponseError{StatusCode: gohttp.StatusInternalServerError}),
			Entry("network error", errors.New("connection reset")),
			Entry("timeout", context.DeadlineExceeded),
		)

		DescribeTable("retains authentication and license recovery", func(failure string, expected error) {
			device.EXPECT().MacAddress().Return("00:11:22:33:44:55", nil)
			response.Data = downloadResult{FailureType: failure}
			downloads.EXPECT().Send(gomock.Any()).After(catalogCall).Return(response, nil)
			_, err := store.Download(input)
			Expect(err).To(MatchError(expected))
		},
			Entry("missing license", FailureTypeLicenseNotFound, ErrLicenseRequired),
			Entry("expired token", FailureTypePasswordTokenExpired, ErrPasswordTokenExpired),
			Entry("sign in required", FailureTypeSignInRequired, ErrPasswordTokenExpired),
		)

		It("does not send the fallback after cancellation during lookup", func() {
			ctx, cancel := context.WithCancel(input.Context)
			defer cancel()
			catalogCall.Do(func(http.Request) { cancel() })
			_, _, err := store.sendDownloadProduct(ctx, input.Account, input.App, guid, "", input.Platform)
			Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		})

		It("propagates cancellation and an earlier caller deadline to the request", func() {
			ctx, cancel := context.WithTimeout(input.Context, time.Second)
			defer cancel()
			downloads.EXPECT().Send(gomock.Any()).After(catalogCall).DoAndReturn(func(req http.Request) (http.Result[downloadResult], error) {
				deadline, ok := req.Context.Deadline()
				Expect(ok).To(BeTrue())
				callerDeadline, _ := ctx.Deadline()
				Expect(deadline).To(Equal(callerDeadline))
				cancel()
				Expect(req.Context.Err()).To(MatchError(context.Canceled))

				return http.Result[downloadResult]{}, req.Context.Err()
			})
			_, _, err := store.sendDownloadProduct(ctx, input.Account, input.App, guid, "", input.Platform)
			Expect(errors.Is(err, context.Canceled)).To(BeTrue())
		})

		DescribeTable("validates the downloaded package before replacing the destination", func(supportedPlatform string) {
			// Exercise the default platform, which must resolve to iPhone for validation.
			input.Platform = ""
			device.EXPECT().MacAddress().Return("00:11:22:33:44:55", nil)
			previousOutput := []byte("previous output")
			Expect(os.WriteFile(input.OutputPath, previousOutput, 0600)).To(Succeed())
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
			downloads.EXPECT().Send(gomock.Any()).After(catalogCall).Return(response, nil)
			assets.EXPECT().NewRequest("GET", response.Data.Items[0].URL, nil).
				Return(&gohttp.Request{Header: gohttp.Header{}}, nil)
			assets.EXPECT().Do(gomock.Any()).Return(&gohttp.Response{
				StatusCode: gohttp.StatusOK, Body: io.NopCloser(bytes.NewReader(packageBuffer.Bytes())),
				ContentLength: int64(packageBuffer.Len()),
			}, nil)

			out, err := store.Download(input)
			if supportedPlatform == "iPhoneOS" {
				Expect(err).NotTo(HaveOccurred())
				Expect(out.DestinationPath).To(Equal(input.OutputPath))
				Expect(store.validatePackagePlatform(out.DestinationPath, PlatformIPhone)).To(Succeed())
			} else {
				Expect(err).To(MatchError(ContainSubstring("does not declare iPhoneOS support")))
				Expect(os.ReadFile(input.OutputPath)).To(Equal(previousOutput))
			}
			Expect(input.OutputPath + ".tmp").NotTo(BeAnExistingFile())
		},
			Entry("iOS package", "iPhoneOS"),
			Entry("tvOS package", "AppleTVOS"),
			Entry("missing platform declaration", ""),
		)
	})
})
