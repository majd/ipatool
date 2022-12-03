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
		mockLoginClient    *http.MockClient[LoginResult]
		as                 *appstore
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockKeychain = keychain.NewMockKeychain(ctrl)
		mockPurchaseClient = http.NewMockClient[PurchaseResult](ctrl)
		mockSearchClient = http.NewMockClient[SearchResult](ctrl)
		mockLoginClient = http.NewMockClient[LoginResult](ctrl)
		mockMachine = util.NewMockMachine(ctrl)
		mockLogger = log.NewMockLogger(ctrl)
		as = &appstore{
			keychain:       mockKeychain,
			purchaseClient: mockPurchaseClient,
			searchClient:   mockSearchClient,
			loginClient:    mockLoginClient,
			machine:        mockMachine,
			logger:         mockLogger,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	When("fails to read MAC address", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("", ErrGetMAC)
		})

		It("returns error", func() {
			err := as.Purchase("")
			Expect(err).To(MatchError(ContainSubstring(ErrGetMAC.Error())))
		})
	})

	When("not logged in", func() {
		BeforeEach(func() {
			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockLogger.EXPECT().
				Verbose().
				Return(nil)

			mockKeychain.EXPECT().
				Get("account").
				Return(nil, ErrGetKeychainItem)
		})

		It("returns error", func() {
			err := as.Purchase("")
			Expect(err).To(MatchError(ContainSubstring(ErrGetKeychainItem.Error())))
			Expect(err).To(MatchError(ContainSubstring(ErrGetAccount.Error())))
		})
	})

	When("country code is invalid", func() {
		BeforeEach(func() {
			mockLogger.EXPECT().
				Verbose().
				Return(nil)

			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{}"), nil)
		})

		It("returns error", func() {
			err := as.Purchase("")
			Expect(err).To(MatchError(ContainSubstring(ErrInvalidCountryCode.Error())))
		})
	})

	When("app lookup fails", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{\"storeFront\":\"143441\"}"), nil)

			mockSearchClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{}, ErrRequest)

			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockLogger.EXPECT().
				Verbose().
				Return(nil)
		})

		It("returns error", func() {
			err := as.Purchase("")
			Expect(err).To(MatchError(ContainSubstring(ErrAppLookup.Error())))
			Expect(err).To(MatchError(ContainSubstring(ErrRequest.Error())))
		})
	})

	When("app is paid", func() {
		BeforeEach(func() {
			mockLogger.EXPECT().
				Verbose().
				Return(nil)

			mockMachine.EXPECT().
				MacAddress().
				Return("00:00:00:00:00:00", nil)

			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{\"storeFront\":\"143441\"}"), nil)

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
			err := as.Purchase("")
			Expect(err).To(MatchError(ContainSubstring(ErrPaidApp.Error())))
		})
	})

	When("purchase request fails", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{\"storeFront\":\"143441\"}"), nil)

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
				Return(http.Result[PurchaseResult]{}, ErrRequest)

			mockLogger.EXPECT().
				Verbose().
				Return(nil)
		})

		It("returns error", func() {
			err := as.Purchase("")
			Expect(err).To(MatchError(ContainSubstring(ErrRequest.Error())))
		})
	})

	When("password token is expired", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{\"storeFront\":\"143441\"}"), nil)

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
				Verbose().
				Return(nil)
		})

		When("renewing credentials fails", func() {
			BeforeEach(func() {
				mockLoginClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[LoginResult]{
						Data: LoginResult{
							FailureType:     "",
							CustomerMessage: CustomerMessageBadLogin,
						},
					}, nil)

				mockLogger.EXPECT().
					Verbose().
					Return(nil).
					Times(2)
			})

			It("returns error", func() {
				err := as.Purchase("")
				Expect(err).To(MatchError(ContainSubstring(ErrPasswordTokenExpired.Error())))
			})
		})

		When("renewing credentials succeeds", func() {
			BeforeEach(func() {
				mockLoginClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[LoginResult]{
						Data: LoginResult{},
					}, nil)

				mockKeychain.EXPECT().
					Get("account").
					Return([]byte("{\"storeFront\":\"143441\"}"), nil)

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

				mockLogger.EXPECT().
					Verbose().
					Return(nil).
					Times(2)

				mockKeychain.EXPECT().
					Set("account", gomock.Any()).
					Return(nil)

				mockPurchaseClient.EXPECT().
					Send(gomock.Any()).
					Return(http.Result[PurchaseResult]{
						Data: PurchaseResult{
							FailureType: FailureTypePasswordTokenExpired,
						},
					}, nil)
			})

			It("attempts to purcahse app", func() {
				err := as.Purchase("")
				Expect(err).To(MatchError(ContainSubstring(ErrPasswordTokenExpired.Error())))
			})
		})
	})

	When("store API returns customer error message", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{\"storeFront\":\"143441\"}"), nil)

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
				Verbose().
				Return(nil).
				Times(2)
		})

		It("returns error", func() {
			err := as.Purchase("")
			Expect(err).To(MatchError(ContainSubstring(CustomerMessageBadLogin)))
		})
	})

	When("store API returns unknown error", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{\"storeFront\":\"143441\"}"), nil)

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
				Verbose().
				Return(nil).
				Times(2)
		})

		It("returns error", func() {
			err := as.Purchase("")
			Expect(err).To(MatchError(ContainSubstring(ErrGeneric.Error())))
		})
	})

	When("account already has a license for the app", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{\"storeFront\":\"143441\"}"), nil)

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
				Verbose().
				Return(nil)
		})

		It("returns error", func() {
			err := as.Purchase("")
			Expect(err).To(MatchError(ContainSubstring(ErrLicenseExists.Error())))
		})
	})

	When("sucessfully purchases the app", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{\"storeFront\":\"143441\"}"), nil)

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
					Data: PurchaseResult{
						JingleDocType: "purchaseSuccess",
						Status:        0,
					},
				}, nil)

			mockLogger.EXPECT().
				Verbose().
				Return(nil)

			mockLogger.EXPECT().
				Log().
				Return(nil)
		})

		It("returns nil", func() {
			err := as.Purchase("")
			Expect(err).ToNot(HaveOccurred())
		})
	})

	When("purchasing the app fails", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{\"storeFront\":\"143441\"}"), nil)

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
					Data: PurchaseResult{
						JingleDocType: "failure",
						Status:        -1,
					},
				}, nil)

			mockLogger.EXPECT().
				Verbose().
				Return(nil).
				Times(2)
		})

		It("returns nil", func() {
			err := as.Purchase("")
			Expect(err).To(MatchError(ContainSubstring(ErrPurchase.Error())))
		})
	})
})
