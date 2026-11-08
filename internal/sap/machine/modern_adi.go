package machine

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/majd/ipatool/v2/internal/sap/assets"
)

const (
	modernADIStartEntry  = storeAgentBase + 0x1b4400
	modernADIEndEntry    = storeAgentBase + 0x1d5124
	modernADIOTPEntry    = storeAgentBase + 0x1dab04
	modernKeyBagEntry    = storeAgentBase + 0x16e0f8
	modernADIStatePath   = "/var/db/fpsd/adi/adi.pb"
	modernADIDBase       = uint64(0x0000200000000000)
	modernADIDEntry      = modernADIDBase + 0x11180
	modernFairPlayDBase  = uint64(0x0000600000000000)
	modernFairPlayDEntry = modernFairPlayDBase + 0x332040
	// Preserve the prelinked image's original 0x510 page offset so its ADRP
	// references retain the same slide as its data segments.
	modernFairPlayKextBase  = uint64(0x0000700000000510)
	modernFairPlayKextStart = modernFairPlayKextBase + 0x2bca984
	modernFairPlayKextSetup = modernFairPlayKextBase + 0x2c67e50
	modernFairPlayKextInit  = modernFairPlayKextBase + 0x2bca714
	modernFairPlayKextEntry = modernFairPlayKextBase + 0x2cf978c
)

// ModernADI runs the ADI implementation embedded in the pinned macOS 27
// commerce image. Apple Store otp/1.5 provisioning requires this generation;
// the Sierra implementation cannot consume its settingInfo payload.
type ModernADI struct {
	runtime       *Machine
	daemon        *Machine
	fairplay      *Machine
	kernel        *Machine
	kernelContext uint64
	storeGlobal   uint32
	daemonHandler uint64
}

func OpenModernADI(ctx context.Context, bundle assets.Bundle, identifier, state []byte) (*ModernADI, error) {
	if len(identifier) != 16 {
		return nil, errors.New("modern ADI host UUID must be 16 bytes")
	}

	runtime, _, err := openRuntime(ctx, bundle, runtimeOptions{
		extraImages: []imageSpec{{name: "commerce", data: bundle.Commerce, base: storeAgentBase}},
		shims:       shimOptions{zeroReturnAliases: storeAgentZeroReturnAliases},
	})
	if err != nil {
		return nil, err
	}

	ready := false

	defer func() {
		if !ready {
			_ = runtime.Close()
		}
	}()

	daemon, _, err := openRuntime(ctx, bundle, runtimeOptions{
		extraImages: []imageSpec{{name: "adid", data: bundle.ADID, base: modernADIDBase}},
		shims:       shimOptions{pauseRunLoop: true},
	})
	if err != nil {
		return nil, fmt.Errorf("open modern ADI daemon: %w", err)
	}

	defer func() {
		if !ready {
			_ = daemon.Close()
		}
	}()

	fairplay, _, err := openRuntime(ctx, bundle, runtimeOptions{
		extraImages: []imageSpec{{name: "fairplayd", data: bundle.FairPlayD, base: modernFairPlayDBase}},
		shims:       shimOptions{pauseRunLoop: true},
	})
	if err != nil {
		return nil, fmt.Errorf("open modern FairPlay daemon: %w", err)
	}

	defer func() {
		if !ready {
			_ = fairplay.Close()
		}
	}()
	copy(runtime.services.hostUUID[:], identifier)
	copy(daemon.services.hostUUID[:], identifier)
	copy(fairplay.services.hostUUID[:], identifier)

	if len(state) != 0 {
		daemon.services.adiFiles[modernADIStatePath] = append([]byte(nil), state...)
	}

	var kernel *Machine
	if len(bundle.FairPlayKext) != 0 {
		kernel, _, err = openRuntime(ctx, bundle, runtimeOptions{
			extraImages: []imageSpec{{name: "FairPlayIOKit", data: bundle.FairPlayKext, base: modernFairPlayKextBase, kernelBase: 0xfffffe0007004000}},
		})
		if err != nil {
			return nil, fmt.Errorf("open modern FairPlay kernel client: %w", err)
		}

		defer func() {
			if !ready {
				_ = kernel.Close()
			}
		}()
		copy(kernel.services.hostUUID[:], identifier)
	}

	adi := &ModernADI{runtime: runtime, daemon: daemon, fairplay: fairplay, kernel: kernel}

	if kernel != nil {
		kernel.services.machRPCHandler = adi.handleFairPlayMachRPC

		if err := adi.initializeFairPlayKernel(); err != nil {
			return nil, err
		}
	}

	fairplay.services.fairPlayHandler = adi.handleFairPlayMessage

	if _, err = fairplay.invoke(modernFairPlayDEntry, 0, 0, 0); err != nil {
		return nil, fmt.Errorf("initialize modern FairPlay daemon: %w", err)
	}

	daemon.services.fairPlayHandler = adi.handleFairPlayMessage
	runtime.services.fairPlayHandler = adi.handleFairPlayMessage

	if err := adi.initializeDaemon(); err != nil {
		return nil, err
	}

	ready = true

	return adi, nil
}

