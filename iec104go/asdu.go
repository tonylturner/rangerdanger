// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"errors"
	"fmt"
)

// ErrUnsupportedType marks types outside the codec profile. DecodeASDU returns
// a RawASDU with this error attached, not a protocol error to close a session.
var ErrUnsupportedType = errors.New("iec104go: unsupported type")

// Params fixes addressing widths for one connection. Zero is not a default;
// use DefaultParams for the IEC 104 profile. COT/CA allow 1..2, IOA 1..3 octets.
type Params struct{ COTSize, CASize, IOASize int }

// DefaultParams returns the IEC 104 COT=2, CA=2, IOA=3 profile.
func DefaultParams() Params { return Params{COTSize: 2, CASize: 2, IOASize: 3} }

// Validate rejects ambiguous or unsupported field widths.
func (p Params) Validate() error {
	if p.COTSize < 1 || p.COTSize > 2 || p.CASize < 1 || p.CASize > 2 || p.IOASize < 1 || p.IOASize > 3 {
		return fmt.Errorf("%w: address widths", ErrMalformed)
	}
	return nil
}
func (p Params) headerSize() int  { return 2 + p.COTSize + p.CASize }
func maxAddress(width int) uint32 { return uint32(1)<<(8*width) - 1 }

// Header contains the ASDU envelope. Cause excludes the Test and Negative
// flags. Originator is always explicit, and must be zero for a one-octet COT.
// Count is authoritative for RawASDU; ASDU encoders derive it from Objects.
type Header struct {
	Type           TypeID
	Sequence       bool
	Count          uint8
	Cause          COT
	Negative, Test bool
	Originator     uint8
	CommonAddress  uint16
}

// Object associates an IOA with one typed element. Generic SQ addresses advance;
// directory entries (type126) instead share the single directory IOA.
type Object struct {
	Address uint32
	Element Element
}

// Message is either an ASDU or RawASDU. The latter lets applications respond
// to unsupported types without the codec choosing COT rejection policy.
type Message interface {
	GetHeader() Header
	MarshalBinary(Params) ([]byte, error)
}

// ASDU contains supported, decoded information objects. Elements are value
// types. The caller must synchronize its own mutations before concurrent use.
type ASDU struct {
	Header  Header
	Objects []Object
}

// GetHeader returns the envelope with the object count derived from Objects.
// MarshalBinary rejects counts outside 1..127 before they reach the wire.
func (a ASDU) GetHeader() Header { h := a.Header; h.Count = uint8(len(a.Objects)); return h }

// RawASDU preserves an unsupported type's complete header and owned payload.
// DecodeError explains why its elements were not interpreted. Structural
// malformation instead returns a non-nil error directly from DecodeASDU.
type RawASDU struct {
	Header      Header
	Payload     []byte
	DecodeError error
}

// GetHeader returns the unmodified raw envelope.
func (a RawASDU) GetHeader() Header { return a.Header }

func appendAddress(b []byte, a uint32, width int) []byte {
	for i := 0; i < width; i++ {
		b = append(b, byte(a>>(8*i)))
	}
	return b
}
func readAddress(b []byte, width int) uint32 {
	var a uint32
	for i := 0; i < width; i++ {
		a |= uint32(b[i]) << (8 * i)
	}
	return a
}

func encodeHeader(p Params, h Header) ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	if h.Count == 0 || h.Count > 127 || h.Cause > 63 || uint32(h.CommonAddress) > maxAddress(p.CASize) || (p.COTSize == 1 && h.Originator != 0) {
		return nil, fmt.Errorf("%w: ASDU header", ErrMalformed)
	}
	vsq := h.Count
	if h.Sequence {
		vsq |= 128
	}
	cause := byte(h.Cause)
	if h.Negative {
		cause |= 64
	}
	if h.Test {
		cause |= 128
	}
	b := []byte{byte(h.Type), vsq, cause}
	if p.COTSize == 2 {
		b = append(b, h.Originator)
	}
	return appendAddress(b, uint32(h.CommonAddress), p.CASize), nil
}

