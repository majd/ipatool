package appstore

import (
	"bytes"
	"testing"
)

func TestMachineIdentity(t *testing.T) {
	tests := []struct {
		name      string
		address   string
		wantGUID  string
		wantBytes []byte
	}{
		{
			name:      "EUI-48",
			address:   "00:11:22:aa:bb:cc",
			wantGUID:  "001122AABBCC",
			wantBytes: []byte{0x00, 0x11, 0x22, 0xaa, 0xbb, 0xcc},
		},
		{
			name:      "EUI-64",
			address:   "00:11:22:33:44:55:66:77",
			wantGUID:  "0011223344556677",
			wantBytes: []byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77},
		},
		{
			name:     "IP over InfiniBand",
			address:  "00:01:02:03:04:05:06:07:08:09:0a:0b:0c:0d:0e:0f:10:11:12:13",
			wantGUID: "000102030405060708090A0B0C0D0E0F10111213",
			wantBytes: []byte{
				0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09,
				0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10, 0x11, 0x12, 0x13,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			guid, machineID, err := machineIdentity(test.address)
			if err != nil {
				t.Fatalf("machineIdentity() error = %v", err)
			}

			if guid != test.wantGUID {
				t.Fatalf("machineIdentity() GUID = %q, want %q", guid, test.wantGUID)
			}

			if !bytes.Equal(machineID, test.wantBytes) {
				t.Fatalf("machineIdentity() machine ID = %x, want %x", machineID, test.wantBytes)
			}
		})
	}
}

func TestMachineIdentityRejectsInvalidAddress(t *testing.T) {
	if _, _, err := machineIdentity("not-a-hardware-address"); err == nil {
		t.Fatal("machineIdentity() error = nil, want parse error")
	}
}
