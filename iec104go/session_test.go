// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"context"
	"errors"
	"io"
	"net"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestSessionWindowAndQueue(t *testing.T) {
	opts := DefaultOptions()
	opts.APCI.K = 1
	opts.SendQueue = 1
	s, peer, _ := manualSession(t, ServerRole, opts, nil)
	startServerSession(t, s, peer)
	if err := s.Send(sampleMessage()); err != nil {
		t.Fatal(err)
	}
	if f := readFrame(t, peer); f.Send != 0 {
		t.Fatalf("first=%+v", f)
	}
	waitView(t, s, func(v sessionView) bool { return v.sent == 1 })
	if err := s.Send(sampleMessage()); err != nil {
		t.Fatal(err)
	}
	if err := s.Send(sampleMessage()); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("queue=%v", err)
	}
	writeFrame(t, peer, Frame{Kind: SFrame, Receive: 1})
	if f := readFrame(t, peer); f.Send != 1 {
		t.Fatalf("released=%+v", f)
	}
	writeFrame(t, peer, Frame{Kind: SFrame, Receive: 2})
	waitView(t, s, func(v sessionView) bool { return v.acked == 2 })
}
func TestSessionReceiveAcks(t *testing.T) {
	t.Run("w", func(t *testing.T) {
		o := DefaultOptions()
		o.APCI.W = 2
		s, p, _ := manualSession(t, ServerRole, o, nil)
		startServerSession(t, s, p)
		writeFrame(t, p, messageFrame(t, 0, 0))
		waitView(t, s, func(v sessionView) bool { return v.pending == 1 })
		writeFrame(t, p, messageFrame(t, 1, 0))
		f := readFrame(t, p)
		if f.Kind != SFrame || f.Receive != 2 {
			t.Fatalf("ack=%+v", f)
		}
	})
	t.Run("t2-first-not-last", func(t *testing.T) {
		s, p, clock := manualSession(t, ServerRole, DefaultOptions(), nil)
		startServerSession(t, s, p)
		writeFrame(t, p, messageFrame(t, 0, 0))
		first := waitView(t, s, func(v sessionView) bool { return v.pending == 1 }).t2
		clock.Advance(6 * time.Second)
		writeFrame(t, p, messageFrame(t, 1, 0))
		v := waitView(t, s, func(v sessionView) bool { return v.pending == 2 })
		if !v.t2.Equal(first) {
			t.Fatal("later receive restarted t2")
		}
		clock.Advance(4 * time.Second)
		if f := readFrame(t, p); f.Kind != SFrame || f.Receive != 2 {
			t.Fatalf("t2 ack=%+v", f)
		}
	})
	t.Run("piggyback", func(t *testing.T) {
		s, p, _ := manualSession(t, ServerRole, DefaultOptions(), nil)
		startServerSession(t, s, p)
		writeFrame(t, p, messageFrame(t, 0, 0))
		waitView(t, s, func(v sessionView) bool { return v.pending == 1 })
		if err := s.Send(sampleMessage()); err != nil {
			t.Fatal(err)
		}
		if f := readFrame(t, p); f.Kind != IFrame || f.Receive != 1 {
			t.Fatalf("piggyback=%+v", f)
		}
		v := waitView(t, s, func(v sessionView) bool { return v.pending == 0 })
		if !v.t2.IsZero() {
			t.Fatal("t2 not canceled")
		}
	})
}
func TestSessionT1(t *testing.T) {
	for _, name := range []string{"I", "START", "TEST", "STOP"} {
		t.Run(name, func(t *testing.T) {
			role := ClientRole
			if name == "I" || name == "TEST" {
				role = ServerRole
			}
			s, p, clock := manualSession(t, role, DefaultOptions(), nil)
			switch name {
			case "I":
				startServerSession(t, s, p)
				if err := s.Send(sampleMessage()); err != nil {
					t.Fatal(err)
				}
				readFrame(t, p)
			case "START":
				go s.StartDT(testContext(t))
				readFrame(t, p)
			case "TEST":
				view(t, s)
				clock.Advance(20 * time.Second)
				if f := readFrame(t, p); f.Function != TestFRAct {
					t.Fatalf("test=%+v", f)
				}
			case "STOP":
				startClientSession(t, s, p)
				go s.StopDT(testContext(t))
				readFrame(t, p)
			}
			// Observe the registered deadline before advancing virtual time.
			view(t, s)
			clock.Advance(15 * time.Second)
			waitClosed(t, s)
			if !errors.Is(s.Err(), ErrT1) {
				t.Fatalf("reason=%v", s.Err())
			}
		})
	}
	t.Run("partial-restart", func(t *testing.T) {
		s, p, clock := manualSession(t, ServerRole, DefaultOptions(), nil)
		startServerSession(t, s, p)
		for i := 0; i < 2; i++ {
			if err := s.Send(sampleMessage()); err != nil {
				t.Fatal(err)
			}
			readFrame(t, p)
		}
		before := waitView(t, s, func(v sessionView) bool { return v.sent == 2 }).t1
		clock.Advance(10 * time.Second)
		writeFrame(t, p, Frame{Kind: SFrame, Receive: 1})
		after := waitView(t, s, func(v sessionView) bool { return v.acked == 1 }).t1
		if !after.Equal(before.Add(10 * time.Second)) {
			t.Fatalf("restart %v -> %v", before, after)
		}
		clock.Advance(5 * time.Second)
		view(t, s)
		clock.Advance(10 * time.Second)
		waitClosed(t, s)
		if !errors.Is(s.Err(), ErrT1) {
			t.Fatal(s.Err())
		}
	})
}
func TestSessionT3BothStates(t *testing.T) {
	for _, started := range []bool{false, true} {
		t.Run(map[bool]string{false: "STOPPED", true: "STARTED"}[started], func(t *testing.T) {
			s, p, clock := manualSession(t, ServerRole, DefaultOptions(), nil)
			if started {
				startServerSession(t, s, p)
			}
			v := view(t, s)
			clock.Advance(v.idle.Sub(clock.Now()))
			if f := readFrame(t, p); f.Function != TestFRAct {
				t.Fatalf("test=%+v", f)
			}
			writeFrame(t, p, Frame{Kind: UFrame, Function: TestFRCon})
			writeFrame(t, p, Frame{Kind: UFrame, Function: TestFRAct})
			if f := readFrame(t, p); f.Function != TestFRCon {
				t.Fatalf("reply=%+v", f)
			}
		})
	}
}
func TestSessionProtocolErrors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		role  Role
		start bool
		frame Frame
	}{
		{"I-stopped", ServerRole, false, messageFrame(t, 0, 0)},
		{"S-stopped", ServerRole, false, Frame{Kind: SFrame}},
		{"bad-NR", ServerRole, true, Frame{Kind: SFrame, Receive: 1}},
		{"bad-NS", ServerRole, true, messageFrame(t, 1, 0)},
		{"client-act", ClientRole, false, Frame{Kind: UFrame, Function: StartDTAct}},
		{"server-con", ServerRole, false, Frame{Kind: UFrame, Function: StartDTCon}},
		{"unsolicited-test-con", ServerRole, false, Frame{Kind: UFrame, Function: TestFRCon}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, p, _ := manualSession(t, tc.role, DefaultOptions(), nil)
			if tc.start {
				startServerSession(t, s, p)
			}
			writeFrame(t, p, tc.frame)
			waitClosed(t, s)
			if !errors.Is(s.Err(), ErrProtocol) {
				t.Fatalf("reason=%v", s.Err())
			}
		})
	}
	t.Run("mismatch-acks-before-close", func(t *testing.T) {
		s, p, _ := manualSession(t, ServerRole, DefaultOptions(), nil)
		startServerSession(t, s, p)
		writeFrame(t, p, messageFrame(t, 0, 0))
		waitView(t, s, func(v sessionView) bool { return v.pending == 1 })
		writeFrame(t, p, messageFrame(t, 2, 0))
		f := readFrame(t, p)
		if f.Kind != SFrame || f.Receive != 1 {
			t.Fatalf("pending ack=%+v", f)
		}
		waitClosed(t, s)
		if !errors.Is(s.Err(), ErrProtocol) {
			t.Fatal(s.Err())
		}
	})
}
func TestSessionEventsIdempotenceAndRetention(t *testing.T) {
	events := make(chan Event, 20)
	s, p, _ := manualSession(t, ServerRole, DefaultOptions(), func(_ context.Context, e Event) { events <- e })
	startServerSession(t, s, p)
	writeFrame(t, p, Frame{Kind: UFrame, Function: StartDTAct})
	readFrame(t, p)
	writeFrame(t, p, messageFrame(t, 0, 0))
	waitView(t, s, func(v sessionView) bool { return v.received == 1 })
	var retained Message
	var kinds []EventKind
	for retained == nil {
		e := <-events
		kinds = append(kinds, e.Kind)
		if e.Kind == ASDUReceived {
			retained = e.ASDU
		}
	}
	result := make(chan error, 1)
	go func() { result <- s.Send(retained) }()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	f := readFrame(t, p)
	m, err := DecodeASDU(f.ASDU, DefaultParams())
	if err != nil || m.GetHeader().Type != M_SP_NA_1 {
		t.Fatalf("retained=%v", err)
	}
	writeFrame(t, p, Frame{Kind: SFrame, Receive: 1})
	waitView(t, s, func(v sessionView) bool { return v.acked == 1 })
	writeFrame(t, p, Frame{Kind: UFrame, Function: StopDTAct})
	if f := readFrame(t, p); f.Function != StopDTCon {
		t.Fatalf("stop=%+v", f)
	}
	closeSession(t, s)
	for {
		e := <-events
		kinds = append(kinds, e.Kind)
		if e.Kind == Closed {
			break
		}
	}
	if !reflect.DeepEqual(kinds, []EventKind{Opened, Started, ASDUReceived, Stopped, Closed}) {
		t.Fatalf("events=%v", kinds)
	}
	if err := s.Send(retained); !errors.Is(err, ErrConnectionGone) {
		t.Fatalf("retired send=%v", err)
	}
}
func TestSessionCallbackDoesNotStall(t *testing.T) {
	entered := make(chan struct{})
	exited := make(chan struct{})
	var once sync.Once
	s, p, clock := manualSession(t, ServerRole, DefaultOptions(), func(ctx context.Context, e Event) {
		if e.Kind == ASDUReceived {
			once.Do(func() { close(entered) })
			<-ctx.Done()
			close(exited)
		}
	})
	startServerSession(t, s, p)
	writeFrame(t, p, messageFrame(t, 0, 0))
	<-entered
	clock.Advance(10 * time.Second)
	if f := readFrame(t, p); f.Kind != SFrame {
		t.Fatalf("callback stalled ack: %+v", f)
	}
	writeFrame(t, p, Frame{Kind: UFrame, Function: TestFRAct})
	if f := readFrame(t, p); f.Function != TestFRCon {
		t.Fatal("callback stalled TESTFR")
	}
	if err := s.Send(sampleMessage()); err != nil {
		t.Fatal(err)
	}
	readFrame(t, p)
	view(t, s)
	clock.Advance(15 * time.Second)
	waitClosed(t, s)
	<-exited
	if !errors.Is(s.Err(), ErrT1) {
		t.Fatal(s.Err())
	}
}
func TestSessionEOFAndLocalClose(t *testing.T) {
	s, p, _ := manualSession(t, ServerRole, DefaultOptions(), nil)
	_ = p.Close()
	waitClosed(t, s)
	if !errors.Is(s.Err(), io.EOF) {
		t.Fatalf("FIN reason=%v", s.Err())
	}
	a, b := net.Pipe()
	other, err := NewSession(context.Background(), a, ClientRole, Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	closeSession(t, other)
	_ = b.Close()
	if !errors.Is(other.Err(), ErrClosed) {
		t.Fatal(other.Err())
	}
}
