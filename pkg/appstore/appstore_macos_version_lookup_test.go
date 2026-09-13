package appstore

import (
	"errors"
	gohttp "net/http"

	"github.com/majd/ipatool/v2/pkg/http"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

const karingMacConfiguration = `{"purchaseConfiguration":{"bundleId":"com.nebula.karing","appPlatforms":["mac","tv","phone","pad"],"metricsPlatformDisplayStyle":"ios","buyParams":"salableAdamId=6472431552&appExtVrsId=876660716"}}`

func macVersionPage(configuration string) []byte {
	return []byte(`<script id="serialized-server-data">` + configuration + `</script>`)
}

var _ = Describe("Mac purchase version selection", func() {
	app := App{ID: 6472431552, BundleID: "com.nebula.karing"}

	It("selects Karing's Mac offer and ignores unrelated and iOS offers", func() {
		body := macVersionPage(`[{"purchaseConfiguration":{"bundleId":"com.nebula.karing","appPlatforms":["phone","pad"],"buyParams":"salableAdamId=6472431552&appExtVrsId=891116578"}}, {"purchaseConfiguration":{"appPlatforms":["mac"],"buyParams":"salableAdamId=100&appExtVrsId=123"}}, ` + karingMacConfiguration + `]`)
		version, err := macOSExternalVersionID(body, app)
		Expect(err).ToNot(HaveOccurred())
		Expect(version).To(Equal("876660716"))
	})

	DescribeTable("rejects invalid catalog metadata", func(configuration string) {
		_, err := macOSExternalVersionID(macVersionPage(configuration), app)
		Expect(err).To(HaveOccurred())
	},
		Entry("invalid JSON", `{`),
		Entry("no offer", `{}`),
		Entry("iOS only", `{"purchaseConfiguration":{"bundleId":"com.nebula.karing","appPlatforms":["phone","pad"],"buyParams":"salableAdamId=6472431552&appExtVrsId=891116578"}}`),
		Entry("wrong bundle", `{"purchaseConfiguration":{"bundleId":"other","appPlatforms":["mac"],"buyParams":"salableAdamId=6472431552&appExtVrsId=123"}}`),
		Entry("invalid version", `{"purchaseConfiguration":{"bundleId":"com.nebula.karing","appPlatforms":["mac"],"buyParams":"salableAdamId=6472431552&appExtVrsId=invalid"}}`),
		Entry("conflicting offers", `[`+karingMacConfiguration+`,{"purchaseConfiguration":{"bundleId":"com.nebula.karing","appPlatforms":["mac"],"buyParams":"salableAdamId=6472431552&appExtVrsId=123"}}]`),
	)

	DescribeTable("pins the Mac offer before requesting a package", func(downloadErr error) {
		ctrl := gomock.NewController(GinkgoT())
		defer ctrl.Finish()
		downloads := http.NewMockClient[downloadResult](ctrl)
		pages := http.NewMockClient[[]byte](ctrl)
		store := &appstore{downloadClient: downloads, storefrontClient: pages}
		expected := http.Result[downloadResult]{StatusCode: gohttp.StatusOK, Data: downloadResult{Items: []downloadItemResult{{URL: "https://example.test/mac.pkg"}}}}
		gomock.InOrder(
			pages.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
				Expect(req.URL).To(Equal("https://apps.apple.com/de/app/id6472431552?platform=mac"))
			}).Return(http.Result[[]byte]{StatusCode: gohttp.StatusOK, Data: macVersionPage(karingMacConfiguration)}, nil),
			downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
				Expect(req.URL).To(Equal(store.volumeStoreEndpoint(Account{}).baseURL + "?guid=001122334455"))
				Expect(req.Payload.(*http.XMLPayload).Content).To(HaveKeyWithValue("externalVersionId", "876660716"))
			}).Return(expected, downloadErr),
		)
		output, platform, err := store.sendDownloadProduct(Account{StoreFront: "143443-2,34"}, app, "001122334455", "", PlatformMacOS)
		Expect(output).To(Equal(expected))
		Expect(platform).To(Equal(PlatformMacOS))
		if downloadErr == nil {
			Expect(err).ToNot(HaveOccurred())
		} else {
			Expect(errors.Is(err, downloadErr)).To(BeTrue())
		}
	},
		Entry("successful primary response", nil),
		Entry("propagates pinned download failure", &http.UnexpectedResponseError{StatusCode: gohttp.StatusInternalServerError}),
	)

	DescribeTable("keeps the Mac version pinned through redownload fallback", func(message string) {
		ctrl := gomock.NewController(GinkgoT())
		defer ctrl.Finish()
		downloads := http.NewMockClient[downloadResult](ctrl)
		pages := http.NewMockClient[[]byte](ctrl)
		bags := http.NewMockClient[bagResult](ctrl)
		store := &appstore{downloadClient: downloads, storefrontClient: pages, bagClient: bags}
		bag := validBagResult()
		bag.URLBag.RedownloadEndpoint = testRedownloadEndpoint
		expected := http.Result[downloadResult]{StatusCode: gohttp.StatusOK, Data: downloadResult{Items: []downloadItemResult{{URL: "https://example.test/mac.pkg"}}}}
		gomock.InOrder(
			pages.EXPECT().Send(gomock.Any()).Return(http.Result[[]byte]{StatusCode: gohttp.StatusOK, Data: macVersionPage(karingMacConfiguration)}, nil),
			downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
				Expect(req.Payload.(*http.XMLPayload).Content).To(HaveKeyWithValue("externalVersionId", "876660716"))
			}).Return(http.Result[downloadResult]{StatusCode: gohttp.StatusOK, Data: downloadResult{CustomerMessage: message}}, nil),
			bags.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: bag}, nil),
			downloads.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
				Expect(req.URL).To(Equal(testRedownloadEndpoint + "?guid=001122334455"))
				Expect(req.Payload.(*http.XMLPayload).Content).To(HaveKeyWithValue("appExtVrsId", "876660716"))
			}).Return(expected, nil),
		)
		output, platform, err := store.sendDownloadProduct(Account{StoreFront: "143443-2,34"}, app, "001122334455", "", PlatformMacOS)
		Expect(err).ToNot(HaveOccurred())
		Expect(output).To(Equal(expected))
		Expect(platform).To(Equal(PlatformMacOS))
	},
		Entry("empty response", ""),
		Entry("unavailable response", "No Longer Available"),
	)

	DescribeTable("propagates lookup failures", func(status int, body []byte, requestErr error) {
		ctrl := gomock.NewController(GinkgoT())
		defer ctrl.Finish()
		pages := http.NewMockClient[[]byte](ctrl)
		store := &appstore{storefrontClient: pages, downloadClient: http.NewMockClient[downloadResult](ctrl)}
		pages.EXPECT().Send(gomock.Any()).Return(http.Result[[]byte]{StatusCode: status, Data: body}, requestErr)
		_, _, err := store.sendDownloadProduct(Account{StoreFront: "143443-2,34"}, app, "001122334455", "", PlatformMacOS)
		Expect(err).To(HaveOccurred())
	},
		Entry("HTTP failure", 500, nil, nil),
		Entry("network failure", 0, nil, errors.New("connection reset")),
		Entry("missing Mac offer", 200, macVersionPage(`{}`), nil),
	)
})
