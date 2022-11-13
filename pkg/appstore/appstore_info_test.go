package appstore

import (
	"github.com/golang/mock/gomock"
	"github.com/majd/ipatool/pkg/keychain"
	"github.com/majd/ipatool/pkg/log"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
)

var _ = Describe("AppStore (Info)", Ordered, func() {
	var (
		ctrl         *gomock.Controller
		appstore     AppStore
		mockKeychain *keychain.MockKeychain
	)

	BeforeAll(func() {
		log.Logger = log.Output(log.NewWriter()).Level(zerolog.Disabled)
	})

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockKeychain = keychain.NewMockKeychain(ctrl)
		appstore = NewAppStore(&Args{
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
			err := appstore.Info()
			Expect(err).To(MatchError(ContainSubstring(testErr.Error())))
			Expect(err).To(MatchError(ContainSubstring("account was not found")))
		})
	})

	When("keychain returns invalid data", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("..."), nil)
		})

		It("fails to unmarshall JSON data", func() {
			err := appstore.Info()
			Expect(err).To(MatchError(ContainSubstring("failed to unmarshall account data")))
		})
	})

	When("keychain returns valid data", func() {
		BeforeEach(func() {
			mockKeychain.EXPECT().
				Get("account").
				Return([]byte("{}"), nil)
		})

		It("returns nil", func() {
			err := appstore.Info()
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
