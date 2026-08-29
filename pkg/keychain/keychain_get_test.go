package keychain

import (
	"errors"

	"github.com/byteness/keyring"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("Keychain (Get)", func() {
	const testLabel = "test-label"

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
			Label:   testLabel,
		})
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	When("keyring returns error", func() {
		const testKey = "test-key"

		BeforeEach(func() {
			mockKeyring.EXPECT().
				Get(testKey).
				Return(keyring.Item{}, errors.New(""))
		})

		It("returns wrapped error", func() {
			data, err := keychain.Get(testKey)
			Expect(err).To(HaveOccurred())
			Expect(data).To(BeNil())
		})
	})

	When("keyring returns a legacy unlabeled item", func() {
		const testKey = "test-key"
		var testData = []byte("test")

		BeforeEach(func() {
			mockKeyring.EXPECT().
				Get(testKey).
				Return(keyring.Item{
					Data:  testData,
					Label: "",
				}, nil)
		})

		It("returns data", func() {
			data, err := keychain.Get(testKey)
			Expect(err).ToNot(HaveOccurred())
			Expect(data).To(Equal(testData))
		})
	})
})
