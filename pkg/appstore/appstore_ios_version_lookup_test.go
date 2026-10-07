package appstore

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

// Synthetic catalog API response for an example app.
const arcadeCatalogResponse = `{"data":[{"id":"42","type":"apps","attributes":{"deviceFamilies":["tvos","iphone","ipad","ipod"],"platformAttributes":{"ios":{"bundleId":"com.example.arcade","externalVersionId":1002}}}}]}`

func decodeCatalogResponse(body string) platformVersionLookupResult {
	var result platformVersionLookupResult

	Expect(json.Unmarshal([]byte(body), &result)).To(Succeed())

	return result
}

var _ = Describe("iOS catalog version fallback", func() {
	var client *http.MockClient[platformVersionLookupResult]
	var store *appstore
	app := App{ID: 42, BundleID: "com.example.arcade"}
	account := Account{StoreFront: "143443-2,34"}

	BeforeEach(func() {
		client = http.NewMockClient[platformVersionLookupResult](gomock.NewController(GinkgoT()))
		store = &appstore{platformClient: client}
	})

	DescribeTable("resolves Arcade versions after exhausting the MDM catalogs", func(platform Platform, noOffers bool, quotedVersion bool) {
		empty := platformVersionLookupResult{}
		if noOffers {
			empty.Results = map[string]platformVersionLookupItem{"42": {}}
		}
		body := arcadeCatalogResponse
		if quotedVersion {
			body = strings.ReplaceAll(body, "1002", `"1002"`)
		}
		calls := make([]any, 0, 4)
		for _, catalog := range []string{"enterprisestore", "iphone", "ipad"} {
			calls = append(calls, client.EXPECT().Send(store.platformVersionLookupRequest(app.ID, "DE", catalog)).Return(http.Result[platformVersionLookupResult]{StatusCode: 200, Data: empty}, nil))
		}
		calls = append(calls, client.EXPECT().Send(http.Request{
			URL:            "https://apps.apple.com/api/apps/v1/catalog/de/apps/42?platform=" + string(platform),
			Method:         http.MethodGET,
			ResponseFormat: http.ResponseFormatJSON,
		}).Return(http.Result[platformVersionLookupResult]{StatusCode: 200, Data: decodeCatalogResponse(body)}, nil))
		gomock.InOrder(calls...)
		version, err := store.lookupLatestExternalVersionID(account, app, platform)
		Expect(err).ToNot(HaveOccurred())
		Expect(version).To(Equal("1002"))
	},
		Entry("missing iPhone app", PlatformIPhone, false, false),
		Entry("missing iPad offers", PlatformIPad, true, false),
		Entry("string version identifier", PlatformIPhone, false, true),
	)

	DescribeTable("preserves catalog and API failures", func(status int, requestErr error, message string) {
		gomock.InOrder(
			client.EXPECT().Send(gomock.Any()).Return(http.Result[platformVersionLookupResult]{StatusCode: 200}, nil).Times(3),
			client.EXPECT().Send(gomock.Any()).Return(http.Result[platformVersionLookupResult]{StatusCode: status}, requestErr),
		)
		_, err := store.lookupLatestExternalVersionID(account, app, PlatformIPhone)
		Expect(err).To(MatchError(And(ContainSubstring("iOS catalog fallback failed"), ContainSubstring(message), ContainSubstring("storefront DE"))))
		Expect(errors.Is(err, errPlatformAppNotFound)).To(BeTrue())
		if requestErr != nil {
			Expect(errors.Is(err, requestErr)).To(BeTrue())
		}
	},
		Entry("HTTP failure", 503, nil, "HTTP 503"),
		Entry("network failure", 0, errors.New("connection reset"), "connection reset"),
		Entry("decoding failure", 200, errors.New("invalid JSON"), "invalid JSON"),
		Entry("empty response", 200, nil, "exactly one app"),
	)

	DescribeTable("rejects unusable API metadata", func(body string, message string) {
		client.EXPECT().Send(gomock.Any()).Return(http.Result[platformVersionLookupResult]{StatusCode: 200, Data: decodeCatalogResponse(body)}, nil)
		_, err := store.lookupLatestIOSExternalVersionID(app, "US", PlatformIPhone)
		Expect(err).To(MatchError(ContainSubstring(message)))
	},
		Entry("no app", `{"data":[]}`, "exactly one app"),
		Entry("wrong app", strings.ReplaceAll(arcadeCatalogResponse, "42", "1"), "different app"),
		Entry("wrong resource type", strings.ReplaceAll(arcadeCatalogResponse, `"apps"`, `"app-bundles"`), "different app"),
		Entry("wrong bundle", strings.ReplaceAll(arcadeCatalogResponse, "com.example.arcade", "other"), "different bundle"),
		Entry("Mac version only", strings.ReplaceAll(arcadeCatalogResponse, `"ios"`, `"macos"`), "requested platform"),
		Entry("TV version only", strings.ReplaceAll(arcadeCatalogResponse, `"ios"`, `"tvos"`), "requested platform"),
		Entry("iPad only", strings.ReplaceAll(arcadeCatalogResponse, `"iphone",`, ""), "requested platform"),
		Entry("missing version", strings.ReplaceAll(arcadeCatalogResponse, "1002", "null"), "no valid external version id"),
		Entry("zero version", strings.ReplaceAll(arcadeCatalogResponse, "1002", "0"), "no valid external version id"),
		Entry("negative version", strings.ReplaceAll(arcadeCatalogResponse, "1002", "-1"), "no valid external version id"),
		Entry("fractional version", strings.ReplaceAll(arcadeCatalogResponse, "1002", "1.5"), "no valid external version id"),
		Entry("nonnumeric version", strings.ReplaceAll(arcadeCatalogResponse, "1002", `"invalid"`), "no valid external version id"),
	)

	It("supports requests made with only an app ID", func() {
		client.EXPECT().Send(gomock.Any()).Return(http.Result[platformVersionLookupResult]{StatusCode: 200, Data: decodeCatalogResponse(arcadeCatalogResponse)}, nil)
		version, err := store.lookupLatestIOSExternalVersionID(App{ID: app.ID}, "US", PlatformIPhone)
		Expect(err).ToNot(HaveOccurred())
		Expect(version).To(Equal("1002"))
	})
})

