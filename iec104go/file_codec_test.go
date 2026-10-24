// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

// Independent wire-position literals, derived from N §§7.3.6.1–7 and I §8.9,
// not extracted from an implementation or claimed to be historical captures.
func fileGoldens(t testing.TB) []elementVector {
	tag, err := DecodeCP56Time2a(unhex(t, "3412c5e6a7b8d9"))
	if err != nil {
		t.Fatal(err)
	}
	return []elementVector{
		{F_FR_NA_1, FileReady{0x1234, 0x563412, 0x81}, "341212345681"},
		{F_SR_NA_1, SectionReady{0x1234, 0x78, 0x563412, 0x82}, "34127812345682"},
		{F_SC_NA_1, FileCall{0x1234, 0x78, 0x56}, "34127856"},
		{F_LS_NA_1, FileLast{0x1234, 0x78, 3, 0xbc}, "34127803bc"},
		{F_AF_NA_1, FileACK{0x1234, 0x78, 0x24}, "34127824"},
		{F_SG_NA_1, FileSegment{0x1234, 0x78, []byte{0x9a, 0xbc, 0xde}}, "341278039abcde"},
		{F_DR_TA_1, FileDirectory{0x1234, 0x563412, 0xe1, tag}, "3412123456e13412c5e6a7b8d9"},
		{F_SC_NB_1, QueryLog{0x1234, tag, CP56Time2a{}}, "34123412c5e6a7b8d900000000000000"},
	}
}
func fileGoldenASDU(t testing.TB, v elementVector) []byte {
	sq := byte(1)
	cause := byte(FileTransfer)
	if v.id == F_DR_TA_1 {
		sq = 0x81
		cause = byte(Request)
	}
	return append([]byte{byte(v.id), sq, cause, 0x42, 0x34, 0x12, 0xff, 0xff, 0xff}, unhex(t, v.hex)...)
}
func TestFileGoldenVectors(t *testing.T) {
	for _, v := range fileGoldens(t) {
		t.Run(v.id.String(), func(t *testing.T) {
			want := fileGoldenASDU(t, v)
			a := ASDU{Header: Header{Type: v.id, Sequence: v.id == F_DR_TA_1, Cause: FileTransfer, Originator: 0x42, CommonAddress: 0x1234}, Objects: []Object{{0xffffff, v.value}}}
			if v.id == F_DR_TA_1 {
				a.Header.Cause = Request
			}
			wire, err := a.MarshalBinary(DefaultParams())
			if err != nil || !bytes.Equal(wire, want) {
				t.Fatalf("%x want %x: %v", wire, want, err)
			}
			m, err := DecodeASDU(want, DefaultParams())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(m.(*ASDU).Objects[0].Element, v.value) {
				t.Fatalf("element=%#v want=%#v", m, v.value)
			}
			for i := range want {
				want[i] = 0
			}
			round, err := m.MarshalBinary(DefaultParams())
			if err != nil || !bytes.Equal(round, wire) {
				t.Fatalf("ownership/roundtrip %x %v", round, err)
			}
			for n := 0; n < len(wire); n++ {
				if _, err := DecodeASDU(wire[:n], DefaultParams()); !errors.Is(err, ErrMalformed) {
					t.Fatalf("truncated %d: %v", n, err)
				}
			}
			if _, err := DecodeASDU(append(wire, 0), DefaultParams()); !errors.Is(err, ErrMalformed) {
				t.Fatalf("trailing: %v", err)
			}
			for _, badVSQ := range []byte{0, 2, 0x82} {
				b := append([]byte(nil), wire...)
				b[1] = badVSQ
				if _, err := DecodeASDU(b, DefaultParams()); err == nil {
					t.Fatalf("accepted VSQ %x", badVSQ)
				}
			}
		})
	}
}
func TestFileDirectorySharedIOAAndBuilder(t *testing.T) {
	p := DefaultParams()
	b, err := NewBuilder(p, Header{Type: F_DR_TA_1, Sequence: true, Cause: Request})
	if err != nil {
		t.Fatal(err)
	}
	// The maximum IOA must not overflow when a directory has multiple entries.
	for i := 0; i < 18; i++ {
		if err := b.Append(Object{0xffffff, FileDirectory{Name: uint16(i + 1), Length: 1}}); err != nil {
			t.Fatal(err)
		}
	}
	before := b.ASDU()
	if err := b.Append(Object{0xffffff, FileDirectory{Length: 1}}); !errors.Is(err, ErrFull) {
		t.Fatal(err)
	}
	wire, err := before.MarshalBinary(p)
	if err != nil || len(wire) != 243 {
		t.Fatalf("len=%d %v", len(wire), err)
	}
	m, err := DecodeASDU(wire, p)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range m.(*ASDU).Objects {
		if o.Address != 0xffffff {
			t.Fatal("directory IOA advanced")
		}
	}
	if err := b.Append(Object{1, FileDirectory{Length: 1}}); !errors.Is(err, ErrMalformed) {
		t.Fatal(err)
	}
}
func TestFileSegmentLimitsAndOwnership(t *testing.T) {
	for cot := 1; cot <= 2; cot++ {
		for ca := 1; ca <= 2; ca++ {
			for ioa := 1; ioa <= 3; ioa++ {
				p := Params{cot, ca, ioa}
				n := 249 - p.headerSize() - ioa - 4
				b, err := NewBuilder(p, Header{Type: F_SG_NA_1})
				if err != nil {
					t.Fatal(err)
				}
				source := bytes.Repeat([]byte{0x7b}, n)
				if err := b.Append(Object{0, FileSegment{1, 255, source}}); err != nil {
					t.Fatal(err)
				}
				source[0] = 0
				snap := b.ASDU()
				if snap.Objects[0].Element.(FileSegment).Data[0] != 0x7b {
					t.Fatal("append aliases")
				}
				mirror, err := Mirror(snap, FileTransfer, false)
				if err != nil {
					t.Fatal(err)
				}
				snap.Objects[0].Element.(FileSegment).Data[0] = 2
				if mirror.(*ASDU).Objects[0].Element.(FileSegment).Data[0] != 0x7b || b.ASDU().Objects[0].Element.(FileSegment).Data[0] != 0x7b {
					t.Fatal("snapshot/mirror aliases")
				}
				wire, err := mirror.MarshalBinary(p)
				if err != nil || len(wire) != 249 {
					t.Fatal(len(wire), err)
				}
				if _, err := DecodeASDU(wire, p); err != nil {
					t.Fatal(err)
				}
				a := b.ASDU()
				seg := a.Objects[0].Element.(FileSegment)
				seg.Data = append(seg.Data, 1)
				a.Objects[0].Element = seg
				if _, err := a.MarshalBinary(p); !errors.Is(err, ErrFull) {
					t.Fatal("oversize", err)
				}
			}
		}
	}
	for _, v := range []Element{FileReady{Length: 0x1000000}, SectionReady{Length: 0x1000000}, FileDirectory{Length: 0x1000000}, FileSegment{Data: make([]byte, 256)}, (*FileSegment)(nil)} {
		if _, _, err := encodeElement(v); !errors.Is(err, ErrMalformed) {
			t.Fatalf("accepted %#v %v", v, err)
		}
	}
	if FileChecksum([]byte{255, 2}) != 1 {
		t.Fatal("checksum is not modulo256")
	}
	// Unused/reserved values remain lossless codec data, not fabricated procedures.
	for _, e := range []Element{FileSegment{}, FileReady{}, SectionReady{}, QueryLog{}} {
		id, _, err := encodeElement(e)
		if err != nil {
			t.Fatal(err)
		}
		a := fileMessage(FileKey{}, e, FileTransfer)
		if !id.Supported() {
			t.Fatal(id)
		}
		w, err := a.MarshalBinary(DefaultParams())
		if err != nil {
			t.Fatal(err)
		}
		m, err := DecodeASDU(w, DefaultParams())
		if err != nil {
			t.Fatal(err)
		}
		r, _ := m.MarshalBinary(DefaultParams())
		if !bytes.Equal(w, r) {
			t.Fatal("raw fields changed")
		}
	}
}
func TestFileCoverage(t *testing.T) {
	seen := map[TypeID]bool{}
	for _, v := range fileGoldens(t) {
		seen[v.id] = true
		if !v.id.Supported() {
			t.Fatal(v.id)
		}
	}
	for id := TypeID(120); id <= 127; id++ {
		if !seen[id] {
			t.Fatal(fmt.Sprint("missing ", id))
		}
	}
}

