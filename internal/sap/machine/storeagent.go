package machine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/majd/ipatool/v2/internal/sap/assets"
)

const (
	storeAgentBase         = uint64(0x00001000c0000000)
	storeAgentGlobalInit   = storeAgentBase + 0x0c5fc0
	storeAgentSessionInit  = storeAgentBase + 0x0debd0
	storeAgentDecryptEntry = storeAgentBase + 0x0ee700
	storeAgentSessionClose = storeAgentBase + 0x1212d0
	storeAgentChunkSize    = 0x8000
	storeAgentSCInfoPath   = "/Users/Shared/SC Info"
)

var storeAgentZeroReturnAliases = []string{
	"_pthread_rwlock_rdlock",
	"_pthread_rwlock_rdlock$UNIX2003",
	"_pthread_mutex_init",
	"_pthread_mutex_destroy",
	"_pthread_rwlock_destroy",
}

type StoreAgent struct {
	mu           sync.Mutex
	guest        *Machine
	session      uint64
	decryptEntry uint64
	closeEntry   uint64
	closed       bool
}

func OpenStoreAgent(ctx context.Context, bundle assets.Bundle, hardwareID, dpInfo []byte) (*StoreAgent, error) {
	if ctx == nil {
		return nil, errors.New("StoreAgent context is nil")
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("open StoreAgent runtime: %w", err)
	}

	image, err := assets.LoadStoreAgent(ctx)
	if err != nil {
		return nil, fmt.Errorf("load Apple StoreAgent asset: %w", err)
	}

	return openStoreAgent(ctx, bundle, image, hardwareID, dpInfo)
}

