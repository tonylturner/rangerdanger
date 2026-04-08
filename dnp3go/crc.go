// Package dnp3go implements a minimal DNP3 TCP outstation (IEEE 1815).
//
// It provides a standalone, zero-dependency DNP3 outstation server that
// responds to integrity polls, class data reads, direct operate (CROB
// and analog output), and select-before-operate sequences. Designed for
// ICS simulation and security training environments.
package dnp3go

// CRC-16/DNP: polynomial 0x3D65, reflected I/O, init=0x0000, final XOR=0xFFFF.
// This is the standard CRC used in DNP3 data link layer frames.

var crcTable [256]uint16

func init() {
	const poly = 0xA6BC // reflected polynomial of 0x3D65
	for i := 0; i < 256; i++ {
		crc := uint16(i)
		for j := 0; j < 8; j++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ poly
			} else {
				crc >>= 1
			}
		}
		crcTable[i] = crc
	}
}

// crcDNP computes the CRC-16/DNP of the given data.
func crcDNP(data []byte) uint16 {
	crc := uint16(0x0000)
	for _, b := range data {
		crc = (crc >> 8) ^ crcTable[byte(crc)^b]
	}
	return ^crc // final XOR 0xFFFF
}

// appendCRC appends a 2-byte little-endian CRC to data and returns the result.
func appendCRC(data []byte) []byte {
	c := crcDNP(data)
	return append(data, byte(c), byte(c>>8))
}

// verifyCRC checks that the last 2 bytes of data are a valid CRC of the preceding bytes.
func verifyCRC(data []byte) bool {
	if len(data) < 3 {
		return false
	}
	payload := data[:len(data)-2]
	got := uint16(data[len(data)-2]) | uint16(data[len(data)-1])<<8
	return crcDNP(payload) == got
}
