package sap

import (
	"errors"
	"testing"
)

func TestClosedSigner(t *testing.T) {
	signer := &Signer{closed: true}
	if _, err := signer.Sign([]byte("message")); !errors.Is(err, errSignerClosed) {
		t.Fatalf("Sign error = %v, want %v", err, errSignerClosed)
	}

	if err := signer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
