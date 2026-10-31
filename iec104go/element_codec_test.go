// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"math"
	"reflect"
	"testing"
)

type elementVector struct {
	id    TypeID
	value Element
	hex   string
}

// Literals assert independent byte positions, not just agreement between an
// encoder and decoder that could share the same layout mistake.
func elementVectors(t *testing.T) []elementVector {
	t.Helper()
	tag, err := DecodeCP56Time2a(unhex(t, "3412c5e6a7b8d9"))
	if err != nil {
		t.Fatal(err)
	}
	base := []elementVector{
		{M_SP_NA_1, SinglePoint{SIQ: 0xe9}, "e9"},
		{M_DP_NA_1, DoublePoint{DIQ: 0xd6}, "d6"},
		{M_ST_NA_1, StepPosition{Value: 0xc1, Quality: 0xb1}, "c1b1"},
		{M_BO_NA_1, Bitstring32{Value: 0x78563412, Quality: 0x91}, "1234567891"},
		{M_ME_NA_1, Normalized{Value: -12345, Quality: 0x71}, "c7cf71"},
		{M_ME_NB_1, Scaled{Value: -23456, Quality: 0x61}, "60a461"},
		{M_ME_NC_1, Float{Value: math.Float32frombits(0x41234567), Quality: 0x51}, "6745234151"},
		{M_IT_NA_1, Counter{Value: -123456789, Flags: 0xe7}, "eb32a4f8e7"},
		{M_PS_NA_1, PackedStatus{Status: 0x1234, Change: 0x5678, Quality: 0x41}, "3412785641"},
		{M_ME_ND_1, NormalizedNoQuality{Value: -32768}, "0080"},
		{C_SC_NA_1, SingleCommand{Qualifier: 0xe1}, "e1"},
		{C_DC_NA_1, DoubleCommand{Qualifier: 0xd2}, "d2"},
		{C_RC_NA_1, RegulatingCommand{Qualifier: 0xc1}, "c1"},
		{C_SE_NA_1, SetpointNormalized{Value: 12345, Qualifier: 0xb2}, "3930b2"},
		{C_SE_NB_1, SetpointScaled{Value: -12345, Qualifier: 0xa3}, "c7cfa3"},
		{C_SE_NC_1, SetpointFloat{Value: math.Float32frombits(0x42345678), Qualifier: 0x94}, "7856344294"},
		{C_BO_NA_1, BitstringCommand{Value: 0x87654321}, "21436587"},
		{M_EI_NA_1, Initialization{COI: 0x83}, "83"},
		{C_IC_NA_1, Interrogation{QOI: 20}, "14"},
		{C_CI_NA_1, CounterInterrogation{QCC: 0x85}, "85"},
		{C_RD_NA_1, ReadCommand{}, ""},
		{C_CS_NA_1, ClockSync{Time: tag}, "3412c5e6a7b8d9"},
		{C_RP_NA_1, ResetProcess{QRP: 2}, "02"},
		{C_TS_TA_1, TestCommand{Sequence: 0x9abc, Time: tag}, "bc9a3412c5e6a7b8d9"},
		{P_ME_NA_1, ParameterNormalized{Value: 32767, QPM: 0x73}, "ff7f73"},
		{P_ME_NB_1, ParameterScaled{Value: -32768, QPM: 0x62}, "008062"},
		{P_ME_NC_1, ParameterFloat{Value: math.Float32frombits(0x43456789), QPM: 0x51}, "8967454351"},
		{P_AC_NA_1, ParameterActivation{QPA: 3}, "03"},
	}
	result := append([]elementVector(nil), base...)
	for _, v := range base {
		if id := timedType(v.value); id != 0 {
			result = append(result, elementVector{id, Timed{Value: v.value, Time: tag}, v.hex + "3412c5e6a7b8d9"})
		}
	}
	result = append(result,
		elementVector{M_EP_TD_1, Timed{Value: ProtectionEvent{SEP: 0xb2, Elapsed: 0x7654}, Time: tag}, "b254763412c5e6a7b8d9"},
		elementVector{M_EP_TE_1, Timed{Value: ProtectionStart{SPE: 0x31, QDP: 0xc8, Elapsed: 0x8765}, Time: tag}, "31c865873412c5e6a7b8d9"},
		elementVector{M_EP_TF_1, Timed{Value: ProtectionOutput{OCI: 0x07, QDP: 0xe8, Elapsed: 0x9876}, Time: tag}, "07e876983412c5e6a7b8d9"},
	)
	return result
}

func unhex(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestEveryType(t *testing.T) {
	seen := map[TypeID]bool{}
	for _, v := range elementVectors(t) {
		seen[v.id] = true
		for _, sq := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/SQ=%v", v.id, sq), func(t *testing.T) {
				h := Header{Type: v.id, Sequence: sq, Cause: UnknownIOA, Negative: true, Test: true, Originator: 0xa5, CommonAddress: 0xffff}
				address := uint32(0xffffff)
				addrWire := "ffffff"
				vsq := "02"
				if sq {
					address = 1
					addrWire = ""
					vsq = "82"
				}
				a := ASDU{Header: h, Objects: []Object{{Address: 0, Element: v.value}, {Address: address, Element: v.value}}}
				want := unhex(t, fmt.Sprintf("%02x%sefa5ffff000000%s%s%s", uint8(v.id), vsq, v.hex, addrWire, v.hex))
				wire, err := a.MarshalBinary(DefaultParams())
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(wire, want) {
					t.Fatalf("wire=%x want=%x", wire, want)
				}
				decoded, err := DecodeASDU(want, DefaultParams())
				if err != nil {
					t.Fatal(err)
				}
				a.Header.Count = 2
				if !reflect.DeepEqual(decoded, &a) {
					t.Fatalf("decoded=%#v want=%#v", decoded, a)
				}
				round, err := decoded.MarshalBinary(DefaultParams())
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(round, want) {
					t.Fatalf("roundtrip=%x want=%x", round, want)
				}
			})
		}
	}
	// File-specific SQ/count/shared-IOA layouts are checked by TestFileGoldenVectors.
	for _, v := range fileGoldens(t) {
		seen[v.id] = true
	}
	if len(seen) != 54 {
		t.Fatalf("coverage: got %d types", len(seen))
	}
	for id := range elementSizes {
		if !seen[id] {
			t.Errorf("missing type %s", id)
		}
	}
}