var _ = Describe("ListVersions for apps missing from the MDM catalogs", func() {
	DescribeTable("uses the API version for redownload and retains license handling", func(licensed bool) {
		ctrl := gomock.NewController(GinkgoT())
		defer ctrl.Finish()
		catalog := http.NewMockClient[platformVersionLookupResult](ctrl)
		downloads := http.NewMockClient[downloadResult](ctrl)
		bags := http.NewMockClient[bagResult](ctrl)
		device := machine.NewMockMachine(ctrl)
		store := &appstore{platformClient: catalog, downloadClient: downloads, bagClient: bags, machine: device}
		device.EXPECT().MacAddress().Return("00:11:22:33:44:55", nil)
		result := downloadResult{FailureType: FailureTypeLicenseNotFound}
		if licensed {
			result = downloadResult{Items: []downloadItemResult{{Metadata: map[string]interface{}{
				"softwareVersionExternalIdentifiers": []interface{}{uint64(1001), uint64(1002)},
				"softwareVersionExternalIdentifier":  uint64(1002),
			}}}}
		}
		gomock.InOrder(
			downloads.EXPECT().Send(gomock.Any()).Return(http.Result[downloadResult]{StatusCode: 200}, nil),
			bags.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{StatusCode: 200, Data: bagResult{URLBag: urlBag{RedownloadEndpoint: testRedownloadEndpoint}}}, nil),
			catalog.EXPECT().Send(gomock.Any()).Return(http.Result[platformVersionLookupResult]{StatusCode: 200}, nil).Times(3),
			catalog.EXPECT().Send(gomock.Any()).Return(http.Result[platformVersionLookupResult]{StatusCode: 200, Data: decodeCatalogResponse(arcadeCatalogResponse)}, nil),
			downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
				Expect(req.URL).To(Equal(testRedownloadEndpoint + "?guid=001122334455"))
				Expect(req.Payload.(*http.XMLPayload).Content).To(HaveKeyWithValue("appExtVrsId", "1002"))
			}).Return(http.Result[downloadResult]{StatusCode: 200, Data: result}, nil),
		)
		out, err := store.ListVersions(ListVersionsInput{
			Account: Account{StoreFront: "143441-1,34"},
			App:     App{ID: 42, BundleID: "com.example.arcade"},
		})
		if licensed {
			Expect(err).ToNot(HaveOccurred())
			Expect(out.LatestExternalVersionID).To(Equal("1002"))
			Expect(out.ExternalVersionIdentifiers).To(Equal([]string{"1001", "1002"}))
		} else {
			Expect(err).To(MatchError(ErrLicenseRequired))
		}
	},
		Entry("licensed app", true),
		Entry("missing license", false),
	)
})