func (a *ModernADI) initializeFairPlayKernel() error {
	a.kernel.beginCall()
	defer a.kernel.clearScratch()

	if _, err := a.kernel.invoke(modernFairPlayKextStart); err != nil {
		return fmt.Errorf("start modern FairPlay kernel: %w", err)
	}

	status, err := a.kernel.invoke(modernFairPlayKextSetup)
	if err != nil {
		return fmt.Errorf("set up modern FairPlay kernel: %w", err)
	}

	if int32(status) != 0 {
		return fmt.Errorf("set up modern FairPlay kernel: status %d", int32(status))
	}

	output, err := a.kernel.scratch(nil, 8)
	if err != nil {
		return err
	}

	task, err := a.kernel.services.allocate(0x1000)
	if err != nil {
		return fmt.Errorf("allocate modern FairPlay kernel task: %w", err)
	}

	if err := a.kernel.engine.MemWrite(task, make([]byte, 0x1000)); err != nil {
		return fmt.Errorf("initialize modern FairPlay kernel task: %w", err)
	}

	status, err = a.kernel.invoke(modernFairPlayKextInit, 0, output, 0, task)
	if err != nil {
		return fmt.Errorf("initialize modern FairPlay kernel session: %w", err)
	}

	if int32(status) != 0 {
		return fmt.Errorf("initialize modern FairPlay kernel session: status %d", int32(status))
	}

	context, err := a.kernel.readUint64(output)
	if err != nil {
		return err
	}

	if context == 0 {
		return errors.New("modern FairPlay kernel returned no session context")
	}

	a.kernelContext = context

	return nil
}

func (a *ModernADI) ImportKeyBag(hardwareID, keyBag []byte) error {
	if a == nil || a.runtime == nil {
		return errors.New("modern ADI runtime is unavailable")
	}

	hardware, err := hardwareBlock(hardwareID)
	if err != nil {
		return err
	}

	a.runtime.beginCall()

	defer a.runtime.clearScratch()

	hardwareAddress, err := a.runtime.scratch(hardware, uint64(len(hardware)))
	if err != nil {
		return err
	}

	path := append([]byte(storeAgentSCInfoPath), 0)

	pathAddress, err := a.runtime.scratch(path, uint64(len(path)))
	if err != nil {
		return err
	}

	contextField, err := a.runtime.scratch(nil, 4)
	if err != nil {
		return err
	}

	status, err := a.runtime.invoke(storeAgentGlobalInit, 0, hardwareAddress, pathAddress, contextField)
	if err != nil {
		return fmt.Errorf("initialize FairPlay keybag context: %w", err)
	}

	if int32(status) != 0 {
		return fmt.Errorf("initialize FairPlay keybag context: status %d", int32(status))
	}

	global, err := a.runtime.readUint32(contextField)
	if err != nil {
		return err
	}

	if global == 0 {
		return errors.New("initialize FairPlay keybag context: null context")
	}

	keyBagAddress, err := a.runtime.scratch(keyBag, uint64(len(keyBag)))
	if err != nil {
		return err
	}

	status, err = a.runtime.invoke(modernKeyBagEntry, uint64(global), keyBagAddress, uint64(len(keyBag)))
	if err != nil {
		return fmt.Errorf("import FairPlay keybag: %w", err)
	}

	if int32(status) != 0 {
		return fmt.Errorf("import FairPlay keybag: status %d", int32(status))
	}

	a.storeGlobal = global

	return nil
}

