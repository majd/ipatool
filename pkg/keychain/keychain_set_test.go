package keychain

import (
	"github.com/99designs/keyring"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
)

var _ = Describe("Keychain (Set)", func() {
	var (
		ctrl        *gomock.Controller
		keychain    Keychain
		mockKeyring *MockKeyring
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockKeyring = NewMockKeyring(ctrl)
		keychain = NewKeychain(KeychainArgs{
			Keyring: mockKeyring,
		})
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	When("keyring returns error", func() {
		const testKey = "test-key"
		var (
			testData = []byte("test")
			testErr  = errors.New("test error")
		)

		BeforeEach(func() {
			mockKeyring.EXPECT().
				Set(keyring.Item{
					Key:  testKey,
					Data: testData,
				}).
				Return(testErr)
		})

		It("returns wrapped error", func() {
			err := keychain.Set(testKey, testData)
			Expect(err).To(MatchError(ContainSubstring(testErr.Error())))
			Expect(err).To(MatchError(ContainSubstring("failed to set item in keyring")))
		})
	})

	When("keyring does not return error", func() {
		const testKey = "test-key"
		var testData = []byte("test")

		BeforeEach(func() {
			mockKeyring.EXPECT().
				Set(keyring.Item{
					Key:  testKey,
					Data: testData,
				}).
				Return(nil)
		})

		It("returns nil", func() {
			err := keychain.Set(testKey, testData)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
