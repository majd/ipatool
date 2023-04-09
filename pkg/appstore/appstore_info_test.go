package appstore

import (
	"fmt"
	"github.com/golang/mock/gomock"
	"github.com/majd/ipatool/pkg/keychain"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
)

var _ = Describe("AppStore (Info)", func() {
	var (
		ctrl         *gomock.Controller
		appstore     AppStore
		mockKeychain *keychain.MockKeychain
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockKeychain = keychain.NewMockKeychain(ctrl)
		appstore = NewAppStore(AppStoreArgs{
			Keychain: mockKeychain,
		})
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	When("keychain returns error", func() {
		var testErr = errors.New("test error")

		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte{}, testErr)
		})

		It("returns wrapped error", func() {
			_, err := appstore.Info()
			Expect(err).To(MatchError(ContainSubstring(testErr.Error())))
			Expect(err).To(MatchError(ContainSubstring(ErrGetAccount.Error())))
		})
	})

	When("keychain returns invalid data", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("..."), nil)
		})

		It("fails to unmarshall JSON data", func() {
			_, err := appstore.Info()
			Expect(err).To(MatchError(ContainSubstring(ErrUnmarshal.Error())))
		})
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
			out, err := appstore.Info()
			Expect(err).ToNot(HaveOccurred())
			Expect(out.Email).To(Equal(testEmail))
			Expect(out.Name).To(Equal(testName))
		})
	})
})