func (a *ModernADI) handleFairPlayMessage(input, indirect []byte, outputSize int) ([]byte, []byte, int32, error) {
	if a.kernel != nil {
		stackProbe, _ := a.kernel.readUint64(modernFairPlayKextBase + 0xb8c028)
		if stackProbe != modernFairPlayKextBase+0x2c66248 {
			return nil, nil, 0, fmt.Errorf("modern FairPlay kernel stack probe is %#x", stackProbe)
		}

		a.kernel.beginCall()

		defer a.kernel.clearScratch()

		message, err := a.kernel.scratch(indirect, uint64(len(indirect)))
		if err != nil {
			return nil, nil, 0, err
		}

		status, err := a.kernel.invoke(modernFairPlayKextEntry, message, uint64(len(indirect)), a.kernelContext)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("run modern FairPlay kernel message: %w", err)
		}

		updated, err := a.kernel.engine.MemRead(message, uint64(len(indirect)))
		if err != nil {
			return nil, nil, 0, fmt.Errorf("read modern FairPlay kernel message: %w", err)
		}

		output := append([]byte(nil), input...)
		if len(output) > outputSize {
			output = output[:outputSize]
		}

		return output, updated, int32(status), nil
	}

	if a.fairplay.services.machPortCallback == 0 {
		return nil, nil, 0, errors.New("modern FairPlay daemon installed no message callback")
	}

	a.fairplay.beginCall()

	defer a.fairplay.clearScratch()

	message, err := a.fairplay.scratch(indirect, uint64(len(indirect)))
	if err != nil {
		return nil, nil, 0, err
	}

	_, err = a.fairplay.invoke(
		a.fairplay.services.machPortCallback,
		fakeHandle,
		message,
		uint64(len(indirect)),
		a.fairplay.services.machPortInfo,
	)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("run modern FairPlay message callback: %w", err)
	}

	updated, err := a.fairplay.engine.MemRead(message, uint64(len(indirect)))
	if err != nil {
		return nil, nil, 0, fmt.Errorf("read modern FairPlay daemon message: %w", err)
	}

	output := append([]byte(nil), input...)
	if len(output) > outputSize {
		output = output[:outputSize]
	}

	return output, updated, 0, nil
}

func (a *ModernADI) handleFairPlayMachRPC(request []byte, receiveSize int) ([]byte, error) {
	if a.fairplay.services.machPortCallback == 0 {
		return nil, errors.New("modern FairPlay daemon installed no message callback")
	}

	a.fairplay.beginCall()

	defer a.fairplay.clearScratch()

	message, err := a.fairplay.scratch(request, uint64(max(len(request), receiveSize)))
	if err != nil {
		return nil, err
	}

	a.fairplay.services.machMessageReply = nil
	_, err = a.fairplay.invoke(
		a.fairplay.services.machPortCallback,
		fakeHandle,
		message,
		uint64(len(request)),
		a.fairplay.services.machPortInfo,
	)

	if err != nil {
		return nil, fmt.Errorf("run modern FairPlay Mach callback: %w", err)
	}

	if len(a.fairplay.services.machMessageReply) == 0 {
		return nil, errors.New("modern FairPlay daemon returned no Mach reply")
	}

	return append([]byte(nil), a.fairplay.services.machMessageReply...), nil
}

func (a *ModernADI) initializeDaemon() error {
	_, entryErr := a.daemon.invoke(modernADIDEntry, 0, 0, 0, 0)
	if a.daemon.services.xpcEventHandler == 0 {
		return fmt.Errorf("initialize modern ADI daemon: %w", entryErr)
	}

	listener := a.daemon.services.xpcEventHandler
	connection := a.daemon.services.newXPCObject(nil)

	if err := a.invokeDaemonBlock(listener, connection); err != nil {
		return fmt.Errorf("accept modern ADI daemon connection: %w", err)
	}

	if a.daemon.services.xpcEventHandler == listener {
		return errors.New("modern ADI daemon did not install a request handler")
	}

	a.daemonHandler = a.daemon.services.xpcEventHandler

	return nil
}

