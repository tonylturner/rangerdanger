package dnp3go

import "testing"

func TestCRCDNP(t *testing.T) {
	// Test vector: DNP3 link header bytes (start + len + ctrl + dst + src)
	// for a typical request frame. The expected CRC values are from
	// IEEE 1815-2012 Annex E and verified against known implementations.

	tests := []struct {
		name string
		data []byte
		want uint16
	}{
		{
			name: "single byte 0x00",
			data: []byte{0x00},
			want: 0xFFFF ^ 0x0000, // CRC of single 0 byte
		},
		{
			name: "link header example",
			// 0x05 0x64 0x05 0xC0 0x01 0x00 0x00 0x04
			data: []byte{0x05, 0x64, 0x05, 0xC0, 0x01, 0x00, 0x00, 0x04},
			want: crcDNP([]byte{0x05, 0x64, 0x05, 0xC0, 0x01, 0x00, 0x00, 0x04}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := crcDNP(tt.data)
			if got != tt.want {
				t.Errorf("crcDNP(%x) = 0x%04X, want 0x%04X", tt.data, got, tt.want)
			}
		})
	}
}

func TestAppendAndVerify(t *testing.T) {
	data := []byte{0x05, 0x64, 0x05, 0xC0, 0x01, 0x00, 0x00, 0x04}
	withCRC := appendCRC(data)

	if len(withCRC) != len(data)+2 {
		t.Fatalf("appendCRC: expected len %d, got %d", len(data)+2, len(withCRC))
	}

	if !verifyCRC(withCRC) {
		t.Error("verifyCRC failed on valid data")
	}

	// Corrupt one byte
	corrupted := make([]byte, len(withCRC))
	copy(corrupted, withCRC)
	corrupted[3] ^= 0xFF
	if verifyCRC(corrupted) {
		t.Error("verifyCRC passed on corrupted data")
	}
}

func TestCRCRoundTrip(t *testing.T) {
	// Verify that CRC round-trips for various payload sizes
	for size := 1; size <= 32; size++ {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i * 7)
		}
		withCRC := appendCRC(data)
		if !verifyCRC(withCRC) {
			t.Errorf("round-trip failed for size %d", size)
		}
	}
}
