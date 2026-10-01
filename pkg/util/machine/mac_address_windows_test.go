package machine

import (
	"net"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/sys/windows"
)

var _ = Describe("Windows hardware address", func() {
	var row windows.MibIfRow2

	BeforeEach(func() {
		row = windows.MibIfRow2{
			PhysicalAddressLength:       6,
			Type:                        windows.IF_TYPE_ETHERNET_CSMACD,
			InterfaceAndOperStatusFlags: 1,
		}
		copy(row.PhysicalAddress[:], []byte{2, 1, 2, 3, 4, 5})
		copy(row.PermanentPhysicalAddress[:], []byte{0, 1, 2, 3, 4, 5})
	})

	It("uses a physical adapter's permanent address regardless of connection state", func() {
		for _, state := range []uint32{windows.IfOperStatusUp, windows.IfOperStatusDown} {
			row.OperStatus = state
			candidate := macAddressFromWindowsRow(row)
			Expect(candidate.address.String()).To(Equal("00:01:02:03:04:05"))
			Expect(candidate.priority).To(Equal(macPriorityPhysical))
		}
	})

	It("uses the current address when the permanent address is unavailable", func() {
		clear(row.PermanentPhysicalAddress[:])
		candidate := macAddressFromWindowsRow(row)
		Expect(candidate.address).To(Equal(net.HardwareAddr{2, 1, 2, 3, 4, 5}))
		Expect(candidate.priority).To(Equal(macPriorityPhysical))
	})

	DescribeTable("classifies adapters", func(flags uint8, interfaceType, medium uint32, priority int) {
		row.InterfaceAndOperStatusFlags = flags
		row.Type = interfaceType
		row.PhysicalMediumType = medium
		Expect(macAddressFromWindowsRow(row).priority).To(Equal(priority))
	},
		Entry("Wi-Fi hardware", uint8(1), uint32(windows.IF_TYPE_IEEE80211), uint32(9), macPriorityPhysical),
		Entry("virtual Ethernet", uint8(0), uint32(windows.IF_TYPE_ETHERNET_CSMACD), uint32(0), macPriorityFallback),
		Entry("filter", uint8(3), uint32(windows.IF_TYPE_ETHERNET_CSMACD), uint32(0), macPriorityFallback),
		Entry("Bluetooth PAN", uint8(1), uint32(windows.IF_TYPE_ETHERNET_CSMACD), uint32(10), macPriorityFallback),
		Entry("tunnel", uint8(1), uint32(windows.IF_TYPE_TUNNEL), uint32(0), macPriorityFallback),
	)

	DescribeTable("rejects non-Ethernet address lengths without slicing past the buffer", func(length uint32) {
		row.PhysicalAddressLength = length
		Expect(macAddressFromWindowsRow(row).address).To(BeEmpty())
	}, Entry("empty", uint32(0)), Entry("EUI-64", uint32(8)), Entry("oversized", uint32(100)))
})
