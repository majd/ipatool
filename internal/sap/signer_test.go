package sap

import (
	"context"
	"errors"
	"testing"
)

func TestNewSignerRejectsInvalidContext(t *testing.T) {
	config := Config{
		SetupURL:       "https://example.apple.com/setup",
		CertificateURL: "https://example.apple.com/certificate",
		Version:        200,
		HardwareID:     []byte{1, 2, 3, 4, 5, 6},
	}
	//nolint:staticcheck // This test verifies that a nil context is rejected.
	if _, err := NewSigner(nil, config); err == nil {
		t.Fatal("NewSigner accepted a nil context")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := NewSigner(ctx, config); !errors.Is(err, context.Canceled) {
		t.Fatalf("NewSigner error = %v, want %v", err, context.Canceled)
	}
}

func TestValidateConfig(t *testing.T) {
	valid := Config{
		SetupURL:       "https://example.apple.com/setup",
		CertificateURL: "https://example.apple.com/certificate",
		Version:        200,
		HardwareID:     []byte{1, 2, 3, 4, 5, 6},
	}
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"version", func(config *Config) { config.Version = 210 }},
		{"empty hardware ID", func(config *Config) { config.HardwareID = nil }},
		{"long hardware ID", func(config *Config) { config.HardwareID = make([]byte, 21) }},
		{"relative setup URL", func(config *Config) { config.SetupURL = "/setup" }},
		{"HTTP certificate URL", func(config *Config) { config.CertificateURL = "http://example.apple.com/cert" }},
		{"credentialed URL", func(config *Config) { config.SetupURL = "https://user@example.apple.com/setup" }},
	}

	if err := validateConfig(valid); err != nil {
		t.Fatalf("valid config: %v", err)
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := valid
			test.mutate(&config)

			if err := validateConfig(config); err == nil {
				t.Fatal("validateConfig accepted invalid config")
			}
		})
	}
}
