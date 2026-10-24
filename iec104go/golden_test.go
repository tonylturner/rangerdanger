// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"testing"
	"time"
)

type golden struct {
	Capture       string   `json:"capture"`
	Frame         int      `json:"frame"`
	SHA256        string   `json:"sha256"`
	Hex           string   `json:"hex"`
	Type          TypeID   `json:"type"`
	Cause         COT      `json:"cause"`
	Originator    uint8    `json:"originator"`
	CommonAddress uint16   `json:"common_address"`
	Addresses     []uint32 `json:"addresses"`
}

func loadGoldens(t testing.TB) []golden {
	t.Helper()
	b, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []golden
	if err := json.Unmarshal(b, &vectors); err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 14 {
		t.Fatalf("got %d goldens", len(vectors))
	}
	return vectors
}

func TestGoldens(t *testing.T) {
	for _, g := range loadGoldens(t) {
		t.Run(fmt.Sprintf("%s/%d", g.Capture, g.Frame), func(t *testing.T) {
			if len(g.SHA256) != 64 {
				t.Fatal("missing pcap provenance")
			}
			wire := unhex(t, g.Hex)
			f, err := NewAPDUReader(bytes.NewReader(wire)).ReadFrame()
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := f.MarshalBinary()
			if err != nil || !bytes.Equal(wire, encoded) {
				t.Fatalf("APDU %x %v", encoded, err)
			}
			if f.Kind != IFrame {
				return
			}
			m, err := DecodeASDU(f.ASDU, DefaultParams())
			if err != nil {
				t.Fatal(err)
			}
			a, ok := m.(*ASDU)
			if !ok {
				t.Fatalf("raw golden: %#v", m)
			}
			h := a.GetHeader()
			if h.Type != g.Type || h.Cause != g.Cause || h.Originator != g.Originator || h.CommonAddress != g.CommonAddress {
				t.Fatalf("header=%+v golden=%+v", h, g)
			}
			var addresses []uint32
			for _, o := range a.Objects {
				addresses = append(addresses, o.Address)
			}
			if !reflect.DeepEqual(addresses, g.Addresses) {
				t.Fatalf("IOAs=%v want=%v", addresses, g.Addresses)
			}
			payload, err := m.MarshalBinary(DefaultParams())
			if err != nil || !bytes.Equal(payload, f.ASDU) {
				t.Fatalf("ASDU %x %v", payload, err)
			}
			if g.Capture == "edge-addressing.pcap" && g.Frame == 11 {
				if len(a.Objects) != 5 || a.Objects[4].Address != 0xffffff {
					t.Fatal("missing edge floats")
				}
				for i, bits := range []uint32{0x43368000, 0x41a33333, 0x428311ec, 0x4283126f, 0x4327c5ac} {
					if got := math.Float32bits(a.Objects[i].Element.(Float).Value); got != bits {
						t.Fatalf("float %d: %x want %x", i, got, bits)
					}
				}
			}
			if g.Capture == "edge-addressing.pcap" && g.Frame == 44 && (!h.Negative || h.Cause != UnknownIOA || a.Objects[0].Element.(DoubleCommand).Qualifier != 2) {
				t.Fatal("COT47 mirror")
			}
			if g.Capture == "sbo-double-command.pcap" && g.Frame >= 8 && g.Frame <= 11 {
				q := a.Objects[0].Element.(DoubleCommand).Qualifier
				if q.Select() != (g.Frame <= 9) || q.State() != 1 || q.QU() != 0 {
					t.Fatalf("SBO: %x", q)
				}
			}
			if g.Capture == "startup-gi-monitor.pcap" {
				tag := a.Objects[0].Element.(Timed).Time
				got, ok := tag.Time()
				want := time.Date(2026, 10, 9, 14, 55, 55, 687000000, time.UTC)
				if !ok || !got.Equal(want) || tag.Invalid || tag.SummerTime || tag.DayOfWeek != 0 {
					t.Fatalf("CP56=%+v %v", tag, got)
				}
			}
			// Successful decode must not retain even its caller's frame backing slice.
			for i := range f.ASDU {
				f.ASDU[i] = 0
			}
			retained, err := m.MarshalBinary(DefaultParams())
			if err != nil || !bytes.Equal(retained, payload) {
				t.Fatal("decoded elements alias input")
			}
		})
	}
}

func FuzzAPDUReader(f *testing.F) {
	for _, g := range loadGoldens(f) {
		f.Add(unhex(f, g.Hex))
	}
	for _, v := range fileGoldens(f) {
		b, err := (Frame{Kind: IFrame, ASDU: fileGoldenASDU(f, v)}).MarshalBinary()
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, b []byte) {
		reader := NewAPDUReader(bytes.NewReader(b))
		offset := 0
		for {
			frame, err := reader.ReadFrame()
			if err != nil {
				return
			}
			wire, err := frame.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			if offset+len(wire) > len(b) || !bytes.Equal(b[offset:offset+len(wire)], wire) {
				t.Fatal("frame changed bytes")
			}
			offset += len(wire)
			if offset == len(b) {
				if _, err := reader.ReadFrame(); err != io.EOF {
					t.Fatal(err)
				}
				return
			}
		}
	})
}

func FuzzDecodeASDU(f *testing.F) {
	for _, g := range loadGoldens(f) {
		b := unhex(f, g.Hex)
		if frame, err := DecodeFrame(b); err == nil && frame.Kind == IFrame {
			f.Add(frame.ASDU, uint8(1), uint8(1), uint8(2))
		}
	}
	for _, v := range fileGoldens(f) {
		f.Add(fileGoldenASDU(f, v), uint8(1), uint8(1), uint8(2))
	}
	// Include every supported type and SQ mode as structured seeds; goldens alone
	// cover only four IDs and would leave most element decoders unexplored.
	for _, v := range elementVectorsForFuzz(f) {
		f.Add(v, uint8(1), uint8(1), uint8(2))
	}
	f.Add([]byte{255, 1, 3, 0, 1, 0, 0xaa}, uint8(1), uint8(1), uint8(2))
	f.Fuzz(func(t *testing.T, b []byte, cot, ca, ioa uint8) {
		p := Params{COTSize: int(cot%2) + 1, CASize: int(ca%2) + 1, IOASize: int(ioa%3) + 1}
		m, err := DecodeASDU(b, p)
		if err != nil {
			return
		}
		encoded, err := m.MarshalBinary(p)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(b, encoded) {
			t.Fatalf("ASDU changed bytes: %x -> %x", b, encoded)
		}
	})
}

func elementVectorsForFuzz(t testing.TB) [][]byte {
	t.Helper()
	var result [][]byte
	for id, size := range elementSizes {
		for _, sq := range []byte{2, 0x82} {
			b := []byte{byte(id), sq, 3, 0, 1, 0}
			for i := 0; i < 2; i++ {
				if i == 0 || sq == 2 {
					b = append(b, byte(i), 0, 0)
				}
				for j := 0; j < size; j++ {
					b = append(b, byte(i*79+j*37+11))
				}
			}
			result = append(result, b)
		}
	}
	return result
}
