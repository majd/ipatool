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

	When("request is successful", func() {
		const testAuthEndpoint = "https://example.com"

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
					Data: bagResult{
						URLBag: urlBag{
							AuthEndpoint: testAuthEndpoint,
						},
					},
				}, nil)
		})

		It("returns output", func() {
			out, err := as.Bag(BagInput{})
			Expect(err).ToNot(HaveOccurred())
			Expect(out.AuthEndpoint).To(Equal(testAuthEndpoint))
		})
	})
})