// MarshalBinary encodes all objects, rejecting mixed types, oversized ASDUs,
// and SQ=1 discontinuities. It never modifies Objects or their elements.
func (a ASDU) MarshalBinary(p Params) ([]byte, error) {
	if len(a.Objects) == 0 || len(a.Objects) > 127 {
		return nil, fmt.Errorf("%w: ASDU count", ErrMalformed)
	}
	if err := fileStructure(a.GetHeader()); err != nil {
		return nil, err
	}
	b, err := encodeHeader(p, a.GetHeader())
	if err != nil {
		return nil, err
	}
	for i, o := range a.Objects {
		if o.Address > maxAddress(p.IOASize) {
			return nil, errFieldRange
		}
		if a.Header.Sequence && i > 0 && o.Address != objectAddress(a.Header.Type, a.Objects[0].Address, i) {
			return nil, fmt.Errorf("%w: SQ address discontinuity", ErrMalformed)
		}
		id, payload, err := encodeElement(o.Element)
		if err != nil {
			return nil, err
		}
		if id != a.Header.Type {
			return nil, fmt.Errorf("%w: mixed element types", ErrMalformed)
		}
		if !a.Header.Sequence || i == 0 {
			b = appendAddress(b, o.Address, p.IOASize)
		}
		b = append(b, payload...)
		if len(b) > MaxASDUSize {
			return nil, ErrFull
		}
	}
	return b, nil
}

// MarshalBinary re-encodes an opaque payload; it does not try to reinterpret
// the type, allowing applications to mirror unsupported requests unchanged.
func (a RawASDU) MarshalBinary(p Params) ([]byte, error) {
	b, err := encodeHeader(p, a.Header)
	if err != nil {
		return nil, err
	}
	if len(a.Payload) > MaxASDUSize-len(b) {
		return nil, ErrFull
	}
	return append(b, a.Payload...), nil
}

// DecodeASDU requires an exact ASDU with no trailing bytes. Unsupported types
// return (*RawASDU,nil); structural errors wrap ErrMalformed. Successful
// results own their bytes and can be retained across subsequent reader calls.
// Single-object file ASDUs accept either SQ bit and normalize it to the strict
// sending profile (directory SQ1, other file types SQ0).
func DecodeASDU(b []byte, p Params) (Message, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	hs := p.headerSize()
	if len(b) < hs || len(b) > MaxASDUSize || b[1]&127 == 0 {
		return nil, fmt.Errorf("%w: ASDU size/count", ErrMalformed)
	}
	h := Header{Type: TypeID(b[0]), Sequence: b[1]&128 != 0, Count: b[1] & 127, Cause: COT(b[2] & 63), Negative: b[2]&64 != 0, Test: b[2]&128 != 0}
	if p.COTSize == 2 {
		h.Originator = b[3]
	}
	h.CommonAddress = uint16(readAddress(b[2+p.COTSize:], p.CASize))
	payload := b[hs:]
	if !h.Type.Supported() {
		return &RawASDU{Header: h, Payload: append([]byte(nil), payload...), DecodeError: fmt.Errorf("%w: %d", ErrUnsupportedType, h.Type)}, nil
	}
	if isFileType(h.Type) && h.Count == 1 {
		h.Sequence = h.Type == F_DR_TA_1
	}
	if err := fileStructure(h); err != nil {
		return nil, err
	}
	a := &ASDU{Header: h, Objects: make([]Object, 0, h.Count)}
	var first uint32
	for i := 0; i < int(h.Count); i++ {
		var address uint32
		if !h.Sequence || i == 0 {
			if len(payload) < p.IOASize {
				return nil, fmt.Errorf("%w: missing IOA", ErrMalformed)
			}
			address = readAddress(payload, p.IOASize)
			payload = payload[p.IOASize:]
			first = address
		} else {
			address = objectAddress(h.Type, first, i)
		}
		if address > maxAddress(p.IOASize) {
			return nil, fmt.Errorf("%w: SQ address overflow", ErrMalformed)
		}
		size, err := elementLength(h.Type, payload)
		if err != nil {
			return nil, err
		}
		e, err := decodeElement(h.Type, payload[:size])
		if err != nil {
			return nil, err
		}
		payload = payload[size:]
		a.Objects = append(a.Objects, Object{Address: address, Element: e})
	}
	if len(payload) != 0 {
		return nil, fmt.Errorf("%w: trailing ASDU bytes", ErrMalformed)
	}
	return a, nil
}
