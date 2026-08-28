package appstore

import (
	"encoding/hex"
	"fmt"
	"net"
	"strings"
)

func machineIdentity(macAddress string) (string, []byte, error) {
	hardwareAddress, err := net.ParseMAC(macAddress)
	if err != nil {
		return "", nil, fmt.Errorf("failed to parse mac address: %w", err)
	}

	if len(hardwareAddress) == 0 || len(hardwareAddress) > 20 {
		return "", nil, fmt.Errorf("hardware address must contain between 1 and 20 bytes, got %d", len(hardwareAddress))
	}

	machineID := append([]byte(nil), hardwareAddress...)
	guid := strings.ToUpper(hex.EncodeToString(machineID))

	return guid, machineID, nil
}