func openStoreAgent(ctx context.Context, bundle assets.Bundle, image, hardwareID, dpInfo []byte) (*StoreAgent, error) {
	if ctx == nil {
		return nil, errors.New("StoreAgent context is nil")
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("open StoreAgent runtime: %w", err)
	}

	if len(dpInfo) == 0 {
		return nil, errors.New("StoreAgent dpInfo is empty")
	}

	if err := assets.VerifyStoreAgent(image); err != nil {
		return nil, fmt.Errorf("verify Apple StoreAgent profile: %w", err)
	}

	hardware, err := hardwareBlock(hardwareID)
	if err != nil {
		return nil, err
	}

	guest, _, err := openRuntime(ctx, bundle, runtimeOptions{
		extraImages: []imageSpec{{name: "storeagent", data: image, base: storeAgentBase}},
		shims: shimOptions{
			zeroReturnAliases: storeAgentZeroReturnAliases,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("start StoreAgent runtime: %w", err)
	}

	agent := &StoreAgent{
		guest:        guest,
		decryptEntry: storeAgentDecryptEntry,
		closeEntry:   storeAgentSessionClose,
	}
	complete := false

	defer func() {
		if !complete {
			_ = agent.Close()
		}
	}()

	globalContext, err := agent.initializeGlobal(hardware)
	if err != nil {
		return nil, err
	}

	agent.session, err = agent.initializeSession(globalContext, dpInfo)
	if err != nil {
		return nil, err
	}

	complete = true

	return agent, nil
}

func (s *StoreAgent) initializeGlobal(hardware []byte) (uint32, error) {
	s.guest.beginCall()
	defer s.guest.clearScratch()

	hardwareAddress, err := s.guest.scratch(hardware, uint64(len(hardware)))
	if err != nil {
		return 0, err
	}

	path := append([]byte(storeAgentSCInfoPath), 0)
	pathAddress, err := s.guest.scratch(path, uint64(len(path)))

	if err != nil {
		return 0, err
	}

	contextField, err := s.guest.scratch(nil, 4)
	if err != nil {
		return 0, err
	}

	status, err := s.guest.invoke(storeAgentGlobalInit, 0, hardwareAddress, pathAddress, contextField)
	if err != nil {
		return 0, fmt.Errorf("initialize StoreAgent global context: %w", err)
	}

	if int32(status) != 0 {
		return 0, fmt.Errorf("StoreAgent global initialization returned %d", int32(status))
	}

	value, err := s.guest.readUint32(contextField)
	if err != nil {
		return 0, err
	}

	if value == 0 {
		return 0, errors.New("StoreAgent global initialization returned a null context")
	}

	return value, nil
}

func (s *StoreAgent) initializeSession(globalContext uint32, dpInfo []byte) (uint64, error) {
	s.guest.beginCall()
	defer s.guest.clearScratch()

	dpInfoAddress, err := s.guest.scratch(dpInfo, uint64(len(dpInfo)))
	if err != nil {
		return 0, err
	}

	sessionField, err := s.guest.scratch(nil, 8)
	if err != nil {
		return 0, err
	}

	status, err := s.guest.invoke(
		storeAgentSessionInit,
		uint64(globalContext),
		dpInfoAddress,
		uint64(len(dpInfo)),
		sessionField,
	)
	if err != nil {
		return 0, fmt.Errorf("initialize StoreAgent session: %w", err)
	}

	if int32(status) != 0 {
		return 0, fmt.Errorf("StoreAgent session initialization returned %d", int32(status))
	}

	session, err := s.guest.readUint64(sessionField)
	if err != nil {
		return 0, err
	}

	if session == 0 {
		return 0, errors.New("StoreAgent session initialization returned a null session")
	}

	return session, nil
}

func (s *StoreAgent) Decrypt(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	if s == nil {
		return 0, errors.New("StoreAgent is nil")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return 0, errors.New("StoreAgent is closed")
	}

	if ctx == nil {
		return 0, errors.New("StoreAgent decryption context is nil")
	}

	if dst == nil {
		return 0, errors.New("StoreAgent decryption destination is nil")
	}

	if src == nil {
		return 0, errors.New("StoreAgent decryption source is nil")
	}

	return decryptStream(ctx, dst, src, s.decryptChunk)
}

func (s *StoreAgent) decryptChunk(data []byte) error {
	s.guest.beginCall()
	defer s.guest.clearScratch()

	address, err := s.guest.scratch(data, uint64(len(data)))
	if err != nil {
		return err
	}

	status, err := s.guest.invoke(
		s.decryptEntry,
		s.session,
		address,
		uint64(len(data)),
		address,
		0,
	)
	if err != nil {
		return fmt.Errorf("decrypt StoreAgent chunk: %w", err)
	}

	if int32(status) != 0 {
		return fmt.Errorf("StoreAgent decryption returned %d", int32(status))
	}

	if err := s.guest.engine.MemReadInto(data, address); err != nil {
		return fmt.Errorf("read decrypted StoreAgent chunk: %w", err)
	}

	return nil
}

func decryptStream(ctx context.Context, dst io.Writer, src io.Reader, decrypt func([]byte) error) (int64, error) {
	buffer := make([]byte, storeAgentChunkSize)

	var written int64

	for {
		if err := ctx.Err(); err != nil {
			return written, fmt.Errorf("decrypt StoreAgent stream: %w", err)
		}

		count, readErr := io.ReadFull(src, buffer)
		if count != 0 {
			chunk := buffer[:count]
			if err := decrypt(chunk); err != nil {
				return written, err
			}

			n, err := dst.Write(chunk)
			written += int64(n)

			if err != nil {
				return written, fmt.Errorf("write decrypted StoreAgent stream: %w", err)
			}

			if n != len(chunk) {
				return written, fmt.Errorf("write decrypted StoreAgent stream: %w", io.ErrShortWrite)
			}
		}

		switch {
		case readErr == nil:
			continue
		case errors.Is(readErr, io.EOF), errors.Is(readErr, io.ErrUnexpectedEOF):
			return written, nil
		default:
			return written, fmt.Errorf("read encrypted StoreAgent stream: %w", readErr)
		}
	}
}

func (s *StoreAgent) Close() error {
	if s == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true

	var errs []error

	if s.guest != nil && s.session != 0 {
		session := s.session
		s.session = 0

		status, err := s.guest.invoke(s.closeEntry, session)
		if err != nil {
			errs = append(errs, fmt.Errorf("close StoreAgent session: %w", err))
		} else if int32(status) != 0 {
			errs = append(errs, fmt.Errorf("StoreAgent session close returned %d", int32(status)))
		}
	}

	if s.guest != nil {
		errs = append(errs, s.guest.Close())
		s.guest = nil
	}

	return errors.Join(errs...)
}
