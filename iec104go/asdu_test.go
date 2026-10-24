// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"reflect"
	"testing"
)

func TestWidths(t *testing.T) {
	for cot := 1; cot <= 2; cot++ {
		for ca := 1; ca <= 2; ca++ {
			for ioa := 1; ioa <= 3; ioa++ {
				for _, sq := range []bool{false, true} {
					t.Run(fmt.Sprintf("%d/%d/%d/SQ=%v", cot, ca, ioa, sq), func(t *testing.T) {
						p := Params{COTSize: cot, CASize: ca, IOASize: ioa}
						h := Header{Type: M_SP_NA_1, Sequence: sq, Cause: Activation, CommonAddress: uint16(maxAddress(ca))}
						if cot == 2 {
							h.Originator = 0xd7
						}
						first := maxAddress(ioa) - 1
						a := ASDU{Header: h, Objects: []Object{{Address: first, Element: SinglePoint{SIQ: 1}}, {Address: first + 1, Element: SinglePoint{SIQ: 0x90}}}}
						b, err := a.MarshalBinary(p)
						if err != nil {
							t.Fatal(err)
						}
						decoded, err := DecodeASDU(b, p)
						if err != nil {
							t.Fatal(err)
						}
						a.Header.Count = 2
						if !reflect.DeepEqual(decoded, &a) {
							t.Fatalf("got %#v want %#v", decoded, a)
						}
						wire, err := decoded.MarshalBinary(p)
						if err != nil || !bytes.Equal(b, wire) {
							t.Fatalf("reencode: %x %v", wire, err)
						}
						// Nonzero OA is an error, never silently discarded in the 1-octet profile.
						if cot == 1 {
							a.Header.Originator = 1
							if _, err := a.MarshalBinary(p); err == nil {
								t.Fatal("discarded OA")
							}
						}
						a.Objects[1].Address = maxAddress(ioa) + 1
						if _, err := a.MarshalBinary(p); err == nil {
							t.Fatal("address overflow")
						}
					})
				}
			}
		}
	}
}

func TestMalformedASDU(t *testing.T) {
	p := DefaultParams()
	valid := unhex(t, "010103000100ffffff81")
	for n := 0; n < len(valid); n++ {
		t.Run(fmt.Sprintf("truncated/%d", n), func(t *testing.T) {
			if _, err := DecodeASDU(valid[:n], p); !errors.Is(err, ErrMalformed) {
				t.Fatalf("err=%v", err)
			}
		})
	}
	cases := []struct{ name, wire string }{
		{"count-zero", "010003000100ffffff81"},
		{"trailing", "010103000100ffffff8100"},
		{"sq-overflow", "018203000100ffffff0102"},
		{"sq-truncated", "01820300010000000001"},
		{"count127-short", "017f0300010000000001"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := DecodeASDU(unhex(t, tc.wire), p); !errors.Is(err, ErrMalformed) {
				t.Fatalf("err=%v", err)
			}
		})
	}
	for _, p := range []Params{{}, {0, 2, 3}, {3, 2, 3}, {2, 0, 3}, {2, 3, 3}, {2, 2, 0}, {2, 2, 4}} {
		if _, err := DecodeASDU(valid, p); err == nil {
			t.Fatalf("accepted params %+v", p)
		}
		if _, err := NewBuilder(p, Header{Type: M_SP_NA_1}); err == nil {
			t.Fatalf("builder accepted params %+v", p)
		}
	}
	if _, err := DecodeASDU(make([]byte, 250), p); !errors.Is(err, ErrMalformed) {
		t.Fatalf("oversize: %v", err)
	}
	for _, a := range []ASDU{
		{Header: Header{Type: M_SP_NA_1}},
		{Header: Header{Type: M_SP_NA_1, Cause: 64}, Objects: []Object{{Element: SinglePoint{}}}},
		{Header: Header{Type: M_SP_NA_1}, Objects: []Object{{Element: DoublePoint{}}}},
		{Header: Header{Type: M_SP_NA_1}, Objects: []Object{{Element: nil}}},
		{Header: Header{Type: M_SP_NA_1}, Objects: []Object{{Element: (*SinglePoint)(nil)}}},
		{Header: Header{Type: M_SP_TB_1}, Objects: []Object{{Element: Timed{Value: Timed{}}}}},
		{Header: Header{Type: M_EP_TD_1}, Objects: []Object{{Element: ProtectionEvent{}}}},
		{Header: Header{Type: M_SP_NA_1, Sequence: true}, Objects: []Object{{Address: 1, Element: SinglePoint{}}, {Address: 3, Element: SinglePoint{}}}},
	} {
		if _, err := a.MarshalBinary(p); !errors.Is(err, ErrMalformed) {
			t.Fatalf("accepted %+v: %v", a, err)
		}
	}
}

