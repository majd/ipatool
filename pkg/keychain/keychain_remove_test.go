package keychain

import (
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
)

var _ = Describe("Keychain (Remove)", func() {
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
		var testErr = errors.New("test error")

		BeforeEach(func() {
			mockKeyring.EXPECT().
				Remove(testKey).
				Return(testErr)
		})

		It("returns wrapped error", func() {
			err := keychain.Remove(testKey)
			Expect(err).To(MatchError(ContainSubstring(testErr.Error())))
			Expect(err).To(MatchError(ContainSubstring("failed to remove item from keyring")))
		})
	})

	When("keyring does not return error", func() {
		const testKey = "test-key"

		BeforeEach(func() {
			mockKeyring.EXPECT().
				Remove(testKey).
				Return(nil)
		})

		It("returns data", func() {
			err := keychain.Remove(testKey)
			Expect(err).ToNot(HaveOccurred())
		})
	})
})
