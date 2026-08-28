package sap

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestSignerIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	signer, err := NewSigner(ctx, Config{
		SetupURL:       "https://fpinit.itunes.apple.com/v1/signSapSetup/legacy",
		CertificateURL: "https://s.mzstatic.com/sap/setupCert.plist",
		Version:        200,
		HardwareID:     []byte{0x02, 0, 0, 0, 0, 1},
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := signer.Close(); err != nil {
			t.Errorf("close signer: %v", err)
		}
	})

	signature, err := signer.Sign([]byte("ipatool SAP smoke test"))
	if err != nil {
		t.Fatal(err)
	}

	if len(signature) == 0 {
		t.Fatal("signature is empty")
	}

	second, err := signer.Sign([]byte("ipatool second SAP smoke test"))
	if err != nil {
		t.Fatal(err)
	}

	if len(second) == 0 || bytes.Equal(signature, second) {
		t.Fatal("second signature is empty or unexpectedly reused")
	}
}
