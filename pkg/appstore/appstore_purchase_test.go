package appstore

import (
	"github.com/golang/mock/gomock"
	"github.com/majd/ipatool/pkg/http"
	"github.com/majd/ipatool/pkg/keychain"
	"github.com/majd/ipatool/pkg/log"
	"github.com/majd/ipatool/pkg/util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("AppStore (Purchase)", func() {
	var (
		ctrl               *gomock.Controller
		mockKeychain       *keychain.MockKeychain
		mockMachine        *util.MockMachine
		mockLogger         *log.MockLogger
		mockPurchaseClient *http.MockClient[PurchaseResult]
		mockSearchClient   *http.MockClient[SearchResult]
		as                 *appstore
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockKeychain = keychain.NewMockKeychain(ctrl)
		mockPurchaseClient = http.NewMockClient[PurchaseResult](ctrl)
		mockSearchClient = http.NewMockClient[SearchResult](ctrl)
		mockMachine = util.NewMockMachine(ctrl)
		mockLogger = log.NewMockLogger(ctrl)
		as = &appstore{
			keychain:       mockKeychain,
			purchaseClient: mockPurchaseClient,
			searchClient:   mockSearchClient,
			machine:        mockMachine,
			logger:         mockLogger,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	When("not logged in", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return(nil, ErrorKeychainGet)
		})

		It("returns error", func() {
			err := as.Purchase("", "", "")
			Expect(err).To(MatchError(ContainSubstring(ErrorKeychainGet.Error())))
			Expect(err).To(MatchError(ContainSubstring(ErrorReadAccount.Error())))
		})
	})

	When("country code is invalid", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{}"), nil)
		})

		It("returns error", func() {
			err := as.Purchase("", "XYZ", "")
			Expect(err).To(MatchError(ContainSubstring(ErrorInvalidCountryCode.Error())))
		})
	})

	When("app lookup fails", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{}"), nil)

			mockSearchClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{}, ErrorRequest)
		})

		It("returns error", func() {
			err := as.Purchase("", "US", DeviceFamilyPhone)
			Expect(err).To(MatchError(ContainSubstring(ErrorReadApp.Error())))
			Expect(err).To(MatchError(ContainSubstring(ErrorRequest.Error())))
		})
	})

	When("app is paid", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{}"), nil)

			mockSearchClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{
					StatusCode: 200,
					Data: SearchResult{
						Count: 1,
						Results: []App{
							{
								Price: 0.99,
							},
						},
					},
				}, nil)
		})

		It("returns error", func() {
			err := as.Purchase("", "US", DeviceFamilyPhone)
			Expect(err).To(MatchError(ContainSubstring(ErrorAppPaid.Error())))
		})
	})

	When("fails to read MAC address", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{}"), nil)

			mockSearchClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{
					StatusCode: 200,
					Data: SearchResult{
						Count: 1,
						Results: []App{
							{},
						},
					},
				}, nil)

			mockMachine.EXPECT().
				MacAddress().
				Return("", ErrorReadMAC)
		})

		It("returns error", func() {
			err := as.Purchase("", "US", DeviceFamilyPhone)
			Expect(err).To(MatchError(ContainSubstring(ErrorReadMAC.Error())))
		})
	})

	When("purchase request fails", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{}"), nil)

			mockSearchClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{
					StatusCode: 200,
					Data: SearchResult{
						Count: 1,
						Results: []App{
							{
								ID:       0,
								BundleID: "",
								Name:     "",
								Version:  "",
								Price:    0,
							},
						},
					},
				}, nil)

			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockPurchaseClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[PurchaseResult]{}, ErrorRequest)

			mockLogger.EXPECT().
				Debug().
				Return(nil)
		})

		It("returns error", func() {
			err := as.Purchase("", "US", DeviceFamilyPhone)
			Expect(err).To(MatchError(ContainSubstring(ErrorRequest.Error())))
		})
	})

	When("password token is expired", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{}"), nil)

			mockSearchClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{
					StatusCode: 200,
					Data: SearchResult{
						Count: 1,
						Results: []App{
							{
								ID:       0,
								BundleID: "",
								Name:     "",
								Version:  "",
								Price:    0,
							},
						},
					},
				}, nil)

			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockPurchaseClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[PurchaseResult]{
					Data: PurchaseResult{
						FailureType: FailureTypePasswordTokenExpired,
					},
				}, nil)

			mockLogger.EXPECT().
				Debug().
				Return(nil)
		})

		It("returns error", func() {
			err := as.Purchase("", "US", DeviceFamilyPhone)
			Expect(err).To(MatchError(ContainSubstring(ErrorPasswordTokenExpired.Error())))
		})
	})

	When("store API returns customer error message", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{}"), nil)

			mockSearchClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{
					StatusCode: 200,
					Data: SearchResult{
						Count: 1,
						Results: []App{
							{
								ID:       0,
								BundleID: "",
								Name:     "",
								Version:  "",
								Price:    0,
							},
						},
					},
				}, nil)

			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockPurchaseClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[PurchaseResult]{
					Data: PurchaseResult{
						FailureType:     "failure",
						CustomerMessage: CustomerMessageBadLogin,
					},
				}, nil)

			mockLogger.EXPECT().
				Debug().
				Return(nil).
				Times(2)
		})

		It("returns error", func() {
			err := as.Purchase("", "US", DeviceFamilyPhone)
			Expect(err).To(MatchError(ContainSubstring(CustomerMessageBadLogin)))
		})
	})

	When("store API returns unknown error", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{}"), nil)

			mockSearchClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{
					StatusCode: 200,
					Data: SearchResult{
						Count: 1,
						Results: []App{
							{
								ID:       0,
								BundleID: "",
								Name:     "",
								Version:  "",
								Price:    0,
							},
						},
					},
				}, nil)

			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockPurchaseClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[PurchaseResult]{
					Data: PurchaseResult{
						FailureType: "failure",
					},
				}, nil)

			mockLogger.EXPECT().
				Debug().
				Return(nil).
				Times(2)
		})

		It("returns error", func() {
			err := as.Purchase("", "US", DeviceFamilyPhone)
			Expect(err).To(MatchError(ContainSubstring(ErrorGeneric.Error())))
		})
	})

	When("account already has a license for the app", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{}"), nil)

			mockSearchClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{
					StatusCode: 200,
					Data: SearchResult{
						Count: 1,
						Results: []App{
							{
								ID:       0,
								BundleID: "",
								Name:     "",
								Version:  "",
								Price:    0,
							},
						},
					},
				}, nil)

			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockPurchaseClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[PurchaseResult]{
					StatusCode: 500,
					Data:       PurchaseResult{},
				}, nil)

			mockLogger.EXPECT().
				Debug().
				Return(nil)
		})

		It("returns error", func() {
			err := as.Purchase("", "US", DeviceFamilyPhone)
			Expect(err).To(MatchError(ContainSubstring(ErrorLicenseExists.Error())))
		})
	})

	When("sucessfully purchases the app", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{}"), nil)

			mockSearchClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{
					StatusCode: 200,
					Data: SearchResult{
						Count: 1,
						Results: []App{
							{
								ID:       0,
								BundleID: "",
								Name:     "",
								Version:  "",
								Price:    0,
							},
						},
					},
				}, nil)

			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockPurchaseClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[PurchaseResult]{
					StatusCode: 200,
					Data:       PurchaseResult{},
				}, nil)

			mockLogger.EXPECT().
				Debug().
				Return(nil)

			mockLogger.EXPECT().
				Info().
				Return(nil)
		})

		It("returns nil", func() {
			err := as.Purchase("", "US", DeviceFamilyPhone)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
