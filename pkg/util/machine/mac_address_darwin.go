//go:build darwin && !ios

package machine

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"unsafe"

	"github.com/ebitengine/purego"
)

func interfaceMacAddress(networkInterface net.Interface) (string, error) {
	return macAddressWithHardwareLookup(networkInterface, hardwareMacAddress)
}

func macAddressWithHardwareLookup(networkInterface net.Interface, lookup func(string) (net.HardwareAddr, error)) (string, error) {
	// macOS 27 can redact network interface addresses with a shared placeholder.
	// Resolve the same interface through IOKit to preserve its existing identity.
	if !bytes.Equal(networkInterface.HardwareAddr, []byte{0x02, 0, 0, 0, 0, 0}) {
		return networkInterface.HardwareAddr.String(), nil
	}

	address, err := lookup(networkInterface.Name)
	if err != nil {
		return "", fmt.Errorf("macOS redacted the mac address for %q; failed to read its hardware address: %w", networkInterface.Name, err)
	}

	if len(address) != 6 || address[0]&1 != 0 ||
		bytes.Equal(address, []byte{0, 0, 0, 0, 0, 0}) ||
		bytes.Equal(address, []byte{0x02, 0, 0, 0, 0, 0}) {
		return "", fmt.Errorf("macOS redacted the mac address for %q; IOKit returned no usable hardware address", networkInterface.Name)
	}

	return address.String(), nil
}

type macAddressAPI struct {
	matching       func(uint32, uint32, string) uintptr
	service        func(uint32, uintptr) uint32
	searchProperty func(uint32, string, uintptr, uintptr, uint32) uintptr
	releaseObject  func(uint32) int32
	createString   func(uintptr, string, uint32) uintptr
	release        func(uintptr)
	typeID         func(uintptr) uintptr
	dataTypeID     func() uintptr
	dataLength     func(uintptr) int64
	dataBytes      func(uintptr) unsafe.Pointer
}

func hardwareMacAddress(name string) (net.HardwareAddr, error) {
	iokit, err := purego.Dlopen("/System/Library/Frameworks/IOKit.framework/IOKit", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, fmt.Errorf("load IOKit: %w", err)
	}

	defer func() { _ = purego.Dlclose(iokit) }()

	coreFoundation, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_NOW|purego.RTLD_LOCAL)
	if err != nil {
		return nil, fmt.Errorf("load CoreFoundation: %w", err)
	}

	defer func() { _ = purego.Dlclose(coreFoundation) }()

	var api macAddressAPI

	purego.RegisterLibFunc(&api.matching, iokit, "IOBSDNameMatching")
	purego.RegisterLibFunc(&api.service, iokit, "IOServiceGetMatchingService")
	purego.RegisterLibFunc(&api.searchProperty, iokit, "IORegistryEntrySearchCFProperty")
	purego.RegisterLibFunc(&api.releaseObject, iokit, "IOObjectRelease")
	purego.RegisterLibFunc(&api.createString, coreFoundation, "CFStringCreateWithCString")
	purego.RegisterLibFunc(&api.release, coreFoundation, "CFRelease")
	purego.RegisterLibFunc(&api.typeID, coreFoundation, "CFGetTypeID")
	purego.RegisterLibFunc(&api.dataTypeID, coreFoundation, "CFDataGetTypeID")
	purego.RegisterLibFunc(&api.dataLength, coreFoundation, "CFDataGetLength")
	purego.RegisterLibFunc(&api.dataBytes, coreFoundation, "CFDataGetBytePtr")

	return readHardwareMacAddress(name, api)
}

func readHardwareMacAddress(name string, api macAddressAPI) (net.HardwareAddr, error) {
	matching := api.matching(0, 0, name)
	if matching == 0 {
		return nil, errors.New("could not create interface matching dictionary")
	}

	// IOServiceGetMatchingService consumes the matching dictionary.
	service := api.service(0, matching)
	if service == 0 {
		return nil, errors.New("network interface was not found in IOKit")
	}
	defer api.releaseObject(service)

	const utf8Encoding = 0x08000100

	key := api.createString(0, "IOMACAddress", utf8Encoding)
	if key == 0 {
		return nil, errors.New("could not create hardware address property key")
	}
	defer api.release(key)

	// Apple's receipt-validation guidance reads IOMACAddress from the interface
	// or its parents in the IOService plane.
	// https://developer.apple.com/documentation/appstorereceipts/validating-receipts-on-the-device
	const iterateRecursivelyAndParents = 0x01 | 0x02

	property := api.searchProperty(service, "IOService", key, 0, iterateRecursivelyAndParents)
	if property == 0 {
		return nil, errors.New("hardware address property was not found in IOKit")
	}
	defer api.release(property)

	if api.typeID(property) != api.dataTypeID() || api.dataLength(property) != 6 {
		return nil, errors.New("IOKit hardware address must contain six bytes")
	}

	data := api.dataBytes(property)
	if data == nil {
		return nil, errors.New("IOKit hardware address data is missing")
	}

	// Copy the bytes before releasing the CoreFoundation property.
	return append(net.HardwareAddr(nil), unsafe.Slice((*byte)(data), 6)...), nil
}
