package machine

import (
	"context"
	"errors"
	"fmt"

	"github.com/majd/ipatool/v2/internal/sap/assets"
)

// GenerateKBSync creates the account and hardware bound FairPlay data required
// by the bag's ent/download endpoint, without opening a decryption session.
func GenerateKBSync(ctx context.Context, bundle assets.Bundle, hardwareID []byte, dsid uint64) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("StoreAgent context is nil")
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("generate kbsync: %w", err)
	}

	if dsid == 0 {
		return nil, errors.New("kbsync requires a nonzero account DSID")
	}

	image, err := assets.LoadStoreAgent(ctx)
	if err != nil {
		return nil, fmt.Errorf("load Apple StoreAgent asset: %w", err)
	}

	agent, globalContext, err := openStoreAgentGlobal(ctx, bundle, image, hardwareID)
	if err != nil {
		return nil, err
	}
	defer agent.Close()

	return agent.guest.generateKBSync(storeAgentKBSyncEntry, globalContext, dsid)
}

func (m *Machine) generateKBSync(entry uint64, globalContext uint32, dsid uint64) ([]byte, error) {
	m.beginCall()
	defer m.clearScratch()

	pointerField, err := m.scratch(nil, 8)
	if err != nil {
		return nil, err
	}

	// StoreAgent writes a uint32 length. Zero the upper half so consumeOutput
	// can share the SAP buffer bounds checking and storage disposal.
	lengthField, err := m.scratch(nil, 8)
	if err != nil {
		return nil, err
	}

	status, err := m.invoke(entry, uint64(globalContext), dsid, 0, 1, pointerField, lengthField)
	if err != nil {
		return nil, fmt.Errorf("generate StoreAgent kbsync: %w", err)
	}

	output, outputErr := m.consumeOutput(pointerField, lengthField)
	if int32(status) != 0 {
		return nil, errors.Join(fmt.Errorf("StoreAgent kbsync returned %d", int32(status)), outputErr)
	}

	if outputErr != nil {
		return nil, outputErr
	}

	if len(output) == 0 {
		return nil, errors.New("StoreAgent kbsync returned an empty buffer")
	}

	return output, nil
}