func (a *ModernADI) invokeDaemonBlock(block, argument uint64) error {
	handler, err := a.daemon.readUint64(block + 16)
	if err != nil {
		return err
	}

	_, err = a.daemon.invoke(handler, block, argument)

	return err
}

func (a *ModernADI) invoke(function uint64, arguments ...uint64) (uint64, error) {
	a.runtime.services.xpcData = nil
	a.runtime.services.xpcSyncReply = nil
	_, firstErr := a.runtime.invoke(function, arguments...)
	request := append([]byte(nil), a.runtime.services.xpcData...)

	if len(request) == 0 {
		return 0, fmt.Errorf("modern ADI client created no daemon request: %w", firstErr)
	}

	a.daemon.services.xpcReply = nil
	a.daemon.services.xpcReplied = false
	requestObject := a.daemon.services.newXPCObject(request)

	if err := a.invokeDaemonBlock(a.daemonHandler, requestObject); err != nil {
		return 0, fmt.Errorf("run modern ADI daemon request: %w", err)
	}

	if !a.daemon.services.xpcReplied || len(a.daemon.services.xpcReply) == 0 {
		return 0, errors.New("modern ADI daemon returned no reply")
	}

	a.runtime.services.xpcSyncReply = append([]byte(nil), a.daemon.services.xpcReply...)

	return a.runtime.invoke(function, arguments...)
}

// ExchangeXPC sends the opaque data payload used by AppleMediaServices to the
// emulated com.apple.adid service and returns its opaque reply payload.
func (a *ModernADI) ExchangeXPC(request []byte) ([]byte, error) {
	if a == nil || a.daemon == nil || a.daemonHandler == 0 {
		return nil, errors.New("modern ADI daemon is unavailable")
	}

	if len(request) == 0 || len(request) > 1<<20 {
		return nil, errors.New("modern ADI XPC request is invalid")
	}

	a.daemon.services.xpcReply = nil
	a.daemon.services.xpcReplied = false
	requestObject := a.daemon.services.newXPCObject(request)

	if err := a.invokeDaemonBlock(a.daemonHandler, requestObject); err != nil {
		return nil, fmt.Errorf("run modern ADI XPC request: %w", err)
	}

	if !a.daemon.services.xpcReplied || len(a.daemon.services.xpcReply) == 0 {
		return nil, errors.New("modern ADI XPC service returned no reply")
	}

	return append([]byte(nil), a.daemon.services.xpcReply...), nil
}

func (a *ModernADI) StartProvisioning(dsid uint64, spim []byte) ([]byte, uint32, error) {
	if a == nil || a.runtime == nil {
		return nil, 0, errors.New("modern ADI runtime is unavailable")
	}

	a.runtime.beginCall()

	defer a.runtime.clearScratch()

	input, err := a.runtime.scratch(spim, uint64(len(spim)))
	if err != nil {
		return nil, 0, err
	}

	pointerField, err := a.runtime.scratch(nil, 8)
	if err != nil {
		return nil, 0, err
	}

	lengthField, err := a.runtime.scratch(nil, 4)
	if err != nil {
		return nil, 0, err
	}

	sessionField, err := a.runtime.scratch(nil, 4)
	if err != nil {
		return nil, 0, err
	}

	status, err := a.invoke(modernADIStartEntry, dsid, input, uint64(len(spim)), pointerField, lengthField, sessionField)
	if err != nil {
		return nil, 0, err
	}

	if int32(status) != 0 {
		return nil, 0, fmt.Errorf("start modern ADI provisioning: status %d", int32(status))
	}

	output, err := a.readOutput(pointerField, lengthField)
	if err != nil {
		return nil, 0, err
	}

	session, err := a.runtime.engine.MemRead(sessionField, 4)
	if err != nil {
		return nil, 0, fmt.Errorf("read modern ADI provisioning session: %w", err)
	}

	return output, binary.LittleEndian.Uint32(session), nil
}

