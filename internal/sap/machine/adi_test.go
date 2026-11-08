package machine

import (
	"encoding/binary"
	"encoding/hex"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("portable IOAES", func() {
	DescribeTable("evaluates FairPlay hardware handles with the emulated key",
		func(handle uint32) {
			keyData := make([]byte, 40)
			binary.LittleEndian.PutUint32(keyData[36:40], handle)
			plaintext := make([]byte, 16)
			iv := make([]byte, 16)
			expected, err := hex.DecodeString("66e94bd4ef8a2c3b884cfa59ca342b2e")
			Expect(err).NotTo(HaveOccurred())

			ciphertext, outputIV, err := emulateIOAES(plaintext, iv, 0, keyData)
			Expect(err).NotTo(HaveOccurred())
			Expect(ciphertext).To(Equal(expected))
			Expect(outputIV).To(Equal(expected))

			decrypted, outputIV, err := emulateIOAES(ciphertext, iv, 1, keyData)
			Expect(err).NotTo(HaveOccurred())
			Expect(decrypted).To(Equal(plaintext))
			Expect(outputIV).To(Equal(ciphertext))
		},
		Entry("commerce", commerceAESHandle),
	)

	It("rejects unrelated key handles", func() {
		keyData := make([]byte, 40)
		binary.LittleEndian.PutUint32(keyData[36:40], 1)
		_, _, err := emulateIOAES(make([]byte, 16), make([]byte, 16), 0, keyData)
		Expect(err).To(MatchError("unsupported IOAES key handle 0x1"))
	})
})
