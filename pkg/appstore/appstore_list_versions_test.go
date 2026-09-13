package appstore

import (
	"errors"
	gohttp "net/http"

	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("AppStore (ListVersions)", func() {
	var (
		ctrl               *gomock.Controller
		mockBagClient      *http.MockClient[bagResult]
		mockDownloadClient *http.MockClient[downloadResult]
		mockPlatformClient *http.MockClient[platformVersionLookupResult]
		mockMachine        *machine.MockMachine
		as                 AppStore
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockBagClient = http.NewMockClient[bagResult](ctrl)
		mockDownloadClient = http.NewMockClient[downloadResult](ctrl)
		mockPlatformClient = http.NewMockClient[platformVersionLookupResult](ctrl)
		mockMachine = machine.NewMockMachine(ctrl)
		as = &appstore{
			bagClient:      mockBagClient,
			downloadClient: mockDownloadClient,
			platformClient: mockPlatformClient,
			machine:        mockMachine,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("pins the Mac offer before requesting a universal app's version history", func() {
		pages := http.NewMockClient[[]byte](ctrl)
		as.(*appstore).storefrontClient = pages
		mockMachine.EXPECT().MacAddress().Return("00:11:22:33:44:55", nil)
		gomock.InOrder(
			pages.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
				Expect(req.URL).To(Equal("https://apps.apple.com/de/app/id6472431552?platform=mac"))
			}).Return(http.Result[[]byte]{StatusCode: gohttp.StatusOK, Data: macVersionPage(karingMacConfiguration)}, nil),
			mockDownloadClient.EXPECT().Send(gomock.Any()).Do(func(req http.Request) {
				Expect(req.URL).To(ContainSubstring("volumeStoreDownloadProduct"))
				Expect(req.Payload.(*http.XMLPayload).Content).To(HaveKeyWithValue("externalVersionId", "876660716"))
			}).Return(http.Result[downloadResult]{StatusCode: gohttp.StatusOK, Data: downloadResult{Items: []downloadItemResult{{Metadata: map[string]interface{}{
				"softwareVersionExternalIdentifiers": []interface{}{uint64(876660700), uint64(876660716)},
				"softwareVersionExternalIdentifier":  uint64(876660716),
			}}}}}, nil),
		)
		out, err := as.ListVersions(ListVersionsInput{Account: Account{StoreFront: "143443-2,34"}, App: App{ID: 6472431552, BundleID: "com.nebula.karing"}, Platform: PlatformMacOS})
		Expect(err).ToNot(HaveOccurred())
		Expect(out.ExternalVersionIdentifiers).To(Equal([]string{"876660700", "876660716"}))
		Expect(out.LatestExternalVersionID).To(Equal("876660716"))
	})

	It("does not fall back to iOS when the Mac version cannot be resolved", func() {
		pages := http.NewMockClient[[]byte](ctrl)
		as.(*appstore).storefrontClient = pages
		mockMachine.EXPECT().MacAddress().Return("00:11:22:33:44:55", nil)
		pages.EXPECT().Send(gomock.Any()).Return(http.Result[[]byte]{StatusCode: gohttp.StatusOK, Data: macVersionPage(`{}`)}, nil)
		_, err := as.ListVersions(ListVersionsInput{Account: Account{StoreFront: "143443-2,34"}, App: App{ID: 42}, Platform: PlatformMacOS})
		Expect(err).To(MatchError(ContainSubstring("failed to resolve platform version")))
	})

	It("rejects unsupported platforms before making requests", func() {
		_, err := as.ListVersions(ListVersionsInput{Platform: PlatformUnknown})
		Expect(err).To(MatchError(`invalid platform "unknown"`))
	})

	When("fails to get MAC address", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("", errors.New(""))
		})

		It("returns error", func() {
			_, err := as.ListVersions(ListVersionsInput{})
			Expect(err).To(HaveOccurred())
		})
	})

	When("request fails", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{}, errors.New(""))
		})

		It("returns error", func() {
			_, err := as.ListVersions(ListVersionsInput{})
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

		It("sends the request to the pod-specific host", func() {
			_, err := as.ListVersions(ListVersionsInput{
				Account: Account{
					Pod: testPod,
				},
			})
			Expect(err).To(HaveOccurred())
		})
	})

	When("password token is expired", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						FailureType: FailureTypePasswordTokenExpired,
					},
				}, nil)
		})

		It("returns error", func() {
			_, err := as.ListVersions(ListVersionsInput{})
			Expect(err).To(HaveOccurred())
		})
	})

	When("Sign In to the iTunes Store", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						FailureType: FailureTypeSignInRequired,
					},
				}, nil)
		})

		It("returns error", func() {
			_, err := as.ListVersions(ListVersionsInput{})
			Expect(err).To(HaveOccurred())
		})
	})

	When("license is required", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						FailureType: FailureTypeLicenseNotFound,
					},
				}, nil)
		})

		It("returns error", func() {
			_, err := as.ListVersions(ListVersionsInput{})
			Expect(err).To(HaveOccurred())
		})
	})

	When("store API returns error with customer message", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						FailureType:     "test-failure",
						CustomerMessage: "test error message",
					},
				}, nil)
		})

		It("returns error with customer message", func() {
			_, err := as.ListVersions(ListVersionsInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("test error message"))
		})
	})

	When("store API returns error without customer message", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						FailureType: "test-failure",
					},
				}, nil)
		})

		It("returns error with failure type", func() {
			_, err := as.ListVersions(ListVersionsInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("test-failure"))
		})
	})

	When("store API returns no items", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					StatusCode: gohttp.StatusOK,
					Data: downloadResult{
						Items: []downloadItemResult{},
					},
				}, nil)

			mockBagClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[bagResult]{
					StatusCode: gohttp.StatusOK,
					Data:       validBagResult(),
				}, nil)
		})

		It("returns error", func() {
			_, err := as.ListVersions(ListVersionsInput{})
			Expect(err).To(HaveOccurred())
		})
	})

	When("version identifiers not found in metadata", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						Items: []downloadItemResult{
							{
								Metadata: map[string]interface{}{
									"someOtherKey": "someValue",
								},
							},
						},
					},
				}, nil)
		})

		It("returns error", func() {
			_, err := as.ListVersions(ListVersionsInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get version identifiers from item metadata"))
		})
	})

	When("latest version not found in metadata", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						Items: []downloadItemResult{
							{
								Metadata: map[string]interface{}{
									"softwareVersionExternalIdentifiers": []interface{}{"12345678", "87654321"},
								},
							},
						},
					},
				}, nil)
		})

		It("returns error", func() {
			_, err := as.ListVersions(ListVersionsInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get latest version from item metadata"))
		})
	})

	It("lists the iOS history from a version-pinned fallback", func() {
		const latest = "890598805"
		bag := validBagResult()
		bag.URLBag.RedownloadEndpoint = testRedownloadEndpoint
		catalog := platformVersionLookupResult{Results: map[string]platformVersionLookupItem{
			"547702041": {Offers: []platformVersionLookupOffer{
				{Version: platformVersionLookupVersion{ExternalID: platformVersionExternalID(latest)}},
			}},
		}}

		mockMachine.EXPECT().MacAddress().Return("00:11:22:33:44:55", nil)
		gomock.InOrder(
			mockDownloadClient.EXPECT().Send(gomock.Any()).
				Return(http.Result[downloadResult]{StatusCode: gohttp.StatusOK}, nil),
			mockBagClient.EXPECT().Send(gomock.Any()).
				Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: bag}, nil),
			mockPlatformClient.EXPECT().Send(gomock.Any()).
				Return(http.Result[platformVersionLookupResult]{StatusCode: gohttp.StatusOK, Data: catalog}, nil),
			mockDownloadClient.EXPECT().Send(gomock.Any()).
				Do(func(req http.Request) {
					Expect(req.Payload.(*http.XMLPayload).Content).To(HaveKeyWithValue("appExtVrsId", latest))
				}).
				Return(http.Result[downloadResult]{StatusCode: gohttp.StatusOK,
					Data: downloadResult{Items: []downloadItemResult{{Metadata: map[string]interface{}{
						"softwareVersionExternalIdentifiers": []interface{}{"9660833", latest},
						"softwareVersionExternalIdentifier":  latest,
					}}}}}, nil),
		)

		out, err := as.ListVersions(ListVersionsInput{
			Account: Account{StoreFront: "143441-1,34"},
			App:     App{ID: 547702041},
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(out.ExternalVersionIdentifiers).To(Equal([]string{"9660833", latest}))
		Expect(out.LatestExternalVersionID).To(Equal(latest))
	})

	When("successfully lists versions", func() {
		const (
			testVersion1 = "12345678"
			testVersion2 = "87654321"
			testLatest   = "87654321"
		)

		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockDownloadClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[downloadResult]{
					Data: downloadResult{
						Items: []downloadItemResult{
							{
								Metadata: map[string]interface{}{
									"softwareVersionExternalIdentifiers": []interface{}{testVersion1, testVersion2},
									"softwareVersionExternalIdentifier":  testLatest,
								},
							},
						},
					},
				}, nil)
		})

		It("returns versions", func() {
			out, err := as.ListVersions(ListVersionsInput{})
			Expect(err).ToNot(HaveOccurred())
			Expect(out.ExternalVersionIdentifiers).To(Equal([]string{testVersion1, testVersion2}))
			Expect(out.LatestExternalVersionID).To(Equal(testLatest))
		})
	})
})
