package sap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/majd/ipatool/v2/internal/sap/assets"
	"github.com/majd/ipatool/v2/internal/sap/machine"
)

type Signer struct {
	mu       sync.Mutex
	machine  *machine.Machine
	context  uint64
	hardware []byte
	closed   bool
}

func NewSigner(ctx context.Context, config Config) (ActionSigner, error) {
	if ctx == nil {
		return nil, errors.New("SAP signer context is nil")
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("create SAP signer: %w", err)
	}

	if err := validateConfig(config); err != nil {
		return nil, err
	}

	bundle, err := assets.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("load Apple SAP assets: %w", err)
	}

	guest, err := machine.Open(ctx, bundle)
	if err != nil {
		return nil, fmt.Errorf("start Apple SAP runtime: %w", err)
	}

	signer := &Signer{
		machine:  guest,
		hardware: append([]byte(nil), config.HardwareID...),
	}
	complete := false

	defer func() {
		if !complete {
			_ = signer.Close()
		}
	}()

	signer.context, err = guest.Initialize(signer.hardware)
	if err != nil {
		return nil, fmt.Errorf("initialize Apple SAP session: %w", err)
	}

	protocol := setupProtocol{client: &http.Client{Timeout: 30 * time.Second}}

	certificate, err := protocol.certificate(ctx, config.CertificateURL)
	if err != nil {
		return nil, err
	}

	request, state, err := guest.Exchange(config.Version, signer.hardware, signer.context, certificate)
	if err != nil {
		return nil, fmt.Errorf("create SAP setup message: %w", err)
	}

	if state != 1 {
		return nil, fmt.Errorf("SAP setup entered unexpected state %d", state)
	}

	if len(request) == 0 {
		return nil, errors.New("SAP setup message is empty")
	}

	reply, err := protocol.exchange(ctx, config.SetupURL, request)
	if err != nil {
		return nil, err
	}

	_, state, err = guest.Exchange(config.Version, signer.hardware, signer.context, reply)
	if err != nil {
		return nil, fmt.Errorf("complete SAP setup: %w", err)
	}

	if state != 0 {
		return nil, fmt.Errorf("SAP setup completed in unexpected state %d", state)
	}

	complete = true

	return signer, nil
}

func (s *Signer) Sign(input []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, errSignerClosed
	}

	signature, err := s.machine.Sign(s.context, input)
	if err != nil {
		return nil, fmt.Errorf("sign Apple request: %w", err)
	}

	if len(signature) == 0 {
		return nil, errors.New("sign Apple request: signature is empty")
	}

	return signature, nil
}

func (s *Signer) Close() error {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true

	var closeErrors []error
	if s.machine != nil && s.context != 0 {
		closeErrors = append(closeErrors, s.machine.Teardown(s.context))
		s.context = 0
	}

	if s.machine != nil {
		closeErrors = append(closeErrors, s.machine.Close())
		s.machine = nil
	}

	for index := range s.hardware {
		s.hardware[index] = 0
	}

	return errors.Join(closeErrors...)
}
