package appstore

import (
	"github.com/golang/mock/gomock"
	"github.com/majd/ipatool/pkg/keychain"
	"github.com/majd/ipatool/pkg/log"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
)

var _ = Describe("AppStore (Info)", func() {
	var (
		ctrl         *gomock.Controller
		appstore     AppStore
		mockKeychain *keychain.MockKeychain
		mockLogger   *log.MockLogger
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockKeychain = keychain.NewMockKeychain(ctrl)
		mockLogger = log.NewMockLogger(ctrl)
		appstore = NewAppStore(AppStoreArgs{
			Keychain: mockKeychain,
			Logger:   mockLogger,
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
			err := appstore.Info()
			Expect(err).To(MatchError(ContainSubstring(ErrUnmarshal.Error())))
		})
	})

	When("keychain returns valid data", func() {
		BeforeEach(func() {
			mockLogger.EXPECT().
				Log().
				Return(nil)

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
