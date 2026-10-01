package machine

import (
	"bytes"
	"errors"
	"fmt"
	"net"
)

const (
	macPriorityPrimary = iota
	macPriorityPhysical
	macPriorityFallback
)

type macAddressCandidate struct {
	address  net.HardwareAddr
	priority int
}

func usableMacAddress(address net.HardwareAddr) bool {
	return len(address) == 6 && address[0]&1 == 0 &&
		!bytes.Equal(address, []byte{0, 0, 0, 0, 0, 0}) &&
		!bytes.Equal(address, []byte{0x02, 0, 0, 0, 0, 0})
}

func selectMacAddress(interfaces []net.Interface, lookup func(net.Interface) (macAddressCandidate, error)) (string, error) {
	var (
		selected  macAddressCandidate
		lookupErr error
	)

	for _, networkInterface := range interfaces {
		if networkInterface.Flags&(net.FlagLoopback|net.FlagPointToPoint) != 0 {
			continue
		}

		candidate, err := lookup(networkInterface)
		if err != nil {
			lookupErr = errors.Join(lookupErr, err)

			continue
		}

		if !usableMacAddress(candidate.address) {
			continue
		}

		// Do not use enumeration order, interface index or link state as a
		// tie-breaker: those can change without changing the machine.
		if selected.address == nil || candidate.priority < selected.priority ||
			(candidate.priority == selected.priority && bytes.Compare(candidate.address, selected.address) < 0) {
			selected = candidate
		}
	}

	if selected.address != nil {
		return selected.address.String(), nil
	}

	if lookupErr != nil {
		return "", fmt.Errorf("could not find network interfaces with a valid mac address: %w", lookupErr)
	}

	return "", errors.New("could not find network interfaces with a valid mac address")
}
