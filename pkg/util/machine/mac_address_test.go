package machine

import (
	"errors"
	"net"
	"slices"
	"sync"
	"sync/atomic"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Machine address selection", func() {
	DescribeTable("validates hardware addresses", func(address net.HardwareAddr, valid bool) {
		Expect(usableMacAddress(address)).To(Equal(valid))
	},
		Entry("missing", nil, false),
		Entry("short", net.HardwareAddr{0, 1}, false),
		Entry("EUI-64", net.HardwareAddr{0, 1, 2, 3, 4, 5, 6, 7}, false),
		Entry("zero", net.HardwareAddr{0, 0, 0, 0, 0, 0}, false),
		Entry("redacted", net.HardwareAddr{2, 0, 0, 0, 0, 0}, false),
		Entry("broadcast", net.HardwareAddr{255, 255, 255, 255, 255, 255}, false),
		Entry("multicast", net.HardwareAddr{1, 0, 0x5e, 0, 0, 1}, false),
		Entry("unicast", net.HardwareAddr{0, 1, 2, 3, 4, 5}, true),
		Entry("locally administered", net.HardwareAddr{2, 1, 2, 3, 4, 5}, true),
	)

	It("prefers physical adapters regardless of enumeration order, index or link state", func() {
		interfaces := []net.Interface{
			{Name: "virtual", Index: 1, Flags: net.FlagUp, HardwareAddr: net.HardwareAddr{0, 0, 0, 0, 0, 1}},
			{Name: "wired", Index: 20, HardwareAddr: net.HardwareAddr{0, 0, 0, 0, 0, 3}},
			{Name: "wireless", Index: 10, Flags: net.FlagUp, HardwareAddr: net.HardwareAddr{0, 0, 0, 0, 0, 2}},
		}
		lookup := func(networkInterface net.Interface) (macAddressCandidate, error) {
			priority := macPriorityPhysical
			if networkInterface.Name == "virtual" {
				priority = macPriorityFallback
			}

			return macAddressCandidate{address: networkInterface.HardwareAddr, priority: priority}, nil
		}
		for range 3 {
			for _, reversed := range []bool{false, true} {
				input := slices.Clone(interfaces)
				if reversed {
					slices.Reverse(input)
				}
				address, err := selectMacAddress(input, lookup)
				Expect(err).NotTo(HaveOccurred())
				Expect(address).To(Equal("00:00:00:00:00:02"))
			}
			interfaces = append(interfaces[1:], interfaces[0])
			for i := range interfaces {
				interfaces[i].Index += 100
				interfaces[i].Flags ^= net.FlagUp
			}
		}
	})

	It("prefers a primary hardware interface over other physical adapters", func() {
		interfaces := []net.Interface{{Name: "other"}, {Name: "primary"}}
		address, err := selectMacAddress(interfaces, func(networkInterface net.Interface) (macAddressCandidate, error) {
			if networkInterface.Name == "primary" {
				return macAddressCandidate{address: net.HardwareAddr{0, 0, 0, 0, 0, 2}, priority: macPriorityPrimary}, nil
			}

			return macAddressCandidate{address: net.HardwareAddr{0, 0, 0, 0, 0, 1}, priority: macPriorityPhysical}, nil
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(address).To(Equal("00:00:00:00:00:02"))
	})

	It("skips loopback, tunnels, failed lookups and unusable addresses before using a fallback", func() {
		interfaces := []net.Interface{
			{Name: "loopback", Flags: net.FlagLoopback},
			{Name: "tunnel", Flags: net.FlagPointToPoint},
			{Name: "unavailable"},
			{Name: "redacted", HardwareAddr: net.HardwareAddr{2, 0, 0, 0, 0, 0}},
			{Name: "virtual", HardwareAddr: net.HardwareAddr{2, 1, 2, 3, 4, 5}},
		}
		address, err := selectMacAddress(interfaces, func(networkInterface net.Interface) (macAddressCandidate, error) {
			Expect(networkInterface.Flags & (net.FlagLoopback | net.FlagPointToPoint)).To(BeZero())
			if networkInterface.Name == "unavailable" {
				return macAddressCandidate{}, errors.New("hardware lookup failed")
			}

			return macAddressCandidate{address: networkInterface.HardwareAddr, priority: macPriorityFallback}, nil
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(address).To(Equal("02:01:02:03:04:05"))
	})

	It("reports missing usable addresses without wrapping a nil error", func() {
		address, err := selectMacAddress(nil, nil)
		Expect(address).To(BeEmpty())
		Expect(err).To(MatchError("could not find network interfaces with a valid mac address"))
	})

	It("retains lookup failures if no usable adapter remains", func() {
		lookupErr := errors.New("hardware unavailable")
		address, err := selectMacAddress([]net.Interface{{Name: "en0"}}, func(net.Interface) (macAddressCandidate, error) {
			return macAddressCandidate{}, lookupErr
		})
		Expect(address).To(BeEmpty())
		Expect(errors.Is(err, lookupErr)).To(BeTrue())
	})

	It("keeps one identity across concurrent calls and subsequent interface changes", func() {
		var calls atomic.Int32
		sut := &machine{interfaces: func() ([]net.Interface, error) {
			last := byte(calls.Add(1))

			return []net.Interface{{Index: 2147483647, Name: "ipatool-test", HardwareAddr: net.HardwareAddr{2, 1, 2, 3, 4, last}}}, nil
		}}
		var wg sync.WaitGroup
		for range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer GinkgoRecover()
				address, err := sut.MacAddress()
				Expect(err).NotTo(HaveOccurred())
				Expect(address).To(Equal("02:01:02:03:04:01"))
			}()
		}
		wg.Wait()
		address, err := sut.MacAddress()
		Expect(err).NotTo(HaveOccurred())
		Expect(address).To(Equal("02:01:02:03:04:01"))
		Expect(calls.Load()).To(Equal(int32(1)))
	})

	It("preserves interface enumeration errors", func() {
		lookupErr := errors.New("interfaces unavailable")
		sut := &machine{interfaces: func() ([]net.Interface, error) { return nil, lookupErr }}
		address, err := sut.MacAddress()
		Expect(address).To(BeEmpty())
		Expect(errors.Is(err, lookupErr)).To(BeTrue())
		Expect(err.Error()).To(HavePrefix("failed to get network interfaces:"))
	})
})
