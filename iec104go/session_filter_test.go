// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"context"
	"testing"
	"time"
)

func TestCadenceFilterPrecedenceAndKnownUnhandled(t *testing.T) {
	for _, tc := range []struct {
		typ     TypeID
		ca      uint16
		payload []byte
		cot     COT
	}{
		{200, 2, []byte{0xb9, 0x0b, 0, 0x81}, UnknownCA},
		{C_RD_NA_1, 1, []byte{0, 0, 0}, UnknownType},
		{C_CS_NA_1, 1, []byte{0, 0, 0, 0, 0, 0, 12, 23, 10, 26}, UnknownType},
	} {
		opts := DefaultOptions()
		opts.APCI.W = 1
		opts.ReceiveFilter = ReceiveFilter{CommonAddresses: []uint16{1}, ApplicationTypes: []TypeID{C_DC_NA_1}}
		cause := Activation
		if tc.typ == C_RD_NA_1 {
			cause = Request
		}
		input := RawASDU{Header: Header{Type: tc.typ, Count: 1, Cause: cause, Test: true, Originator: 19, CommonAddress: tc.ca}, Payload: tc.payload}
		b, err := input.MarshalBinary(opts.Params)
		if err != nil {
			t.Fatal(err)
		}
		s, p, _ := manualSession(t, ServerRole, opts, func(_ context.Context, e Event) {
			if e.Kind == ASDUReceived {
				t.Error("rejection delivered")
			}
		})
		startServerSession(t, s, p)
		writeFrame(t, p, Frame{Kind: IFrame, ASDU: b})
		b[2] = 0x80 | 0x40 | byte(tc.cot)
		want, err := (Frame{Kind: IFrame, Receive: 1, ASDU: b}).MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		exactAPDU(t, p, want)
		// A TESTFR barrier catches an unwanted trailing S deterministically.
		writeFrame(t, p, Frame{Kind: UFrame, Function: TestFRAct})
		exactAPDU(t, p, []byte{104, 4, 131, 0, 0, 0})
	}
}

func TestCadenceFilterCopiesAndExemptions(t *testing.T) {
	o := DefaultOptions()
	o.ReceiveFilter = ReceiveFilter{CommonAddresses: []uint16{1}, ApplicationTypes: []TypeID{C_DC_NA_1}}
	normalized, err := o.normalized()
	if err != nil {
		t.Fatal(err)
	}
	o.ReceiveFilter.CommonAddresses[0] = 2
	o.ReceiveFilter.ApplicationTypes[0] = C_CS_NA_1
	f := normalized.ReceiveFilter
	if f.reject(Header{Type: C_DC_NA_1, CommonAddress: 1}) != 0 || f.reject(Header{Type: C_CS_NA_1, CommonAddress: 1}) != UnknownType {
		t.Fatal("filter aliases caller slices")
	}
	if f.reject(Header{Type: F_SC_NA_1, CommonAddress: 1}) != 0 || f.reject(Header{Type: F_SC_NA_1, CommonAddress: 2}) != UnknownCA {
		t.Fatal("file exemption changed CA policy")
	}
	if (ReceiveFilter{}).reject(Header{Type: 200, CommonAddress: 2}) != 0 {
		t.Fatal("empty filter must accept")
	}
	// Client-side application delivery and cadence ignore even a restrictive filter.
	o.APCI.W = 1
	delivered := make(chan struct{}, 1)
	s, p, _ := manualSession(t, ClientRole, o, func(_ context.Context, e Event) {
		if e.Kind == ASDUReceived {
			delivered <- struct{}{}
		}
	})
	startClientSession(t, s, p)
	writeFrame(t, p, messageFrame(t, 0, 0))
	exactAPDU(t, p, []byte{104, 4, 1, 0, 2, 0})
	<-delivered
}

