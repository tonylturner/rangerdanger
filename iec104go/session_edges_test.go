// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestSessionRawAndMalformed(t *testing.T) {
	events := make(chan Event, 10)
	s, p, clock := manualSession(t, ServerRole, DefaultOptions(), func(_ context.Context, e Event) { events <- e })
	startServerSession(t, s, p)
	raw := RawASDU{Header: Header{Type: 255, Count: 1, Cause: COT(63), Originator: 17, CommonAddress: 42}, Payload: []byte{0xde, 0xad}}
	payload, err := raw.MarshalBinary(DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	writeFrame(t, p, Frame{Kind: IFrame, ASDU: payload})
	for {
		e := <-events
		if e.Kind == ASDUReceived {
			if _, ok := e.ASDU.(*RawASDU); !ok {
				t.Fatal("unknown type not delivered raw")
			}
			break
		}
	}
	v := view(t, s)
	if v.sent != 0 {
		t.Fatal("library auto-answered unknown COT/type")
	}
	clock.Advance(10 * time.Second)
	if f := readFrame(t, p); f.Kind != SFrame {
		t.Fatalf("unexpected automatic response %+v", f)
	}
	// Header count zero is structural, unlike an unsupported type/cause/CA/IOA.
	writeFrame(t, p, Frame{Kind: IFrame, Send: 1, ASDU: []byte{1, 0, 3, 0, 1, 0}})
	waitClosed(t, s)
	if !errors.Is(s.Err(), ErrMalformed) {
		t.Fatal(s.Err())
	}
}
func TestSessionStopFlushesAdmittedQueue(t *testing.T) {
	opts := DefaultOptions()
	opts.APCI.K = 1
	s, p, _ := manualSession(t, ClientRole, opts, nil)
	startClientSession(t, s, p)
	if err := s.Send(sampleMessage()); err != nil {
		t.Fatal(err)
	}
	readFrame(t, p)
	if err := s.Send(sampleMessage()); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	ctx := testContext(t)
	go func() { done <- s.StopDT(ctx) }()
	eventually(t, func() bool { return !s.IsStarted() })
	writeFrame(t, p, Frame{Kind: SFrame, Receive: 1})
	if f := readFrame(t, p); f.Kind != IFrame || f.Send != 1 {
		t.Fatalf("admitted object lost: %+v", f)
	}
	if f := readFrame(t, p); f.Function != StopDTAct {
		t.Fatalf("STOP overtook data: %+v", f)
	}
	writeFrame(t, p, Frame{Kind: SFrame, Receive: 2})
	writeFrame(t, p, Frame{Kind: UFrame, Function: StopDTCon})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func TestSessionShortT3DoesNotExtendT1(t *testing.T) {
	opts := DefaultOptions()
	opts.APCI.T3 = 2 * time.Second
	s, p, clock := manualSession(t, ServerRole, opts, nil)
	view(t, s)
	clock.Advance(2 * time.Second)
	if f := readFrame(t, p); f.Function != TestFRAct {
		t.Fatal("missing TESTFR")
	}
	view(t, s)
	// Equal/short timers are legal. Expiring t3 repeatedly must not retransmit or
	// restart the original confirmation deadline.
	for i := 0; i < 7; i++ {
		clock.Advance(2 * time.Second)
		view(t, s)
	}
	clock.Advance(time.Second)
	waitClosed(t, s)
	if !errors.Is(s.Err(), ErrT1) {
		t.Fatal(s.Err())
	}
}
func TestSessionInvalidRoleAndEarlyCon(t *testing.T) {
	s, p, _ := manualSession(t, ClientRole, DefaultOptions(), nil)
	startClientSession(t, s, p)
	if err := s.Send(sampleMessage()); err != nil {
		t.Fatal(err)
	}
	readFrame(t, p)
	ctx := testContext(t)
	done := make(chan error, 1)
	go func() { done <- s.StopDT(ctx) }()
	readFrame(t, p)
	writeFrame(t, p, Frame{Kind: UFrame, Function: StopDTCon})
	waitClosed(t, s)
	if !errors.Is(s.Err(), ErrProtocol) {
		t.Fatal(s.Err())
	}
	<-done
	server, _, _ := manualSession(t, ServerRole, DefaultOptions(), nil)
	if err := server.StartDT(ctx); !errors.Is(err, ErrProtocol) {
		t.Fatal(err)
	}
	if err := server.StopDT(ctx); !errors.Is(err, ErrProtocol) {
		t.Fatal(err)
	}
}

// advancingClock advances between the caller's Now and timer registration.
// Duration-relative registration used to silently move every deadline here.
type advancingClock struct {
	*fakeClock
	advance time.Duration
}

func (c advancingClock) NewTimer(at time.Time) Timer {
	c.Advance(c.advance)
	return c.fakeClock.NewTimer(at)
}
func TestSessionAbsoluteTimerRegistration(t *testing.T) {
	opts := DefaultOptions()
	clock := advancingClock{newFakeClock(), opts.APCI.T3}
	opts.Clock = clock
	a, p := net.Pipe()
	s, err := NewSession(context.Background(), a, ServerRole, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close(); closeSession(t, s) })
	if f := readFrame(t, p); f.Function != TestFRAct {
		t.Fatalf("deadline moved during registration: %+v", f)
	}
	view(t, s)
	clock.Advance(opts.APCI.T1)
	waitClosed(t, s)
	if !errors.Is(s.Err(), ErrT1) {
		t.Fatal(s.Err())
	}
}

func TestSessionLargeWindowSlowWriter(t *testing.T) {
	opts := DefaultOptions()
	opts.APCI.K = 32767
	opts.SendQueue = 1
	s, p, _ := manualSession(t, ClientRole, opts, nil)
	startClientSession(t, s, p)
	admitted := 0
	for i := 0; i < 100; i++ {
		err := s.Send(sampleMessage())
		if errors.Is(err, ErrQueueFull) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		admitted++
		view(t, s)
	}
	if admitted == 0 || admitted == 100 {
		t.Fatalf("unexpected admission count %d", admitted)
	}
	for i := 0; i < admitted; i++ {
		f := readFrame(t, p)
		if f.Kind != IFrame || f.Send != uint16(i) {
			t.Fatalf("admitted frame lost: %+v", f)
		}
		writeFrame(t, p, Frame{Kind: SFrame, Receive: uint16(i + 1)})
	}
	if err := s.Send(sampleMessage()); err != nil {
		t.Fatal(err)
	}
	readFrame(t, p)
}

func TestSessionHandlerOwnsPublishedSession(t *testing.T) {
	a, p := net.Pipe()
	defer p.Close()
	events := make(chan Event, 2)
	s, err := NewSession(context.Background(), a, ClientRole, Options{}, func(_ context.Context, e Event) {
		if e.Session == nil || e.Session.ID() != e.ConnID {
			t.Error("event has no initialized session")
			return
		}
		if e.Kind == Opened {
			e.Session.RequestClose()
		}
		if e.Kind == Closed {
			if err := e.Session.Send(sampleMessage()); !errors.Is(err, ErrConnectionGone) {
				t.Errorf("send from Closed: %v", err)
			}
		}
		events <- e
	})
	if err != nil {
		t.Fatal(err)
	}
	waitClosed(t, s)
	if (<-events).Kind != Opened || (<-events).Kind != Closed {
		t.Fatal("handler-safe immediate closure lost event order")
	}
}
