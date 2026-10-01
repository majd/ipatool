package machine

import (
	"net"

	"golang.org/x/sys/windows"
)

var getIfEntry2Ex = windows.NewLazySystemDLL("iphlpapi.dll").NewProc("GetIfEntry2Ex")

func interfaceMacAddress(networkInterface net.Interface) (macAddressCandidate, error) {
	// Older Windows 10 builds lack this entry point. Metadata is optional;
	// preserve the validated network address as a fallback on those systems.
	fallback := macAddressCandidate{address: networkInterface.HardwareAddr, priority: macPriorityFallback}
	if err := getIfEntry2Ex.Find(); err != nil {
		return fallback, nil
	}

	row := windows.MibIfRow2{InterfaceIndex: uint32(networkInterface.Index)}
	if err := windows.GetIfEntry2Ex(windows.MibIfTableNormalWithoutStatistics, &row); err != nil {
		return fallback, nil
	}

	return macAddressFromWindowsRow(row), nil
}

func macAddressFromWindowsRow(row windows.MibIfRow2) macAddressCandidate {
	candidate := macAddressCandidate{priority: macPriorityFallback}
	if row.PhysicalAddressLength != 6 {
		return candidate
	}

	candidate.address = append(net.HardwareAddr(nil), row.PhysicalAddress[:6]...)
	// Use the permanent address so Wi-Fi randomization does not change the
	// identity. Virtual adapters remain available as a lower-priority fallback.
	if usableMacAddress(row.PermanentPhysicalAddress[:6]) {
		candidate.address = append(net.HardwareAddr(nil), row.PermanentPhysicalAddress[:6]...)
	}

	// MIB_IF_ROW2's first two status bits are HardwareInterface and
	// FilterInterface. Bluetooth PAN can otherwise look like Ethernet.
	const (
		hardwareInterface = 1
		filterInterface   = 2
		bluetoothMedium   = 10
	)

	if row.InterfaceAndOperStatusFlags&(hardwareInterface|filterInterface) == hardwareInterface &&
		row.PhysicalMediumType != bluetoothMedium &&
		(row.Type == windows.IF_TYPE_ETHERNET_CSMACD || row.Type == windows.IF_TYPE_IEEE80211) {
		candidate.priority = macPriorityPhysical
	}

	return candidate
}
