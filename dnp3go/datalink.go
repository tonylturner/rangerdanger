package dnp3go

import (
	"encoding/binary"
	"errors"
	"io"
)

// DNP3 data link layer constants.
const (
	startByte1 = 0x05
	startByte2 = 0x64

	// Header size: start(2) + length(1) + control(1) + dest(2) + src(2) + crc(2) = 10
	linkHeaderSize = 10

	// Maximum user data per link frame data block (before CRC).
	blockSize = 16

	// Control byte bits
	ctrlDIR = 0x80 // Direction: 1=master→outstation, 0=outstation→master
	ctrlPRM = 0x40 // Primary: 1=primary message, 0=secondary

	// Function codes in control byte (lower 4 bits)
	ctrlFCUnconfirmedData = 0x04
)

// LinkFrame represents a parsed DNP3 data link layer frame.
type LinkFrame struct {
	Control uint8
	Dest    uint16
	Source  uint16
	Payload []byte // transport + application data (CRCs stripped)
}

var (
	ErrBadStartBytes = errors.New("dnp3: invalid start bytes")
	ErrBadHeaderCRC  = errors.New("dnp3: header CRC mismatch")
	ErrBadBlockCRC   = errors.New("dnp3: data block CRC mismatch")
	ErrFrameTooShort = errors.New("dnp3: frame too short")
)

// ReadLinkFrame reads a single DNP3 link frame from r.
// It scans for start bytes, validates CRCs, and returns the reassembled payload.
func ReadLinkFrame(r io.Reader) (*LinkFrame, error) {
	// Scan for start bytes
	var b [1]byte
	for {
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, err
		}
		if b[0] != startByte1 {
			continue
		}
		if _, err := io.ReadFull(r, b[:]); err != nil {
			return nil, err
		}
		if b[0] == startByte2 {
			break
		}
	}

	// Read remaining header: length(1) + control(1) + dest(2) + src(2) + crc(2) = 8 bytes
	hdrRest := make([]byte, 8)
	if _, err := io.ReadFull(r, hdrRest); err != nil {
		return nil, err
	}

	// Verify header CRC (covers start bytes + length + control + dest + src)
	fullHeader := make([]byte, 10)
	fullHeader[0] = startByte1
	fullHeader[1] = startByte2
	copy(fullHeader[2:], hdrRest)

	if !verifyCRC(fullHeader) {
		return nil, ErrBadHeaderCRC
	}

	length := fullHeader[2]  // number of bytes in frame after length field (excl CRCs)
	control := fullHeader[3]
	dest := binary.LittleEndian.Uint16(fullHeader[4:6])
	source := binary.LittleEndian.Uint16(fullHeader[6:8])

	// User data length = length - 5 (control + dest + src = 5 fixed bytes counted in length)
	if length < 5 {
		return &LinkFrame{Control: control, Dest: dest, Source: source}, nil
	}
	userDataLen := int(length) - 5

	// Read user data in 16-byte blocks, each followed by 2-byte CRC
	var payload []byte
	remaining := userDataLen

	for remaining > 0 {
		blockLen := remaining
		if blockLen > blockSize {
			blockLen = blockSize
		}

		// Read block + 2 byte CRC
		block := make([]byte, blockLen+2)
		if _, err := io.ReadFull(r, block); err != nil {
			return nil, err
		}

		if !verifyCRC(block) {
			return nil, ErrBadBlockCRC
		}

		payload = append(payload, block[:blockLen]...)
		remaining -= blockLen
	}

	return &LinkFrame{
		Control: control,
		Dest:    dest,
		Source:  source,
		Payload: payload,
	}, nil
}

// WriteLinkFrame writes a DNP3 link frame to w with proper CRCs.
func WriteLinkFrame(w io.Writer, f *LinkFrame) error {
	userDataLen := len(f.Payload)
	length := byte(5 + userDataLen) // control(1) + dest(2) + src(2) + user data

	// Build header without CRC
	header := make([]byte, 8)
	header[0] = startByte1
	header[1] = startByte2
	header[2] = length
	header[3] = f.Control
	binary.LittleEndian.PutUint16(header[4:6], f.Dest)
	binary.LittleEndian.PutUint16(header[6:8], f.Source)

	// Append header CRC
	headerWithCRC := appendCRC(header)

	// Build output buffer
	var buf []byte
	buf = append(buf, headerWithCRC...)

	// Add data blocks, each followed by its own 2-byte CRC.
	//
	// NOTE: we must NOT use appendCRC on a sub-slice of f.Payload here.
	// f.Payload[offset:end] shares the payload's backing array, so
	// appendCRC's append() would write the CRC bytes *into* the following
	// block's data, corrupting every block after the first. Append the
	// data and CRC to buf directly instead.
	offset := 0
	for offset < userDataLen {
		end := offset + blockSize
		if end > userDataLen {
			end = userDataLen
		}
		block := f.Payload[offset:end]
		buf = append(buf, block...)
		c := crcDNP(block)
		buf = append(buf, byte(c), byte(c>>8))
		offset = end
	}

	_, err := w.Write(buf)
	return err
}

// IsFromMaster returns true if the control byte indicates a master→outstation message.
func (f *LinkFrame) IsFromMaster() bool {
	return f.Control&ctrlDIR != 0 && f.Control&ctrlPRM != 0
}

// MakeResponseControl builds a control byte for an outstation response.
func MakeResponseControl() uint8 {
	return ctrlFCUnconfirmedData // DIR=0, PRM=0, FC=4
}
