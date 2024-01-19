package keychain

import (
	"errors"

	"github.com/99designs/keyring"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
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
		keychain = New(Args{
			Keyring: mockKeyring,
		})
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	When("keyring returns error", func() {
		const testKey = "test-key"
		var testData = []byte("test")

		BeforeEach(func() {
			mockKeyring.EXPECT().
				Set(keyring.Item{
					Key:  testKey,
					Data: testData,
				}).
				Return(errors.New(""))
		})

		It("returns wrapped error", func() {
			err := keychain.Set(testKey, testData)
			Expect(err).To(HaveOccurred())
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