func TestRawASDU(t *testing.T) {
	for _, id := range []byte{0, 2, 4, 17, 104, 106, 200, 255} {
		wire := []byte{id, 0x81, 0xef, 0x42, 0x34, 0x12, 0xde, 0xad, 0xbe, 0xef}
		m, err := DecodeASDU(wire, DefaultParams())
		if err != nil {
			t.Fatal(err)
		}
		raw, ok := m.(*RawASDU)
		if !ok || !errors.Is(raw.DecodeError, ErrUnsupportedType) {
			t.Fatalf("not raw: %#v", m)
		}
		if h := raw.GetHeader(); h.Type != TypeID(id) || h.Cause != UnknownIOA || !h.Negative || !h.Test || h.Originator != 0x42 || h.CommonAddress != 0x1234 {
			t.Fatalf("header=%+v", h)
		}
		original := append([]byte(nil), wire...)
		for i := range wire {
			wire[i] = 0
		}
		encoded, err := raw.MarshalBinary(DefaultParams())
		if err != nil || !bytes.Equal(original, encoded) {
			t.Fatalf("raw aliases input: %x %v", encoded, err)
		}
		encoded[6] = 0
		if raw.Payload[0] != 0xde {
			t.Fatal("encoder aliases raw")
		}
	}
	raw := RawASDU{Header: Header{Type: 255, Count: 0}}
	if _, err := raw.MarshalBinary(DefaultParams()); err == nil {
		t.Fatal("raw count zero")
	}
	raw.Header.Count = 1
	raw.Payload = make([]byte, 244)
	if _, err := raw.MarshalBinary(DefaultParams()); !errors.Is(err, ErrFull) {
		t.Fatalf("raw oversize: %v", err)
	}
}