func (a *ModernADI) EndProvisioning(session uint32, settingInfo, transportKey []byte) error {
	if a == nil || a.runtime == nil {
		return errors.New("modern ADI runtime is unavailable")
	}

	a.runtime.beginCall()

	defer a.runtime.clearScratch()

	settingAddress, err := a.runtime.scratch(settingInfo, uint64(len(settingInfo)))
	if err != nil {
		return err
	}

	keyAddress, err := a.runtime.scratch(transportKey, uint64(len(transportKey)))
	if err != nil {
		return err
	}

	status, err := a.invoke(modernADIEndEntry, uint64(session), settingAddress, uint64(len(settingInfo)), keyAddress, uint64(len(transportKey)))
	if err != nil {
		return err
	}

	if int32(status) != 0 {
		return fmt.Errorf("finish modern ADI provisioning: status %d", int32(status))
	}

	return nil
}

func (a *ModernADI) RequestOTP(dsid uint64) ([]byte, []byte, error) {
	if a == nil || a.runtime == nil {
		return nil, nil, errors.New("modern ADI runtime is unavailable")
	}

	a.runtime.beginCall()

	defer a.runtime.clearScratch()

	midPointer, err := a.runtime.scratch(nil, 8)
	if err != nil {
		return nil, nil, err
	}

	midLength, err := a.runtime.scratch(nil, 4)
	if err != nil {
		return nil, nil, err
	}

	otpPointer, err := a.runtime.scratch(nil, 8)
	if err != nil {
		return nil, nil, err
	}

	otpLength, err := a.runtime.scratch(nil, 4)
	if err != nil {
		return nil, nil, err
	}

	status, err := a.invoke(modernADIOTPEntry, dsid, midPointer, midLength, otpPointer, otpLength)
	if err != nil {
		return nil, nil, err
	}

	if int32(status) != 0 {
		return nil, nil, fmt.Errorf("request modern ADI OTP: status %d", int32(status))
	}

	mid, err := a.readOutput(midPointer, midLength)
	if err != nil {
		return nil, nil, err
	}

	otp, err := a.readOutput(otpPointer, otpLength)
	if err != nil {
		return nil, nil, err
	}

	return mid, otp, nil
}

func (a *ModernADI) readOutput(pointerField, lengthField uint64) ([]byte, error) {
	pointer, err := a.runtime.readUint64(pointerField)
	if err != nil {
		return nil, err
	}

	length, err := a.runtime.readUint32(lengthField)
	if err != nil {
		return nil, err
	}

	if pointer == 0 || length == 0 || length > 1<<20 {
		return nil, errors.New("modern ADI returned invalid output")
	}

	output, err := a.runtime.engine.MemRead(pointer, uint64(length))
	if err != nil {
		return nil, fmt.Errorf("read modern ADI output: %w", err)
	}

	return output, nil
}

func (a *ModernADI) State() ([]byte, error) {
	if a == nil || a.runtime == nil {
		return nil, errors.New("modern ADI runtime is unavailable")
	}

	if state := a.daemon.services.adiFiles[modernADIStatePath]; len(state) != 0 {
		return append([]byte(nil), state...), nil
	}

	for path, state := range a.daemon.services.adiFiles {
		if filepath.Base(path) == "adi.pb" && len(state) != 0 {
			return append([]byte(nil), state...), nil
		}
	}

	return nil, errors.New("modern ADI state was not created")
}

func (a *ModernADI) Close() error {
	if a == nil || a.runtime == nil {
		return nil
	}

	var err error

	if a.storeGlobal != 0 {
		if status, closeErr := a.runtime.invoke(storeAgentGlobalClose, uint64(a.storeGlobal)); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("close FairPlay keybag context: %w", closeErr))
		} else if int32(status) != 0 {
			err = errors.Join(err, fmt.Errorf("close FairPlay keybag context: status %d", int32(status)))
		}
	}

	err = errors.Join(err, a.runtime.Close())
	err = errors.Join(err, a.daemon.Close())
	err = errors.Join(err, a.fairplay.Close())

	if a.kernel != nil {
		err = errors.Join(err, a.kernel.Close())
	}

	a.runtime = nil
	a.daemon = nil
	a.fairplay = nil
	a.kernel = nil

	return err
}
