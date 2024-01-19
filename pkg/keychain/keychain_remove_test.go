package keychain

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
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
		keychain = New(Args{
			Keyring: mockKeyring,
		})
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	When("keyring returns error", func() {
		const testKey = "test-key"

		BeforeEach(func() {
			mockKeyring.EXPECT().
				Remove(testKey).
				Return(errors.New(""))
		})

		It("returns wrapped error", func() {
			err := keychain.Remove(testKey)
			Expect(err).To(HaveOccurred())
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
