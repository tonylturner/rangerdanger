// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

func TestLabBitFields(t *testing.T) {
	tag := CP56FromTime(time.Date(2026, 10, 9, 14, 55, 55, 687000000, time.UTC), true)
	for _, tc := range []struct {
		id          TypeID
		makeElement func(uint8) Element
	}{
		{M_SP_NA_1, func(v uint8) Element { return SinglePoint{SIQ: SIQ(v)} }},
		{M_DP_NA_1, func(v uint8) Element { return DoublePoint{DIQ: DIQ(v)} }},
		{M_ST_NA_1, func(v uint8) Element { return StepPosition{Value: VTI(v), Quality: Quality(v ^ 0xa5)} }},
		{M_ME_NC_1, func(v uint8) Element { return Float{Value: -182.5, Quality: Quality(v)} }},
		{M_SP_TB_1, func(v uint8) Element { return Timed{Value: SinglePoint{SIQ: SIQ(v)}, Time: tag} }},
		{M_DP_TB_1, func(v uint8) Element { return Timed{Value: DoublePoint{DIQ: DIQ(v)}, Time: tag} }},
		{M_ST_TB_1, func(v uint8) Element {
			return Timed{Value: StepPosition{Value: VTI(v), Quality: Quality(v ^ 0xa5)}, Time: tag}
		}},
		{C_SC_NA_1, func(v uint8) Element { return SingleCommand{Qualifier: CommandQualifier(v)} }},
		{C_DC_NA_1, func(v uint8) Element { return DoubleCommand{Qualifier: CommandQualifier(v)} }},
		{C_RC_NA_1, func(v uint8) Element { return RegulatingCommand{Qualifier: CommandQualifier(v)} }},
		{C_IC_NA_1, func(v uint8) Element { return Interrogation{QOI: v} }},
	} {
		t.Run(tc.id.String(), func(t *testing.T) {
			// All 256 descriptors include reserved bits and application-invalid command
			// states/qualifiers. The codec must preserve, not decide station policy.
			for v := 0; v < 256; v++ {
				a := ASDU{Header: Header{Type: tc.id, Cause: Spontaneous, CommonAddress: 1}, Objects: []Object{{Address: 0xffffff, Element: tc.makeElement(uint8(v))}}}
				b, err := a.MarshalBinary(DefaultParams())
				if err != nil {
					t.Fatal(err)
				}
				m, err := DecodeASDU(b, DefaultParams())
				if err != nil {
					t.Fatal(err)
				}
				e := m.(*ASDU).Objects[0].Element
				gotID, got, err := encodeElement(e)
				if err != nil {
					t.Fatal(err)
				}
				wantID, want, err := encodeElement(tc.makeElement(uint8(v)))
				if err != nil {
					t.Fatal(err)
				}
				if gotID != wantID || !bytes.Equal(got, want) {
					t.Fatalf("descriptor %x: %x want %x", v, got, want)
				}
			}
		})
	}
}

func TestASDUCapacityRejection(t *testing.T) {
	p := DefaultParams()
	a := ASDU{Header: Header{Type: M_SP_NA_1, Sequence: true}, Objects: make([]Object, 128)}
	for i := range a.Objects {
		a.Objects[i] = Object{Address: uint32(i), Element: SinglePoint{}}
	}
	if _, err := a.MarshalBinary(p); !errors.Is(err, ErrMalformed) {
		t.Fatalf("count128: %v", err)
	}
	a.Header.Sequence = false
	a.Objects = a.Objects[:61]
	if _, err := a.MarshalBinary(p); !errors.Is(err, ErrFull) {
		t.Fatalf("ASDU250: %v", err)
	}
	for _, width := range []int{1, 2, 3} {
		p.IOASize = width
		// Last possible address followed by one more element must never wrap.
		b := []byte{1, 0x82, 3, 0, 1, 0}
		for i := 0; i < width; i++ {
			b = append(b, 0xff)
		}
		b = append(b, 1, 2)
		if _, err := DecodeASDU(b, p); !errors.Is(err, ErrMalformed) {
			t.Fatalf("SQ overflow width%d: %v", width, err)
		}
	}
}
