package storeauth

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestStoreAuthentication(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Store Authentication")
}

var _ = Describe("local anisette state", func() {
	It("keeps TLS verification enabled for Apple authentication", func() {
		transport, ok := NewHTTPClient(nil).Transport.(*http.Transport)
		Expect(ok).To(BeTrue())
		Expect(transport.TLSClientConfig.InsecureSkipVerify).To(BeFalse())
		Expect(transport.TLSClientConfig.MinVersion).To(Equal(uint16(tls.VersionTLS12)))
	})

	It("creates a fresh software identity", func() {
		state, err := decodeAnisetteState(nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(state.Identifier).To(HaveLen(16))
		Expect(state.Provisioning).To(BeEmpty())
		Expect(state.RoutingInfo).To(BeZero())
	})

	It("rejects oversized persisted provisioning", func() {
		encoded, err := json.Marshal(anisetteState{Identifier: make([]byte, 16), Provisioning: make([]byte, anisetteLimit+1)})
		Expect(err).NotTo(HaveOccurred())
		_, err = decodeAnisetteState(encoded)
		Expect(err).To(MatchError("invalid persisted anisette state"))
	})

	It("builds the complete Apple header set", func() {
		state := &anisetteState{Identifier: []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}, RoutingInfo: 123}
		headers := anisetteHeaders(state, []byte("otp"), []byte("mid"))
		Expect(headers.Get("X-Apple-I-MD")).To(Equal("b3Rw"))
		Expect(headers.Get("X-Apple-I-MD-M")).To(Equal("bWlk"))
		Expect(headers.Get("X-Apple-I-MD-RINFO")).To(Equal("123"))
		Expect(headers.Get("X-Mme-Device-Id")).To(Equal("00010203-0405-0607-0809-0a0b0c0d0e0f"))
		Expect(headers.Get("X-MMe-Client-Info")).To(Equal(anisetteClientInfo))
	})
})
