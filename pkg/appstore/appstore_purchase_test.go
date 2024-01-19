package appstore

import (
	"errors"

	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/keychain"
	"github.com/majd/ipatool/v2/pkg/util/machine"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("AppStore (Purchase)", func() {
	var (
		ctrl               *gomock.Controller
		mockKeychain       *keychain.MockKeychain
		mockMachine        *machine.MockMachine
		mockPurchaseClient *http.MockClient[purchaseResult]
		mockLoginClient    *http.MockClient[loginResult]
		as                 *appstore
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockPurchaseClient = http.NewMockClient[purchaseResult](ctrl)
		mockLoginClient = http.NewMockClient[loginResult](ctrl)
		mockKeychain = keychain.NewMockKeychain(ctrl)
		mockMachine = machine.NewMockMachine(ctrl)
		as = &appstore{
			keychain:       mockKeychain,
			purchaseClient: mockPurchaseClient,
			loginClient:    mockLoginClient,
			machine:        mockMachine,
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
			err := as.Purchase(PurchaseInput{})
			Expect(err).To(HaveOccurred())
		})
	})

	When("app is paid", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)
		})

		It("returns error", func() {
			err := as.Purchase(PurchaseInput{
				Account: Account{
					StoreFront: "143441",
				},
				App: App{
					Price: 0.99,
				},
			})
			Expect(err).To(HaveOccurred())
		})
	})

	When("purchase request fails", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockPurchaseClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[purchaseResult]{}, errors.New(""))
		})

		It("returns error", func() {
			err := as.Purchase(PurchaseInput{
				Account: Account{
					StoreFront: "143441",
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

			mockPurchaseClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[purchaseResult]{
					Data: purchaseResult{
						FailureType: FailureTypePasswordTokenExpired,
					},
				}, nil)
		})

		It("returns error", func() {
			err := as.Purchase(PurchaseInput{
				Account: Account{
					StoreFront: "143441",
				},
			})
			Expect(err).To(HaveOccurred())
		})
	})

	When("store API returns customer error message", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockPurchaseClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[purchaseResult]{
					Data: purchaseResult{
						FailureType:     "failure",
						CustomerMessage: CustomerMessageBadLogin,
					},
				}, nil)
		})

		It("returns error", func() {
			err := as.Purchase(PurchaseInput{
				Account: Account{
					StoreFront: "143441",
				},
			})
			Expect(err).To(HaveOccurred())
		})
	})

	When("store API returns unknown error", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockPurchaseClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[purchaseResult]{
					Data: purchaseResult{
						FailureType: "failure",
					},
				}, nil)
		})

		It("returns error", func() {
			err := as.Purchase(PurchaseInput{
				Account: Account{
					StoreFront: "143441",
				},
			})
			Expect(err).To(HaveOccurred())
		})
	})

	When("account already has a license for the app", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockPurchaseClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[purchaseResult]{
					StatusCode: 500,
					Data:       purchaseResult{},
				}, nil)
		})

		It("returns error", func() {
			err := as.Purchase(PurchaseInput{
				Account: Account{
					StoreFront: "143441",
				},
			})
			Expect(err).To(HaveOccurred())
		})
	})

	When("subscription is required", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockPurchaseClient.EXPECT().
				Send(pricingParametersMatcher{"STDQ"}).
				Return(http.Result[purchaseResult]{
					StatusCode: 200,
					Data: purchaseResult{
						CustomerMessage: "This item is temporarily unavailable.",
						FailureType:     FailureTypeTemporarilyUnavailable,
					},
				}, nil)

			mockPurchaseClient.EXPECT().
				Send(pricingParametersMatcher{"GAME"}).
				Return(http.Result[purchaseResult]{
					StatusCode: 200,
					Data: purchaseResult{
						CustomerMessage: CustomerMessageSubscriptionRequired,
					},
				}, nil)
		})

		It("returns error", func() {
			err := as.Purchase(PurchaseInput{
				Account: Account{
					StoreFront: "143441",
				},
			})
			Expect(err).To(HaveOccurred())
		})
	})

	When("successfully purchases the app", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockPurchaseClient.EXPECT().
				Send(pricingParametersMatcher{"STDQ"}).
				Return(http.Result[purchaseResult]{
					StatusCode: 200,
					Data: purchaseResult{
						CustomerMessage: "This item is temporarily unavailable.",
						FailureType:     FailureTypeTemporarilyUnavailable,
					},
				}, nil)

			mockPurchaseClient.EXPECT().
				Send(pricingParametersMatcher{"GAME"}).
				Return(http.Result[purchaseResult]{
					StatusCode: 200,
					Data: purchaseResult{
						JingleDocType: "purchaseSuccess",
						Status:        0,
					},
				}, nil)
		})

		It("returns nil", func() {
			err := as.Purchase(PurchaseInput{
				Account: Account{
					StoreFront: "143441",
				},
			})
			Expect(err).ToNot(HaveOccurred())
		})
	})

	When("purchasing the app fails", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockPurchaseClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[purchaseResult]{
					StatusCode: 200,
					Data: purchaseResult{
						JingleDocType: "failure",
						Status:        -1,
					},
				}, nil)
		})

		It("returns nil", func() {
			err := as.Purchase(PurchaseInput{
				Account: Account{
					StoreFront: "143441",
				},
			})
			Expect(err).To(HaveOccurred())
		})
	})
})

type pricingParametersMatcher struct {
	pricingParameters string
}

func (p pricingParametersMatcher) Matches(in interface{}) bool {
	return in.(http.Request).Payload.(*http.XMLPayload).Content["pricingParameters"] == p.pricingParameters
}

func (p pricingParametersMatcher) String() string {
	return "payload pricingParameters is " + p.pricingParameters
}
