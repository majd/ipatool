package cmd

import (
	"fmt"
	"time"

	"github.com/byteness/keyring"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("iOS keyring", func() {
	It("persists and updates credentials without invalid SecItemUpdate parameters", func() {
		ring, err := openKeyring(keyring.Config{
			AllowedBackends: []keyring.BackendType{keyring.KeychainBackend},
			ServiceName:     fmt.Sprintf("ipatool-test-%d", time.Now().UnixNano()),
		})
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() { Expect(ring.Remove("account")).To(Succeed()) })
		Expect(ring.Set(keyring.Item{Key: "account", Data: []byte("first"), Label: "ipatool"})).To(Succeed())
		Expect(ring.Set(keyring.Item{Key: "account", Data: []byte("updated"), Label: "ipatool"})).To(Succeed())
		item, err := ring.Get("account")
		Expect(err).NotTo(HaveOccurred())
		Expect(item.Data).To(Equal([]byte("updated")))
		Expect(item.Label).To(Equal("ipatool"))
	})
})