func TestCadenceBlockedHandlerLiveness(t *testing.T) {
	for _, acknowledge := range []bool{false, true} {
		t.Run(map[bool]string{false: "t1-expires", true: "peer-progress"}[acknowledge], func(t *testing.T) {
			opts := DefaultOptions()
			opts.APCI.K = 1
			opts.APCI.W = 1
			entered := make(chan struct{})
			release := make(chan struct{})
			s, p, clock := manualSession(t, ServerRole, opts, func(ctx context.Context, e Event) {
				if e.Kind == ASDUReceived {
					select {
					case <-entered:
					default:
						close(entered)
					}
					select {
					case <-release:
					case <-ctx.Done():
					}
				}
			})
			startServerSession(t, s, p)
			if err := s.Send(sampleMessage()); err != nil {
				t.Fatal(err)
			}
			readFrame(t, p)
			v := waitView(t, s, func(v sessionView) bool { return !v.t1.IsZero() })
			clock.Advance(10 * time.Second)
			writeFrame(t, p, messageFrame(t, 0, 0))
			<-entered
			// No callback completion or clock advance is needed for this threshold S.
			exactAPDU(t, p, []byte{104, 4, 1, 0, 2, 0})
			if acknowledge {
				// The second I carries the previously deferred on-time N(R).
				writeFrame(t, p, messageFrame(t, 1, 1))
				exactAPDU(t, p, []byte{104, 4, 1, 0, 4, 0})
				writeFrame(t, p, Frame{Kind: SFrame, Receive: 1})
				waitView(t, s, func(v sessionView) bool { return v.acked == 1 && v.received == 2 })
			}
			writeFrame(t, p, Frame{Kind: UFrame, Function: TestFRAct})
			exactAPDU(t, p, []byte{104, 4, 131, 0, 0, 0})
			if acknowledge {
				writeFrame(t, p, Frame{Kind: UFrame, Function: StopDTAct})
				exactAPDU(t, p, []byte{104, 4, 35, 0, 0, 0})
				waitView(t, s, func(v sessionView) bool { return !v.started })
				clock.Advance(v.t1.Sub(clock.Now()))
				if v := view(t, s); !v.t1.IsZero() {
					t.Fatal("on-time N(R) did not cancel t1")
				}
			} else {
				clock.Advance(v.t1.Sub(clock.Now()))
				select {
				case <-s.transportDone:
				case <-time.After(3 * time.Second):
					t.Fatal("blocked callback held t1")
				}
				if s.Err() != ErrT1 {
					t.Fatal(s.Err())
				}
			}
			close(release)
		})
	}
}

func TestCadenceSpontaneousSendDuringHandler(t *testing.T) {
	for _, during := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-dispatch", true: "inside-handler"}[during], func(t *testing.T) {
			opts := DefaultOptions()
			opts.APCI.W = 1
			entered, release, dispatch := make(chan struct{}), make(chan struct{}), make(chan struct{})
			if during {
				close(dispatch)
			}
			s, p, _ := manualSession(t, ServerRole, opts, func(ctx context.Context, e Event) {
				if e.Kind == Opened {
					select {
					case <-dispatch:
					case <-ctx.Done():
						return
					}
				}
				if e.Kind == ASDUReceived {
					close(entered)
					select {
					case <-release:
					case <-ctx.Done():
						return
					}
					m, err := Mirror(e.ASDU, UnknownType, true)
					if err == nil {
						err = e.Session.Send(m)
					}
					if err != nil {
						t.Error(err)
					}
				}
			})
			startServerSession(t, s, p)
			writeFrame(t, p, messageFrame(t, 0, 0))
			exactAPDU(t, p, []byte{104, 4, 1, 0, 2, 0})
			if during {
				<-entered
			}
			// This worker's Send precedes the handler's Send in both runs. Only
			// dispatch interleaving differs, so the entire output must be identical.
			if err := s.Send(sampleMessage()); err != nil {
				t.Fatal(err)
			}
			want := messageFrame(t, 0, 1)
			b, err := want.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			exactAPDU(t, p, b)
			if !during {
				close(dispatch)
				<-entered
			}
			close(release)
			response, err := Mirror(sampleMessage(), UnknownType, true)
			if err != nil {
				t.Fatal(err)
			}
			payload, err := response.MarshalBinary(opts.Params)
			if err != nil {
				t.Fatal(err)
			}
			b, err = (Frame{Kind: IFrame, Send: 1, Receive: 1, ASDU: payload}).MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			exactAPDU(t, p, b)
			writeFrame(t, p, Frame{Kind: UFrame, Function: TestFRAct})
			exactAPDU(t, p, []byte{104, 4, 131, 0, 0, 0})
		})
	}
}

func TestCadenceEarlierResponseNotHeldByNextHandler(t *testing.T) {
	opts := DefaultOptions()
	opts.APCI.W = 2
	entered, release := make(chan struct{}), make(chan struct{})
	s, p, _ := manualSession(t, ServerRole, opts, func(ctx context.Context, e Event) {
		if e.Kind != ASDUReceived {
			return
		}
		select {
		case <-entered:
			<-ctx.Done()
			return
		default:
			close(entered)
		}
		select {
		case <-release:
		case <-ctx.Done():
			return
		}
		if err := e.Session.Send(sampleMessage()); err != nil {
			t.Error(err)
		}
	})
	startServerSession(t, s, p)
	writeFrame(t, p, messageFrame(t, 0, 0))
	<-entered
	writeFrame(t, p, messageFrame(t, 1, 0))
	exactAPDU(t, p, []byte{104, 4, 1, 0, 4, 0})
	close(release)
	if f := readFrame(t, p); f.Kind != IFrame || f.Receive != 2 {
		t.Fatal(f)
	}
}
