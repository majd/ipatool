package assets

import (
	"bytes"
	"crypto/sha256"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("StoreAgent asset profile", func() {
	It("pins the verified Apple payload asset", func() {
		Expect(storeAgentSpec.name).To(Equal("storeagent"))
		Expect(storeAgentSpec.path).To(Equal("./System/Library/PrivateFrameworks/CommerceKit.framework/Versions/A/Resources/storeagent"))
		Expect(storeAgentSpec.size).To(Equal(2580176))
		Expect(storeAgentSpec.digest).To(Equal(mustDigest("70ce036f9dbcbc04db9511ebd08de0dd3cbc35ccc9d44b089c90170cb5453c59")))
	})

	It("rejects an asset with the wrong size", func() {
		Expect(VerifyStoreAgent(nil)).To(MatchError(ContainSubstring("has size 0, expected 2580176")))
	})

	It("rejects an asset with the wrong digest", func() {
		data := bytes.Repeat([]byte{0xa5}, storeAgentSpec.size)
		Expect(sha256.Sum256(data)).NotTo(Equal(storeAgentSpec.digest))
		Expect(VerifyStoreAgent(data)).To(MatchError("apple StoreAgent asset failed integrity verification"))
	})

	It("uses a cache namespace separate from ordinary SAP assets", func() {
		Expect(storeAgentCacheDirectory).NotTo(Equal("apple-assets-v2"))
	})
})