func TestBuilderLimits(t *testing.T) {
	for _, tc := range []struct {
		name        string
		id          TypeID
		element     Element
		sq          bool
		count, size int
	}{
		{"floats", M_ME_NC_1, Float{Value: 1.25}, false, 30, 246},
		{"float-sequence", M_ME_NC_1, Float{Value: 1.25}, true, 48, 249},
		{"count127", M_SP_NA_1, SinglePoint{SIQ: 1}, true, 127, 136},
		{"APDU253", C_RD_NA_1, ReadCommand{}, false, 81, 249},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, err := NewBuilder(DefaultParams(), Header{Type: tc.id, Sequence: tc.sq, Cause: InterrogatedStation, Count: 99})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := b.ASDU().MarshalBinary(DefaultParams()); err == nil {
				t.Fatal("empty builder encoded")
			}
			for i := 0; i < tc.count; i++ {
				if err := b.Append(Object{Address: uint32(i), Element: tc.element}); err != nil {
					t.Fatalf("append %d: %v", i, err)
				}
			}
			before := b.ASDU()
			if err := b.Append(Object{Address: uint32(tc.count), Element: tc.element}); !errors.Is(err, ErrFull) {
				t.Fatalf("expected full: %v", err)
			}
			if !reflect.DeepEqual(b.ASDU(), before) {
				t.Fatal("failed append mutated builder")
			}
			wire, err := before.MarshalBinary(DefaultParams())
			if err != nil || len(wire) != tc.size {
				t.Fatalf("size=%d want=%d err=%v", len(wire), tc.size, err)
			}
			m, err := DecodeASDU(wire, DefaultParams())
			if err != nil || m.GetHeader().Count != uint8(tc.count) {
				t.Fatalf("boundary decode %v", err)
			}
			f := Frame{Kind: IFrame, Send: 32767, Receive: 32767, ASDU: wire}
			apdu, err := f.MarshalBinary()
			if err != nil || len(apdu) != tc.size+6 {
				t.Fatalf("APDU %d %v", len(apdu), err)
			}
			if tc.size == 249 && apdu[1] != 253 {
				t.Fatal("length must be 253")
			}
			before.Objects[0].Address = 123
			if b.ASDU().Objects[0].Address != 0 {
				t.Fatal("snapshot aliases builder")
			}
		})
	}
	b, err := NewBuilder(DefaultParams(), Header{Type: M_SP_NA_1, Sequence: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Append(Object{Address: 0xffffff, Element: SinglePoint{}}); err != nil {
		t.Fatal(err)
	}
	for _, o := range []Object{{Address: 0x1000000, Element: SinglePoint{}}, {Address: 0, Element: SinglePoint{}}, {Element: DoublePoint{}}} {
		if err := b.Append(o); !errors.Is(err, ErrMalformed) {
			t.Fatalf("invalid append: %v", err)
		}
	}
	if len(b.ASDU().Objects) != 1 {
		t.Fatal("invalid append changed count")
	}
	if _, err := NewBuilder(DefaultParams(), Header{Type: 255}); !errors.Is(err, ErrUnsupportedType) {
		t.Fatalf("unknown builder: %v", err)
	}
}

func TestFloatBitPreservation(t *testing.T) {
	for _, bits := range []uint32{0, 0x80000000, 1, 0x7f800000, 0xff800000, 0x7f800001, 0x7fc12345, 0xffc12345} {
		a := ASDU{Header: Header{Type: M_ME_NC_1}, Objects: []Object{{Element: Float{Value: math.Float32frombits(bits)}}}}
		wire, err := a.MarshalBinary(DefaultParams())
		if err != nil {
			t.Fatal(err)
		}
		m, err := DecodeASDU(wire, DefaultParams())
		if err != nil {
			t.Fatal(err)
		}
		f := m.(*ASDU).Objects[0].Element.(Float)
		if got := math.Float32bits(f.Value); got != bits {
			t.Fatalf("got %x want %x", got, bits)
		}
	}
}

func TestDescriptorsAndNames(t *testing.T) {
	for _, position := range []int8{-64, -1, 0, 63} {
		for _, transient := range []bool{false, true} {
			v, err := NewVTI(position, transient)
			if err != nil || v.Position() != position || v.Transient() != transient {
				t.Fatalf("VTI %v %v", v, err)
			}
		}
	}
	for _, n := range []int8{-128, -65, 64, 127} {
		if _, err := NewVTI(n, false); err == nil {
			t.Fatal("VTI wrapped")
		}
	}
	q, err := NewCommandQualifier(2, 31, true)
	if err != nil || q.State() != 2 || q.QU() != 31 || !q.Select() {
		t.Fatalf("command %v %v", q, err)
	}
	for _, v := range [][2]uint8{{4, 0}, {0, 32}} {
		if _, err := NewCommandQualifier(v[0], v[1], false); err == nil {
			t.Fatal("command wrapped")
		}
	}
	if !SIQ(0xf1).State() || SIQ(0xf1).Quality() != 0xf0 || DIQ(0xf2).State() != 2 || DIQ(0xf2).Quality() != 0xf0 || SetpointQualifier(0xfe).QL() != 126 || !SetpointQualifier(0xfe).Select() {
		t.Fatal("descriptor masks")
	}
	if M_DP_TB_1.String() != "M_DP_TB_1" || Spontaneous.String() != "spont" || ActivationCon.String() != "actcon" || COT(21).String() != "inro1" || COT(36).String() != "inro16" || COT(38).String() != "reqco1" || COT(41).String() != "reqco4" || TypeID(255).String() != "TypeID(255)" || COT(63).String() != "COT(63)" {
		t.Fatal("mnemonics")
	}
}
