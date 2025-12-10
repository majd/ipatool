package appstore

import (
	"encoding/json"

	"github.com/majd/ipatool/v2/pkg/keychain"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("AppStore (Revoke)", func() {
	var (
		ctrl         *gomock.Controller
		appstore     AppStore
		mockKeychain *keychain.MockKeychain
	)

	var (
		testEmail = "test-email"
		testName  = "test-name"
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockKeychain = keychain.NewMockKeychain(ctrl)
		appstore = NewAppStore(Args{
			Keychain: mockKeychain,
		})
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	When("keychain removes item", func() {
		BeforeEach(func() {

			var accountStorage = AccountStorage{
				Accounts: []Account{
					{
						Email: testEmail,
						Name:  testName,
					},
				},
				Current: testEmail,
			}

			expectedData, _ := json.Marshal(accountStorage)

			mockKeychain.EXPECT().
				Get(AccountKey).
				Return(expectedData, nil).
				AnyTimes()
			mockKeychain.EXPECT().
				Set(AccountKey, gomock.Any()).
				Return(nil).
				AnyTimes()
		})

		It("returns data", func() {
			err := appstore.Revoke()
			Expect(err).ToNot(HaveOccurred())
		})
	})

	When("keychain returns error", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get(AccountKey).
				Return([]byte("..."), nil).
				AnyTimes()
			mockKeychain.EXPECT().
				Set(AccountKey, gomock.Any()).
				Return(nil).
				AnyTimes()
		})

		It("returns wrapped error", func() {
			err := appstore.Revoke()
			Expect(err).To(HaveOccurred())
		})
	})
})
