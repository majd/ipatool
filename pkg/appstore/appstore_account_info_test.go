package appstore

import (
	"encoding/json"
	"errors"

	"github.com/majd/ipatool/v2/pkg/http"
	"github.com/majd/ipatool/v2/pkg/keychain"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("AppStore (AccountInfo)", func() {
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

	When("keychain returns valid data", func() {

		BeforeEach(func() {
			var defaultAccount = Account{
				Email: testEmail,
				Name:  testName,
			}
			var expectedResult, _ = json.Marshal(defaultAccount)
			mockKeychain.EXPECT().
				Get(AccountKey).
				Return(expectedResult, nil).
				AnyTimes()
		})

		It("returns output", func() {
			out, err := appstore.AccountInfo()
			Expect(err).ToNot(HaveOccurred())
			Expect(out.Account.Email).To(Equal(testEmail))
			Expect(out.Account.Name).To(Equal(testName))
		})
	})

	When("keychain returns new version valid data", func() {
		BeforeEach(func() {
			var defaultAccount = Account{
				Email: testEmail,
				Name:  testName,
			}
			var accountStorage = AccountStorage{
				Current:  testEmail,
				Accounts: []Account{defaultAccount},
			}
			var expectedResult, _ = json.Marshal(accountStorage)
			mockKeychain.EXPECT().
				Get(AccountKey).
				Return(expectedResult, nil).
				AnyTimes()
		})
	})

	When("switching accounts", func() {
		var mockCookieJar *http.MockCookieJar

		BeforeEach(func() {
			mockCookieJar = http.NewMockCookieJar(ctrl)
			appstore = NewAppStore(Args{
				Keychain:  mockKeychain,
				CookieJar: mockCookieJar,
			})

			accountStorage := AccountStorage{
				Current: testEmail,
				Accounts: []Account{
					{Email: testEmail, Name: testName},
					{Email: "other@example.com", Name: "Other"},
				},
			}
			storageData, err := json.Marshal(accountStorage)
			Expect(err).ToNot(HaveOccurred())

			mockKeychain.EXPECT().
				Get(AccountKey).
				Return(storageData, nil).
				AnyTimes()
			mockKeychain.EXPECT().
				Set(AccountKey, gomock.Any()).
				Return(nil)
			mockCookieJar.EXPECT().
				RemoveAll()
			mockCookieJar.EXPECT().
				Save().
				Return(nil)
		})

		It("resets session cookies before switching", func() {
			_, err := appstore.SwitchAccount("other@example.com")
			Expect(err).ToNot(HaveOccurred())
		})
	})

	When("keychain returns error", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get(AccountKey).
				Return([]byte{}, errors.New("")).
				AnyTimes()

		})

		It("returns wrapped error", func() {
			_, err := appstore.AccountInfo()
			Expect(err).To(HaveOccurred())
		})
	})

	When("keychain returns invalid data", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get(AccountKey).
				Return([]byte("..."), nil).
				AnyTimes()
		})

		It("fails to unmarshall JSON data", func() {
			_, err := appstore.AccountInfo()
			Expect(err).To(HaveOccurred())
		})
	})
})
