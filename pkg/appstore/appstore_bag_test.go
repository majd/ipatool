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

var _ = Describe("AppStore (Bag)", func() {
	var (
		ctrl          *gomock.Controller
		mockBagClient *http.MockClient[bagResult]
		mockMachine   *machine.MockMachine
		as            AppStore
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockBagClient = http.NewMockClient[bagResult](ctrl)
		mockMachine = machine.NewMockMachine(ctrl)
		as = &appstore{
			bagClient: mockBagClient,
			machine:   mockMachine,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	DescribeTable("normalizes the authentication path while preserving the bag host and query", func(endpoint, expected string) {
		mockMachine.EXPECT().MacAddress().Return("00:11:22:33:44:55", nil)
		result := validBagResult()
		result.URLBag.AuthEndpoint = endpoint
		mockBagClient.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: result}, nil)
		out, err := as.Bag(BagInput{})
		Expect(err).NotTo(HaveOccurred())
		Expect(out.AuthEndpoint).To(Equal(expected))
		Expect(out.SAPConfig.AuthEndpoint).To(Equal(expected))
	},
		Entry("bare path", testAuthEndpoint, testAuthEndpoint+"/"),
		Entry("trailing slash", testAuthEndpoint+"/", testAuthEndpoint+"/"),
		Entry("bare path with routing query", testAuthEndpoint+"?Pod=7&routing=a%2Fb+c", testAuthEndpoint+"/?Pod=7&routing=a%2Fb+c"),
		Entry("bare pod URL with routing query", "https://p7-buy.itunes.apple.com"+PrivateAppStoreAPIPathAuth+"?Pod=7&PRH=7", "https://p7-buy.itunes.apple.com"+PrivateAppStoreAPIPathAuth+"/?Pod=7&PRH=7"),
		Entry("slashed pod URL with routing query", "https://p7-buy.itunes.apple.com"+PrivateAppStoreAPIPathAuth+"/?routing=opaque", "https://p7-buy.itunes.apple.com"+PrivateAppStoreAPIPathAuth+"/?routing=opaque"),
		Entry("explicit HTTPS port", "https://buy.itunes.apple.com:443"+PrivateAppStoreAPIPathAuth, "https://buy.itunes.apple.com:443"+PrivateAppStoreAPIPathAuth+"/"),
	)

	DescribeTable("rejects invalid authentication URLs before normalizing the path", func(endpoint string) {
		mockMachine.EXPECT().MacAddress().Return("00:11:22:33:44:55", nil)
		result := validBagResult()
		result.URLBag.AuthEndpoint = endpoint
		mockBagClient.EXPECT().Send(gomock.Any()).Return(http.Result[bagResult]{StatusCode: gohttp.StatusOK, Data: result}, nil)
		_, err := as.Bag(BagInput{})
		Expect(err).To(HaveOccurred())
	},
		Entry("double slash", testAuthEndpoint+"//"),
		Entry("extra path", testAuthEndpoint+"/extra"),
		Entry("encoded slash", testAuthEndpoint+"%2f"),
		Entry("different path", "https://buy.itunes.apple.com/other"),
		Entry("foreign host", "https://example.com"+PrivateAppStoreAPIPathAuth),
		Entry("HTTP", "http://buy.itunes.apple.com"+PrivateAppStoreAPIPathAuth),
		Entry("userinfo", "https://user@buy.itunes.apple.com"+PrivateAppStoreAPIPathAuth),
		Entry("unsupported port", "https://buy.itunes.apple.com:8443"+PrivateAppStoreAPIPathAuth),
		Entry("fragment", testAuthEndpoint+"#fragment"),
		Entry("malformed URL", testAuthEndpoint+"%zz"),
	)

	When("fails to read machine MAC address", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("", errors.New("mac error"))
		})

		It("returns error", func() {
			_, err := as.Bag(BagInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get mac address"))
		})
	})

	When("request fails", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)

			mockBagClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[bagResult]{}, errors.New("request error"))
		})

		It("returns wrapped error", func() {
			_, err := as.Bag(BagInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to send http request"))
		})
	})

	When("request returns non-200 status code", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:11:22:33:44:55", nil)

			mockBagClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[bagResult]{
					StatusCode: gohttp.StatusForbidden,
				}, nil)
		})

		It("returns error", func() {
			_, err := as.Bag(BagInput{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("received unexpected status code"))
		})
	})

	When("request contains valid SAP configuration", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("aa:bb:cc:dd:ee:ff", nil)

			mockBagClient.EXPECT().
				Send(gomock.Any()).
				Do(func(req http.Request) {
					Expect(req.Method).To(Equal(http.MethodGET))
					Expect(req.URL).To(Equal("https://init.itunes.apple.com/bag.xml?guid=AABBCCDDEEFF"))
					Expect(req.ResponseFormat).To(Equal(http.ResponseFormatXML))
					Expect(req.Headers).To(HaveKeyWithValue("Accept", "application/xml"))
				}).
				Return(http.Result[bagResult]{
					StatusCode: gohttp.StatusOK,
					Data:       validBagResult(),
				}, nil)
		})

		It("returns parsed SAP configuration", func() {
			out, err := as.Bag(BagInput{})
			Expect(err).ToNot(HaveOccurred())
			Expect(out.SAPConfig).To(Equal(validSAPConfig()))
		})
	})

	When("the authentication endpoint is missing", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("aa:bb:cc:dd:ee:ff", nil)

			mockBagClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[bagResult]{
					StatusCode: gohttp.StatusOK,
					Data: bagResult{URLBag: urlBag{
						SAPSetupEndpoint:     testSAPSetupEndpoint,
						SAPSetupCertEndpoint: testSAPSetupCertEndpoint,
						SAPVersion:           testSAPVersion,
					}},
				}, nil)
		})

		It("returns an error", func() {
			_, err := as.Bag(BagInput{})
			Expect(err).To(MatchError(ContainSubstring("invalid authentication endpoint")))
		})
	})

	When("the SAP version is invalid", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("aa:bb:cc:dd:ee:ff", nil)

			result := validBagResult()
			result.URLBag.SAPVersion = "invalid"
			mockBagClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[bagResult]{
					StatusCode: gohttp.StatusOK,
					Data:       result,
				}, nil)
		})

		It("returns an error", func() {
			_, err := as.Bag(BagInput{})
			Expect(err).To(MatchError(ContainSubstring("invalid SAP version")))
		})
	})
})

const (
	testAuthEndpoint         = "https://buy.itunes.apple.com/WebObjects/MZFinance.woa/wa/authenticate"
	testSAPSetupEndpoint     = "https://fpinit.example.com/v1/signSapSetup/legacy"
	testSAPSetupCertEndpoint = "https://static.example.com/sap/setupCert.plist"
	testSAPVersion           = "200"
)

func validBagResult() bagResult {
	return bagResult{URLBag: urlBag{
		AuthEndpoint:         testAuthEndpoint,
		SAPSetupEndpoint:     testSAPSetupEndpoint,
		SAPSetupCertEndpoint: testSAPSetupCertEndpoint,
		SAPVersion:           testSAPVersion,
	}}
}

func validSAPConfig() SAPConfig {
	return SAPConfig{
		AuthEndpoint:   testAuthEndpoint + "/",
		SetupURL:       testSAPSetupEndpoint,
		CertificateURL: testSAPSetupCertEndpoint,
		Version:        200,
	}
}
