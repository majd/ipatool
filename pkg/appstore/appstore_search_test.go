package appstore

import (
	"github.com/golang/mock/gomock"
	"github.com/majd/ipatool/pkg/http"
	"github.com/majd/ipatool/pkg/keychain"
	"github.com/majd/ipatool/pkg/log"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	"os"
)

var _ = Describe("AppStore (Search)", func() {
	var (
		ctrl         *gomock.Controller
		mockClient   *http.MockClient[SearchResult]
		mockLogger   *log.MockLogger
		mockKeychain *keychain.MockKeychain
		as           AppStore
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockClient = http.NewMockClient[SearchResult](ctrl)
		mockLogger = log.NewMockLogger(ctrl)
		mockKeychain = keychain.NewMockKeychain(ctrl)
		as = &appstore{
			searchClient: mockClient,
			ioReader:     os.Stdin,
			logger:       mockLogger,
			keychain:     mockKeychain,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	When("user is not logged in", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return(nil, ErrGetKeychainItem)
		})

		It("returns error", func() {
			_, err := as.Search("", 0)
			Expect(err).To(MatchError(ContainSubstring(ErrGetAccount.Error())))
		})
	})

	When("country code is invalid", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{}"), nil)
		})

		It("returns error", func() {
			_, err := as.Search("", 0)
			Expect(err).To(MatchError(ContainSubstring(ErrInvalidCountryCode.Error())))
		})
	})

	When("request fails", func() {
		var testErr = errors.New("test")

		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{\"storeFront\":\"143441\"}"), nil)

			mockClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{}, testErr)
		})

		It("returns error", func() {
			_, err := as.Search("", 0)
			Expect(err).To(MatchError(ContainSubstring(testErr.Error())))
			Expect(err).To(MatchError(ContainSubstring(ErrRequest.Error())))
		})
	})

	When("request returns bad status code", func() {
		BeforeEach(func() {
			mockLogger.EXPECT().
				Verbose().
				Return(nil)

			mockClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{
					StatusCode: 400,
				}, nil)

			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{\"storeFront\":\"143441\"}"), nil)
		})

		It("returns error", func() {
			_, err := as.Search("", 0)
			Expect(err).To(MatchError(ContainSubstring(ErrRequest.Error())))
		})
	})

	When("request is successful", func() {
		const (
			testID       = 0
			testBundleID = "test-bundle-id"
			testName     = "test-name"
			testVersion  = "test-version"
			testPrice    = 0.0
		)

		BeforeEach(func() {
			mockClient.EXPECT().
				Send(gomock.Any()).
				Return(http.Result[SearchResult]{
					StatusCode: 200,
					Data: SearchResult{
						Count: 1,
						Results: []App{
							{
								ID:       testID,
								BundleID: testBundleID,
								Name:     testName,
								Version:  testVersion,
								Price:    testPrice,
							},
						},
					},
				}, nil)

			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{\"storeFront\":\"143441\"}"), nil)
		})

		It("returns output", func() {
			out, err := as.Search("", 0)
			Expect(err).ToNot(HaveOccurred())
			Expect(out.Count).To(Equal(1))
			Expect(len(out.Results)).To(Equal(1))
			Expect(out.Results[0]).To(Equal(App{
				ID:       testID,
				BundleID: testBundleID,
				Name:     testName,
				Version:  testVersion,
				Price:    testPrice,
			}))
		})
	})
})
