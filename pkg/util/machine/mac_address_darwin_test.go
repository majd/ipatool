//go:build darwin && !ios

package machine

import (
	"errors"
	"net"
	"unsafe"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("macOS hardware address", func() {
	var networkInterface net.Interface

	BeforeEach(func() {
		networkInterface = net.Interface{
			Name:         "en4",
			HardwareAddr: net.HardwareAddr{0x02, 0, 0, 0, 0, 0},
		}
	})

	It("falls back to an unredacted address when IOKit is unavailable", func() {
		networkInterface.HardwareAddr = net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}

		address, err := macAddressWithHardwareLookup(networkInterface, func(string) (net.HardwareAddr, error) {
			return nil, errors.New("IOKit unavailable")
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(address.address.String()).To(Equal("aa:bb:cc:dd:ee:ff"))
		Expect(address.priority).To(Equal(macPriorityFallback))
	})

	It("recovers the hardware address of the selected interface when redacted", func() {
		address, err := macAddressWithHardwareLookup(networkInterface, func(name string) (net.HardwareAddr, error) {
			Expect(name).To(Equal("en4"))

			return net.HardwareAddr{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}, nil
		})

		Expect(err).ToNot(HaveOccurred())
		Expect(address.address.String()).To(Equal("aa:bb:cc:dd:ee:ff"))
		Expect(address.priority).To(Equal(macPriorityPhysical))
	})

	It("prefers en0's hardware address over its randomized network address", func() {
		networkInterface.Name = "en0"
		networkInterface.HardwareAddr = net.HardwareAddr{0x02, 1, 2, 3, 4, 5}
		address, err := macAddressWithHardwareLookup(networkInterface, func(name string) (net.HardwareAddr, error) {
			Expect(name).To(Equal("en0"))

			return net.HardwareAddr{0x00, 1, 2, 3, 4, 5}, nil
		})

		Expect(err).NotTo(HaveOccurred())
		Expect(address.address.String()).To(Equal("00:01:02:03:04:05"))
		Expect(address.priority).To(Equal(macPriorityPrimary))
	})

	It("reports unavailable hardware information without returning the placeholder", func() {
		lookupErr := errors.New("interface unavailable")
		address, err := macAddressWithHardwareLookup(networkInterface, func(string) (net.HardwareAddr, error) {
			return nil, lookupErr
		})

		Expect(address.address).To(BeEmpty())
		Expect(errors.Is(err, lookupErr)).To(BeTrue())
		Expect(err.Error()).To(ContainSubstring("failed to read hardware address for \"en4\""))
	})

	DescribeTable("rejects an unusable IOKit address", func(hardware net.HardwareAddr) {
		address, err := macAddressWithHardwareLookup(networkInterface, func(string) (net.HardwareAddr, error) {
			return hardware, nil
		})

		Expect(address.address).To(BeEmpty())
		Expect(err).To(MatchError(ContainSubstring("IOKit returned no usable hardware address")))
	},
		Entry("missing", nil),
		Entry("wrong length", net.HardwareAddr{0xaa, 0xbb}),
		Entry("zero", net.HardwareAddr{0, 0, 0, 0, 0, 0}),
		Entry("redacted", net.HardwareAddr{0x02, 0, 0, 0, 0, 0}),
		Entry("broadcast", net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}),
		Entry("multicast", net.HardwareAddr{0x01, 0, 0x5e, 0, 0, 1}),
	)
})

var _ = Describe("IOKit hardware address lookup", func() {
	const (
		dictionary = uintptr(1)
		service    = uint32(2)
		key        = uintptr(3)
		property   = uintptr(4)
		dataType   = uintptr(5)
	)

	var (
		api             macAddressAPI
		data            []byte
		released        []uintptr
		releasedObjects []uint32
	)

	BeforeEach(func() {
		data = []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
		released = nil
		releasedObjects = nil
		api = macAddressAPI{
			matching: func(port, options uint32, name string) uintptr {
				Expect(port).To(BeZero())
				Expect(options).To(BeZero())
				Expect(name).To(Equal("en4"))

				return dictionary
			},
			service: func(port uint32, matching uintptr) uint32 {
				Expect(port).To(BeZero())
				Expect(matching).To(Equal(dictionary))

				return service
			},
			searchProperty: func(object uint32, plane string, propertyKey, allocator uintptr, options uint32) uintptr {
				Expect(object).To(Equal(service))
				Expect(plane).To(Equal("IOService"))
				Expect(propertyKey).To(Equal(key))
				Expect(allocator).To(BeZero())
				Expect(options).To(Equal(uint32(3)))

				return property
			},
			releaseObject: func(object uint32) int32 {
				releasedObjects = append(releasedObjects, object)

				return 0
			},
			createString: func(allocator uintptr, value string, encoding uint32) uintptr {
				Expect(allocator).To(BeZero())
				Expect(value).To(Equal("IOMACAddress"))
				Expect(encoding).To(Equal(uint32(0x08000100)))

				return key
			},
			release: func(object uintptr) {
				released = append(released, object)
				if object == property {
					clear(data)
				}
			},
			typeID: func(object uintptr) uintptr {
				Expect(object).To(Equal(property))

				return dataType
			},
			dataTypeID: func() uintptr { return dataType },
			dataLength: func(object uintptr) int64 {
				Expect(object).To(Equal(property))

				return int64(len(data))
			},
			dataBytes: func(object uintptr) unsafe.Pointer {
				Expect(object).To(Equal(property))

				return unsafe.Pointer(&data[0])
			},
		}
	})

	It("copies the address before releasing the property, key, and interface", func() {
		address, err := readHardwareMacAddress("en4", api)

		Expect(err).ToNot(HaveOccurred())
		Expect(address.String()).To(Equal("aa:bb:cc:dd:ee:ff"))
		Expect(released).To(Equal([]uintptr{property, key}))
		Expect(releasedObjects).To(Equal([]uint32{service}))
	})

	It("handles a missing matching dictionary", func() {
		api.matching = func(uint32, uint32, string) uintptr { return 0 }

		_, err := readHardwareMacAddress("en4", api)

		Expect(err).To(MatchError("could not create interface matching dictionary"))
		Expect(released).To(BeEmpty())
		Expect(releasedObjects).To(BeEmpty())
	})

	It("handles a missing interface without releasing the consumed dictionary twice", func() {
		api.service = func(uint32, uintptr) uint32 { return 0 }

		_, err := readHardwareMacAddress("en4", api)

		Expect(err).To(MatchError("network interface was not found in IOKit"))
		Expect(released).To(BeEmpty())
		Expect(releasedObjects).To(BeEmpty())
	})

	It("releases the interface when the property key cannot be created", func() {
		api.createString = func(uintptr, string, uint32) uintptr { return 0 }

		_, err := readHardwareMacAddress("en4", api)

		Expect(err).To(MatchError("could not create hardware address property key"))
		Expect(released).To(BeEmpty())
		Expect(releasedObjects).To(Equal([]uint32{service}))
	})

	It("releases the interface and key when the property is missing", func() {
		api.searchProperty = func(uint32, string, uintptr, uintptr, uint32) uintptr { return 0 }

		_, err := readHardwareMacAddress("en4", api)

		Expect(err).To(MatchError("hardware address property was not found in IOKit"))
		Expect(released).To(Equal([]uintptr{key}))
		Expect(releasedObjects).To(Equal([]uint32{service}))
	})

	It("rejects a non-data property without accessing its bytes", func() {
		api.typeID = func(uintptr) uintptr { return dataType + 1 }
		api.dataLength = nil
		api.dataBytes = nil

		_, err := readHardwareMacAddress("en4", api)

		Expect(err).To(MatchError("IOKit hardware address must contain six bytes"))
		Expect(released).To(Equal([]uintptr{property, key}))
		Expect(releasedObjects).To(Equal([]uint32{service}))
	})

	It("rejects an unexpected length without accessing the bytes", func() {
		api.dataLength = func(uintptr) int64 { return 8 }
		api.dataBytes = nil

		_, err := readHardwareMacAddress("en4", api)

		Expect(err).To(MatchError("IOKit hardware address must contain six bytes"))
		Expect(released).To(Equal([]uintptr{property, key}))
		Expect(releasedObjects).To(Equal([]uint32{service}))
	})

	It("handles missing data without dereferencing a null pointer", func() {
		api.dataBytes = func(uintptr) unsafe.Pointer { return nil }

		_, err := readHardwareMacAddress("en4", api)

		Expect(err).To(MatchError("IOKit hardware address data is missing"))
		Expect(released).To(Equal([]uintptr{property, key}))
		Expect(releasedObjects).To(Equal([]uint32{service}))
	})
})
