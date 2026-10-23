// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// MaxASDUSize is the 104 ASDU limit, excluding the four control octets.
const MaxASDUSize = 249

// ErrMalformed identifies structural wire errors, not unsupported types.
var ErrMalformed = errors.New("iec104go: malformed encoding")

// FrameKind distinguishes the three APCI control formats.
type FrameKind uint8

const (
	// IFrame carries numbered application data.
	IFrame FrameKind = iota
	// SFrame acknowledges received application data.
	SFrame
	// UFrame carries an unnumbered transfer-control function.
	UFrame
)

// UFunction is the complete first control octet of a U frame.
type UFunction uint8

const (
	// StartDTAct requests data transfer.
	StartDTAct UFunction = 0x07
	// StartDTCon confirms data transfer.
	StartDTCon UFunction = 0x0b
	// StopDTAct requests stopped transfer.
	StopDTAct UFunction = 0x13
	// StopDTCon confirms stopped transfer.
	StopDTCon UFunction = 0x23
	// TestFRAct requests link supervision confirmation.
	TestFRAct UFunction = 0x43
	// TestFRCon confirms link supervision.
	TestFRCon UFunction = 0x83
)

func (u UFunction) valid() bool {
	switch u {
	case StartDTAct, StartDTCon, StopDTAct, StopDTCon, TestFRAct, TestFRCon:
		return true
	}
	return false
}

// Frame holds an APCI and, for I frames only, owned ASDU bytes. Send and
// Receive are unshifted 15-bit counters. Unused fields must be zero.
type Frame struct {
	Kind          FrameKind
	Send, Receive uint16
	Function      UFunction
	ASDU          []byte
}

// MarshalBinary encodes a complete APDU, rejecting reserved control bits.
func (f Frame) MarshalBinary() ([]byte, error) {
	if f.Send > 32767 || f.Receive > 32767 {
		return nil, fmt.Errorf("%w: counter exceeds 15 bits", ErrMalformed)
	}
	b := make([]byte, 6, 255)
	b[0] = 0x68
	switch f.Kind {
	case IFrame:
		if f.Function != 0 || len(f.ASDU) == 0 || len(f.ASDU) > MaxASDUSize {
			return nil, fmt.Errorf("%w: I frame payload/function", ErrMalformed)
		}
		binary.LittleEndian.PutUint16(b[2:], f.Send<<1)
		binary.LittleEndian.PutUint16(b[4:], f.Receive<<1)
		b = append(b, f.ASDU...)
	case SFrame:
		if f.Send != 0 || f.Function != 0 || len(f.ASDU) != 0 {
			return nil, fmt.Errorf("%w: S frame fields", ErrMalformed)
		}
		b[2] = 1
		binary.LittleEndian.PutUint16(b[4:], f.Receive<<1)
	case UFrame:
		if f.Send != 0 || f.Receive != 0 || len(f.ASDU) != 0 || !f.Function.valid() {
			return nil, fmt.Errorf("%w: U frame fields", ErrMalformed)
		}
		b[2] = byte(f.Function)
	default:
		return nil, fmt.Errorf("%w: frame kind", ErrMalformed)
	}
	b[1] = byte(len(b) - 2)
	return b, nil
}

// DecodeFrame requires exactly one complete APDU; it never aliases b.
func DecodeFrame(b []byte) (Frame, error) {
	bad := func() (Frame, error) { return Frame{}, fmt.Errorf("%w: APCI", ErrMalformed) }
	if len(b) < 6 || b[0] != 0x68 || b[1] < 4 || b[1] > 253 || int(b[1])+2 != len(b) {
		return bad()
	}
	if b[2]&1 == 0 {
		if len(b) == 6 || b[4]&1 != 0 {
			return bad()
		}
		return Frame{Kind: IFrame, Send: binary.LittleEndian.Uint16(b[2:]) >> 1, Receive: binary.LittleEndian.Uint16(b[4:]) >> 1, ASDU: append([]byte(nil), b[6:]...)}, nil
	}
	if len(b) != 6 {
		return bad()
	}
	if b[2] == 1 && b[3] == 0 && b[4]&1 == 0 {
		return Frame{Kind: SFrame, Receive: binary.LittleEndian.Uint16(b[4:]) >> 1}, nil
	}
	if UFunction(b[2]).valid() && b[3] == 0 && b[4] == 0 && b[5] == 0 {
		return Frame{Kind: UFrame, Function: UFunction(b[2])}, nil
	}
	return bad()
}

// APDUReader reads exact APDU boundaries from a TCP-like stream. It does not
// resynchronize after malformed input: a session must close rather than hide
// corrupt framing. The caller owns deadlines and concurrent access.
type APDUReader struct{ reader io.Reader }

// NewAPDUReader wraps r without read-ahead, allowing taps to observe each APDU.
func NewAPDUReader(r io.Reader) *APDUReader { return &APDUReader{reader: r} }

// ReadFrame returns io.EOF only at a clean boundary, io.ErrUnexpectedEOF for
// truncation, and ErrMalformed for bad APCI. Each frame owns its ASDU bytes.
func (r *APDUReader) ReadFrame() (Frame, error) {
	var b [255]byte
	if _, err := io.ReadFull(r.reader, b[:2]); err != nil {
		return Frame{}, err
	}
	n := int(b[1])
	if b[0] != 0x68 || n < 4 || n > 253 {
		return Frame{}, fmt.Errorf("%w: APDU prefix", ErrMalformed)
	}
	if _, err := io.ReadFull(r.reader, b[2:n+2]); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return Frame{}, err
	}
	return DecodeFrame(b[:n+2])
}
