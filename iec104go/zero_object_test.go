// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestZeroObjectEnvelope(t *testing.T) {
	for id := TypeID(0); ; id++ {
		for _, sq := range []bool{false, true} {
			for _, p := range []Params{DefaultParams(), {1, 1, 1}} {
				t.Run(fmt.Sprintf("%d/%t/%v", id, sq, p), func(t *testing.T) {
					a := ASDU{Header: Header{Type: id, Sequence: sq, Cause: Activation, CommonAddress: 1, Test: true}}
					wire, err := a.MarshalBinary(p)
					if err != nil {
						t.Fatal(err)
					}
					m, err := DecodeASDU(wire, p)
					if err != nil || m.GetHeader() != a.GetHeader() {
						t.Fatalf("%+v %v", m, err)
					}
					if id.Supported() {
						if decoded, ok := m.(*ASDU); !ok || len(decoded.Objects) != 0 {
							t.Fatalf("not an empty typed ASDU: %#v", m)
						}
					}
					mirror, err := Mirror(m, UnknownIOA, true)
					if err != nil {
						t.Fatal(err)
					}
					got, err := mirror.MarshalBinary(p)
					want := append([]byte(nil), wire...)
					want[2] = 0x80 | 0x40 | byte(UnknownIOA)
					if err != nil || !bytes.Equal(got, want) {
						t.Fatalf("mirror=%x err=%v want=%x", got, err, want)
					}
					if _, err = DecodeASDU(append(wire, 0), p); !errors.Is(err, ErrMalformed) {
						t.Fatalf("trailing payload: %v", err)
					}
				})
			}
		}
		if id == 255 {
			break
		}
	}
}

func TestZeroObjectSessionDispatch(t *testing.T) {
	for _, tc := range []struct {
		role Role
		typ  TypeID
	}{
		{ServerRole, C_DC_NA_1}, {ServerRole, C_IC_NA_1}, {ClientRole, M_DP_NA_1},
	} {
		t.Run(fmt.Sprintf("%d/%d", tc.role, tc.typ), func(t *testing.T) {
			received := make(chan Message, 1)
			opts := DefaultOptions()
			opts.APCI.W = 1
			s, peer, _ := manualSession(t, tc.role, opts, func(_ context.Context, e Event) {
				if e.Kind == ASDUReceived {
					received <- e.ASDU
				}
			})
			if tc.role == ServerRole {
				startServerSession(t, s, peer)
			} else {
				startClientSession(t, s, peer)
			}
			wire := []byte{byte(tc.typ), 0, byte(Activation), 0, 1, 0}
			writeFrame(t, peer, Frame{Kind: IFrame, ASDU: wire})
			exactAPDU(t, peer, []byte{0x68, 4, 1, 0, 2, 0})
			select {
			case m := <-received:
				if a, ok := m.(*ASDU); !ok || len(a.Objects) != 0 {
					t.Fatalf("%#v", m)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("empty ASDU was not dispatched")
			}
			writeFrame(t, peer, Frame{Kind: UFrame, Function: TestFRAct})
			exactAPDU(t, peer, []byte{0x68, 4, 0x83, 0, 0, 0})
		})
	}
}

// Optional file policy must not assume the codec supplies a first object.
func TestZeroObjectFilePolicy(t *testing.T) {
	w := &fileServing{state: 2, key: FileKey{Name: 17}}
	for typ := F_FR_NA_1; typ <= F_SC_NB_1; typ++ {
		if err := w.handle(context.Background(), &ASDU{Header: Header{Type: typ}}); err != nil {
			t.Fatal(err)
		}
		if w.state != 2 || w.key.Name != 17 {
			t.Fatal("empty file envelope changed transfer state")
		}
	}
}
