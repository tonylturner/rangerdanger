// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"testing"
	"time"
)

func exactAPDU(t *testing.T, peer net.Conn, want []byte) {
	t.Helper()
	f := readFrame(t, peer)
	got, err := f.MarshalBinary()
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("APDU=%x, err=%v; want %x", got, err, want)
	}
}

func TestCadenceClientEveryReceive(t *testing.T) {
	opts := DefaultOptions()
	opts.APCI.W = 1
	opts.SendQueue = 1
	claimed := make(chan struct{}, 1)
	opts.Tap = func(d Direction, _ uint64, _ time.Time, b []byte) {
		if d == Transmit && b[2]&1 == 0 {
			claimed <- struct{}{}
		}
	}
	s, peer, _ := manualSession(t, ClientRole, opts, nil)
	startClientSession(t, s, peer)
	if err := s.Send(sampleMessage()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-claimed:
	case <-time.After(3 * time.Second):
		t.Fatal("writer did not claim I-frame")
	}
	var burst []byte
	for n := uint16(0); n < 32; n++ {
		b, err := messageFrame(t, n, 0).MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		burst = append(burst, b...)
	}
	done := make(chan error, 1)
	go func() { _, err := peer.Write(burst); done <- err }()
	waitView(t, s, func(v sessionView) bool { return v.received >= 1 })
	if v := view(t, s); v.received != 1 {
		t.Fatalf("accepted more than w without a successful wire ACK: %+v", v)
	}
	b, err := messageFrame(t, 0, 0).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	exactAPDU(t, peer, b)
	// cc-rtu C baseline frames18/19/21/22: never collapse or advance these ACKs.
	for n := byte(1); n <= 32; n++ {
		exactAPDU(t, peer, []byte{0x68, 4, 1, 0, n * 2, 0})
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestCadenceServerResponseOrder(t *testing.T) {
	for _, cause := range []COT{ActivationCon, UnknownType, UnknownCause, UnknownCA, UnknownIOA} {
		t.Run(fmt.Sprint(cause), func(t *testing.T) {
			opts := DefaultOptions()
			opts.APCI.W = 1
			input := sampleMessage()
			response, err := Mirror(input, cause, cause != ActivationCon)
			if err != nil {
				t.Fatal(err)
			}
			s, peer, _ := manualSession(t, ServerRole, opts, func(_ context.Context, e Event) {
				if e.Kind == ASDUReceived {
					if err := e.Session.Send(response); err != nil {
						t.Error(err)
					}
				}
			})
			startServerSession(t, s, peer)
			writeFrame(t, peer, messageFrame(t, 0, 0))
			// negative-addressing C baseline: 44 (frame18) and46 (frame30)
			// piggyback; 45 (frames23/24),47 (frames35/36) have an S first.
			if cause != UnknownType && cause != UnknownCA {
				exactAPDU(t, peer, []byte{0x68, 4, 1, 0, 2, 0})
			}
			payload, err := response.MarshalBinary(opts.Params)
			if err != nil {
				t.Fatal(err)
			}
			want, err := (Frame{Kind: IFrame, Receive: 1, ASDU: payload}).MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			exactAPDU(t, peer, want)
		})
	}
}

func TestCadenceQueuedSnapshots(t *testing.T) {
	s := &Session{out: make(chan *wireWrite, 8)}
	for n := uint16(1); n <= 4; n++ {
		s.received.Store(uint64(n))
		if err := s.enqueueWire(Frame{Kind: SFrame, Receive: n}, false, nil); err != nil {
			t.Fatal(err)
		}
	}
	if len(s.out) != 4 {
		t.Fatalf("collapsed S frames: %d", len(s.out))
	}
	for n := byte(1); n <= 4; n++ {
		w := s.claimWire(<-s.out)
		if !bytes.Equal(w.bytes, []byte{0x68, 4, 1, 0, n * 2, 0}) || w.receiveCount != uint64(n) {
			t.Fatalf("snapshot=%x count=%d", w.bytes, w.receiveCount)
		}
	}
}

func TestCadenceBlockedCallbackClock(t *testing.T) {
	opts := DefaultOptions()
	opts.APCI.W = 1
	entered := make(chan struct{})
	s, peer, clock := manualSession(t, ServerRole, opts, func(ctx context.Context, e Event) {
		if e.Kind == ASDUReceived {
			close(entered)
			<-ctx.Done()
		}
	})
	startServerSession(t, s, peer)
	writeFrame(t, peer, messageFrame(t, 0, 0))
	<-entered
	v := waitView(t, s, func(v sessionView) bool { return v.pending == 1 })
	writeFrame(t, peer, Frame{Kind: UFrame, Function: TestFRAct})
	exactAPDU(t, peer, []byte{0x68, 4, 0x83, 0, 0, 0})
	clock.Advance(v.t2.Sub(clock.Now()))
	exactAPDU(t, peer, []byte{0x68, 4, 1, 0, 2, 0})
}

func TestCadenceFullKCannotDeferACK(t *testing.T) {
	opts := DefaultOptions()
	opts.APCI.W, opts.APCI.K = 1, 1
	s, peer, _ := manualSession(t, ServerRole, opts, func(_ context.Context, e Event) {
		if e.Kind == ASDUReceived {
			response, err := Mirror(e.ASDU, UnknownType, true)
			if err == nil {
				err = e.Session.Send(response)
			}
			if err != nil {
				t.Error(err)
			}
		}
	})
	startServerSession(t, s, peer)
	if err := s.Send(sampleMessage()); err != nil {
		t.Fatal(err)
	}
	readFrame(t, peer)
	writeFrame(t, peer, messageFrame(t, 0, 0))
	exactAPDU(t, peer, []byte{0x68, 4, 1, 0, 2, 0})
	writeFrame(t, peer, Frame{Kind: SFrame, Receive: 1})
	if f := readFrame(t, peer); f.Kind != IFrame || f.Send != 1 || f.Receive != 1 {
		t.Fatal(f)
	}
}

func TestCadenceExpiredT2WinsMirror(t *testing.T) {
	clock := newFakeClock()
	opts := DefaultOptions()
	opts.Clock, opts.APCI.W = clock, 1
	conn, peer := net.Pipe()
	s, err := NewSession(context.Background(), conn, ServerRole, opts, func(_ context.Context, e Event) {
		if e.Kind == ASDUReceived {
			// Both timer and callback completion can now be ready before the
			// actor is scheduled. The expired clock, not select, decides order.
			clock.Advance(opts.APCI.T2)
			response, err := Mirror(e.ASDU, UnknownType, true)
			if err == nil {
				err = e.Session.Send(response)
			}
			if err != nil {
				t.Error(err)
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close(); closeSession(t, s) })
	startServerSession(t, s, peer)
	writeFrame(t, peer, messageFrame(t, 0, 0))
	exactAPDU(t, peer, []byte{0x68, 4, 1, 0, 2, 0})
	if f := readFrame(t, peer); f.Kind != IFrame || f.Receive != 1 {
		t.Fatal(f)
	}
}

func TestCadenceCallbackWithoutReply(t *testing.T) {
	opts := DefaultOptions()
	opts.APCI.W = 1
	s, peer, _ := manualSession(t, ServerRole, opts, func(context.Context, Event) {})
	startServerSession(t, s, peer)
	writeFrame(t, peer, messageFrame(t, 0, 0))
	exactAPDU(t, peer, []byte{0x68, 4, 1, 0, 2, 0})
}

func TestCadenceQueueOccupancyDoesNotChooseReply(t *testing.T) {
	opts := DefaultOptions()
	opts.Clock, opts.SendQueue = newFakeClock(), 1
	input := sampleMessage()
	response, err := Mirror(input, UnknownType, true)
	if err != nil {
		t.Fatal(err)
	}
	request, err := input.MarshalBinary(opts.Params)
	if err != nil {
		t.Fatal(err)
	}
	reply, err := response.MarshalBinary(opts.Params)
	if err != nil {
		t.Fatal(err)
	}
	ticket := &receiveACK{request: request, first: reply}
	s := &Session{opts: opts, out: make(chan *wireWrite, 9), sendReady: make(chan struct{})}
	s.received.Store(1)
	s.sends = []queuedASDU{{bytes: reply, ack: ticket}}
	st := sessionState{s: s, started: true, received: 1, pending: 1, ackWait: ticket, t2: opts.Clock.Now().Add(opts.APCI.T2)}
	if err := s.enqueueWire(Frame{Kind: UFrame, Function: TestFRCon}, false, nil); err != nil {
		t.Fatal(err)
	}
	if ready, piggyback := st.callbackACKDecision(); !ready || !piggyback {
		t.Fatalf("writer queue changed the decision: ready=%v piggyback=%v", ready, piggyback)
	}
	if err := st.pump(); err != nil {
		t.Fatal(err)
	}
	if len(s.out) != 1 || st.ackWait != ticket || st.t2.IsZero() {
		t.Fatal("full writer queue must retain the decision and its t2 bound")
	}
	s.claimWire(<-s.out)
	if err := st.pump(); err != nil {
		t.Fatal(err)
	}
	w := s.claimWire(<-s.out)
	if w.bytes[2]&1 != 0 || w.bytes[4] != 2 || st.ackWait != nil || st.pending != 0 {
		t.Fatalf("deferred piggyback=%x", w.bytes)
	}
}
