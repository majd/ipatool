package appstore

import (
	"errors"

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
			mockKeychain.EXPECT().
				Remove("account").
				Return(nil)
		})

		It("returns data", func() {
			err := appstore.Revoke()
			Expect(err).ToNot(HaveOccurred())
		})
	})

	When("keychain returns error", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Remove("account").
				Return(errors.New(""))
		})

		It("returns wrapped error", func() {
			err := appstore.Revoke()
			Expect(err).To(HaveOccurred())
		})
	})
})
