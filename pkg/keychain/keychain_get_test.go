package keychain

import (
	"github.com/99designs/keyring"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
)

var _ = Describe("Keychain (Get)", func() {
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
				Get(testKey).
				Return(keyring.Item{}, testErr)
		})

		It("returns wrapped error", func() {
			data, err := keychain.Get(testKey)
			Expect(err).To(MatchError(ContainSubstring(testErr.Error())))
			Expect(err).To(MatchError(ContainSubstring("failed to get item from keyring")))
			Expect(data).To(BeNil())
		})
	})

	When("keyring returns item", func() {
		const testKey = "test-key"
		var testData = []byte("test")

		BeforeEach(func() {
			mockKeyring.EXPECT().
				Get(testKey).
				Return(keyring.Item{
					Data: testData,
				}, nil)
		})

		It("returns data", func() {
			data, err := keychain.Get(testKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(data).To(Equal(testData))
		})
	})
})