func TestFileDecodeSingleObjectVSQ(t *testing.T) {
	for _, v := range fileGoldens(t) {
		t.Run(v.id.String(), func(t *testing.T) {
			canonical := fileGoldenASDU(t, v)
			alternate := append([]byte(nil), canonical...)
			alternate[1] ^= 0x80
			m, err := DecodeASDU(alternate, DefaultParams())
			if err != nil {
				t.Fatal("unambiguous single-object VSQ rejected", err)
			}
			if !reflect.DeepEqual(m.(*ASDU).Objects[0].Element, v.value) {
				t.Fatal("element changed")
			}
			wire, err := m.MarshalBinary(DefaultParams())
			if err != nil || !bytes.Equal(wire, canonical) {
				t.Fatalf("did not canonicalize SQ: %x %v", wire, err)
			}
			a := *m.(*ASDU)
			a.Header.Sequence = !a.Header.Sequence
			if _, err := a.MarshalBinary(DefaultParams()); !errors.Is(err, ErrMalformed) {
				t.Fatal("noncanonical encode accepted", err)
			}
			if _, err := NewBuilder(DefaultParams(), a.Header); !errors.Is(err, ErrMalformed) {
				t.Fatal("noncanonical builder accepted", err)
			}
		})
	}
}

func TestFileDirectoryGoldenCause(t *testing.T) {
	for _, v := range fileGoldens(t) {
		if v.id == F_DR_TA_1 && fileGoldenASDU(t, v)[2] != byte(Request) {
			t.Fatal("positive directory golden must use COT5")
		}
	}
}

func TestFileDirectoryArbitraryCausePreservation(t *testing.T) {
	a := fileMessage(testFileKey, FileDirectory{Name: 9, Status: 32}, FileTransfer)
	b, err := a.MarshalBinary(DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	m, err := DecodeASDU(b, DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	round, err := m.MarshalBinary(DefaultParams())
	if err != nil || !bytes.Equal(round, b) || m.GetHeader().Cause != FileTransfer {
		t.Fatal("codec lost deliberately non-nominal COT13", err)
	}
}
