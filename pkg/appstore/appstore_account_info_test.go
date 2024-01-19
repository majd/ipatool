package appstore

import (
	"errors"
	"fmt"

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
		const (
			testEmail = "test-email"
			testName  = "test-name"
		)

		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte(fmt.Sprintf("{\"email\": \"%s\", \"name\": \"%s\"}", testEmail, testName)), nil)
		})

		It("returns output", func() {
			out, err := appstore.AccountInfo()
			Expect(err).ToNot(HaveOccurred())
			Expect(out.Account.Email).To(Equal(testEmail))
			Expect(out.Account.Name).To(Equal(testName))
		})
	})

	When("keychain returns error", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte{}, errors.New(""))
		})

		It("returns wrapped error", func() {
			_, err := appstore.AccountInfo()
			Expect(err).To(HaveOccurred())
		})
	})

	When("keychain returns invalid data", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("..."), nil)
		})

		It("fails to unmarshall JSON data", func() {
			_, err := appstore.AccountInfo()
			Expect(err).To(HaveOccurred())
		})
	})
})
