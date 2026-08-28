package appstore

import (
	"context"
	"fmt"

	"github.com/majd/ipatool/v2/internal/sap"
	"github.com/majd/ipatool/v2/pkg/http"
)

type SAPConfig struct {
	AuthEndpoint   string
	SetupURL       string
	CertificateURL string
	Version        uint32
}

type ActionSigner interface {
	http.ActionSigner
	Close() error
}

type ActionSignerFactory func(config SAPConfig, machineID []byte) (ActionSigner, error)

func defaultActionSignerFactory(config SAPConfig, machineID []byte) (ActionSigner, error) {
	signer, err := sap.NewSigner(context.Background(), sap.Config{
		SetupURL:       config.SetupURL,
		CertificateURL: config.CertificateURL,
		Version:        config.Version,
		HardwareID:     machineID,
	})
	if err != nil {
		return nil, fmt.Errorf("create SAP signer: %w", err)
	}

	return signer, nil
}
