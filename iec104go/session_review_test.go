// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"errors"
	"testing"
	"time"
)

func TestSessionControlRepliesCoalesce(t *testing.T) {
	opts := DefaultOptions()
	opts.SendQueue = 1
	opts.APCI.W = 1
	s, p, _ := manualSession(t, ServerRole, opts, nil)
	startServerSession(t, s, p)
	if err := s.Send(sampleMessage()); err != nil {
		t.Fatal(err)
	}
	waitView(t, s, func(v sessionView) bool { return v.sent == 1 })
	// The writer is blocked on our I-frame while inbound I/TESTFR keep arriving.
	for i := 0; i < 32; i++ {
		writeFrame(t, p, messageFrame(t, uint16(i), 0))
		writeFrame(t, p, Frame{Kind: UFrame, Function: TestFRAct})
		waitView(t, s, func(v sessionView) bool { return v.received == uint16(i+1) })
	}
	if f := readFrame(t, p); f.Kind != IFrame {
		t.Fatalf("blocked write=%+v", f)
	}
	if f := readFrame(t, p); f.Kind != SFrame || f.Receive != 32 {
		t.Fatalf("latest cumulative ACK not coalesced: %+v", f)
	}
	if f := readFrame(t, p); f.Kind != UFrame || f.Function != TestFRCon {
		t.Fatalf("TESTFR con not retained: %+v", f)
	}
	if err := s.Send(sampleMessage()); err != nil {
		t.Fatal(err)
	}
	if f := readFrame(t, p); f.Kind != IFrame || f.Receive != 32 {
		t.Fatalf("duplicate replies or stale piggyback: %+v", f)
	}
}

func TestSessionCoalescedAckDoesNotRegressPiggyback(t *testing.T) {
	opts := DefaultOptions()
	opts.APCI.W = 1
	s, p, _ := manualSession(t, ServerRole, opts, nil)
	startServerSession(t, s, p)
	if err := s.Send(sampleMessage()); err != nil {
		t.Fatal(err)
	}
	waitView(t, s, func(v sessionView) bool { return v.sent == 1 })
	writeFrame(t, p, messageFrame(t, 0, 0))
	waitView(t, s, func(v sessionView) bool { return v.received == 1 })
	if err := s.Send(sampleMessage()); err != nil {
		t.Fatal(err)
	}
	waitView(t, s, func(v sessionView) bool { return v.sent == 2 })
	writeFrame(t, p, messageFrame(t, 1, 0))
	waitView(t, s, func(v sessionView) bool { return v.received == 2 })
	readFrame(t, p)
	if f := readFrame(t, p); f.Kind != SFrame || f.Receive != 2 {
		t.Fatalf("coalesced ACK=%+v", f)
	}
	if f := readFrame(t, p); f.Kind != IFrame || f.Receive != 2 {
		t.Fatalf("piggyback regressed after cumulative S ACK: %+v", f)
	}
}

func TestSessionCumulativeWrapAndPartialTimer(t *testing.T) {
	clock := newFakeClock()
	opts := DefaultOptions()
	opts.Clock = clock
	// Start immediately before the wrap without spending 32766 round trips;
	// the existing full-wire wrap test covers reaching this state.
	st := sessionState{s: &Session{opts: opts}, acked: 32766, sent: 3}
	st.t1 = clock.Now().Add(opts.APCI.T1)
	clock.Advance(10 * time.Second)
	if err := st.acknowledge(1); err != nil {
		t.Fatal(err)
	}
	if st.outstanding() != 2 || !st.t1.Equal(clock.Now().Add(opts.APCI.T1)) {
		t.Fatalf("wrapped partial ACK failed: outstanding=%d deadline=%v", st.outstanding(), st.t1)
	}
	if err := st.acknowledge(3); err != nil || st.outstanding() != 0 || !st.t1.IsZero() {
		t.Fatalf("wrapped cumulative ACK failed: %v %+v", err, st)
	}
}

func TestSessionClientRejectsStopConBeforeAck(t *testing.T) {
	s, p, _ := manualSession(t, ClientRole, DefaultOptions(), nil)
	startClientSession(t, s, p)
	if err := s.Send(sampleMessage()); err != nil {
		t.Fatal(err)
	}
	readFrame(t, p)
	done := make(chan error, 1)
	ctx := testContext(t)
	go func() { done <- s.StopDT(ctx) }()
	if f := readFrame(t, p); f.Function != StopDTAct {
		t.Fatalf("STOPDT=%+v", f)
	}
	writeFrame(t, p, Frame{Kind: UFrame, Function: StopDTCon})
	waitClosed(t, s)
	if !errors.Is(s.Err(), ErrProtocol) {
		t.Fatalf("early STOPDT con accepted: %v", s.Err())
	}
	if err := <-done; !errors.Is(err, ErrProtocol) {
		t.Fatalf("StopDT result=%v", err)
	}
}

func TestServerSendAfterPeerReconnect(t *testing.T) {
	server, err := NewServer(Options{Clock: newFakeClock()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	old, p := attachPipe(t, server)
	startServerSession(t, old, p)
	_ = p.Close() // The peer, not the server application, disconnects.
	waitClosed(t, old)
	fresh, q := attachPipe(t, server)
	startServerSession(t, fresh, q)
	if fresh.ID() <= old.ID() {
		t.Fatal("reconnected identity reused")
	}
	if err := server.Send(old.ID(), sampleMessage()); !errors.Is(err, ErrConnectionGone) {
		t.Fatalf("late send routed: %v", err)
	}
	if err := server.Send(fresh.ID(), sampleMessage()); err != nil {
		t.Fatal(err)
	}
	if f := readFrame(t, q); f.Kind != IFrame || f.Send != 0 {
		t.Fatalf("fresh identity send failed: %+v", f)
	}
	if err := server.Shutdown(testContext(t)); err != nil {
		t.Fatal(err)
	}
}
