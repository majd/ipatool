package sap

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/avast/retry-go"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSignerIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SAP Signer Integration Suite")
}

var _ = Describe("SAP signer integration", func() {
	It("signs distinct payloads with distinct signatures", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		var signer ActionSigner

		// Retry live endpoint timeouts with a fresh session, within the overall deadline.
		err := retry.Do(func() error {
			var err error
			signer, err = NewSigner(ctx, Config{
				SetupURL:       "https://fpinit.itunes.apple.com/v1/signSapSetup/legacy",
				CertificateURL: "https://s.mzstatic.com/sap/setupCert.plist",
				Version:        200,
				HardwareID:     []byte{0x02, 0, 0, 0, 0, 1},
			})

			return err
		}, retry.Context(ctx), retry.Attempts(3), retry.RetryIf(func(err error) bool {
			var networkError net.Error

			return errors.As(err, &networkError) && networkError.Timeout()
		}))
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(signer.Close)

		signature, err := signer.Sign([]byte("ipatool SAP smoke test"))
		Expect(err).NotTo(HaveOccurred())
		Expect(signature).NotTo(BeEmpty())

		second, err := signer.Sign([]byte("ipatool second SAP smoke test"))
		Expect(err).NotTo(HaveOccurred())
		Expect(second).NotTo(BeEmpty())
		Expect(second).NotTo(Equal(signature))
	})
})
