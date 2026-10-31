// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"runtime/pprof"
	"sync"
	"testing"
	"time"
)

func TestSessionStopDrainBothRoles(t *testing.T) {
	for _, role := range []Role{ClientRole, ServerRole} {
		t.Run(map[Role]string{ClientRole: "client", ServerRole: "server"}[role], func(t *testing.T) {
			s, p, _ := manualSession(t, role, DefaultOptions(), nil)
			if role == ClientRole {
				startClientSession(t, s, p)
			} else {
				startServerSession(t, s, p)
			}
			if err := s.Send(sampleMessage()); err != nil {
				t.Fatal(err)
			}
			readFrame(t, p)
			if role == ServerRole {
				writeFrame(t, p, Frame{Kind: UFrame, Function: StopDTAct})
				eventually(t, func() bool { return !s.IsStarted() })
				if err := s.Send(sampleMessage()); !errors.Is(err, ErrNotStarted) {
					t.Fatal(err)
				}
				writeFrame(t, p, Frame{Kind: SFrame, Receive: 1})
				if f := readFrame(t, p); f.Function != StopDTCon {
					t.Fatalf("drain=%+v", f)
				}
			} else {
				done := make(chan error, 1)
				ctx := testContext(t)
				go func() { done <- s.StopDT(ctx) }()
				if f := readFrame(t, p); f.Function != StopDTAct {
					t.Fatalf("stop=%+v", f)
				}
				if err := s.Send(sampleMessage()); !errors.Is(err, ErrNotStarted) {
					t.Fatal(err)
				}
				// In-flight server data and S frames remain legal until con.
				writeFrame(t, p, messageFrame(t, 0, 1))
				waitView(t, s, func(v sessionView) bool { return v.received == 1 })
				writeFrame(t, p, Frame{Kind: UFrame, Function: StopDTCon})
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				if f := readFrame(t, p); f.Kind != SFrame || f.Receive != 1 {
					t.Fatalf("final in-flight receive not acknowledged: %+v", f)
				}
			}
			v := waitView(t, s, func(v sessionView) bool { return !v.started })
			if v.pending != 0 || !v.t2.IsZero() {
				t.Fatalf("receive ACK timer survived STOPDT: %+v", v)
			}
			if role == ClientRole {
				startClientSession(t, s, p)
			} else {
				startServerSession(t, s, p)
			}
			if err := s.Send(sampleMessage()); err != nil {
				t.Fatal(err)
			}
			f := readFrame(t, p)
			if f.Send != 1 {
				t.Fatalf("STOPDT reset counter: %+v", f)
			}
		})
	}
}
func TestSessionSequenceWrap(t *testing.T) {
	opts := DefaultOptions()
	opts.APCI.K = 32767
	opts.APCI.W = 128
	opts.EventQueue = 65536
	s, p, clock := manualSession(t, ServerRole, opts, nil)
	startServerSession(t, s, p)
	for i := 0; i <= 32768; i++ {
		if err := s.Send(sampleMessage()); err != nil {
			t.Fatal(err)
		}
		f := readFrame(t, p)
		if f.Send != uint16(i&32767) {
			t.Fatalf("send %d=%d", i, f.Send)
		}
		if i < 32764 {
			writeFrame(t, p, Frame{Kind: SFrame, Receive: uint16(i + 1)})
		}
	}
	// Five outstanding frames span 32764..0. N(R)=0 cumulatively acknowledges
	// four across the wrap and restarts t1 for the one remaining frame.
	before := view(t, s).t1
	clock.Advance(10 * time.Second)
	writeFrame(t, p, Frame{Kind: SFrame, Receive: 0})
	partial := waitView(t, s, func(v sessionView) bool { return v.acked == 0 })
	if !partial.t1.Equal(before.Add(10 * time.Second)) {
		t.Fatalf("wrapped partial ACK did not restart t1: %+v", partial)
	}
	clock.Advance(14 * time.Second) // Past the original deadline, before restart.
	view(t, s)
	writeFrame(t, p, Frame{Kind: SFrame, Receive: 1})
	waitView(t, s, func(v sessionView) bool { return v.acked == 1 && v.t1.IsZero() })
	for i := 0; i <= 32768; i++ {
		writeFrame(t, p, messageFrame(t, uint16(i&32767), 1))
		if i%128 == 127 {
			f := readFrame(t, p)
			if f.Kind != SFrame || f.Receive != uint16((i+1)&32767) {
				t.Fatalf("receive wrap: %+v", f)
			}
		}
	}
	v := waitView(t, s, func(v sessionView) bool { return v.received == 1 && v.sent == 1 && v.acked == 1 })
	if v.pending != 1 {
		t.Fatalf("wrap state=%+v", v)
	}
}
func TestSessionBoundedCallbackBacklog(t *testing.T) {
	blocked := make(chan struct{})
	entered := make(chan struct{})
	events := make(chan EventKind, 10)
	opts := DefaultOptions()
	opts.EventQueue = 2
	s, p, _ := manualSession(t, ServerRole, opts, func(ctx context.Context, e Event) {
		events <- e.Kind
		if e.Kind == ASDUReceived {
			select {
			case <-entered:
			default:
				close(entered)
			}
			<-blocked
		}
	})
	startServerSession(t, s, p)
	// Ensure lifecycle events drained before exhausting precisely the ASDU queue.
	for i := 0; i < 2; i++ {
		<-events
	}
	writeFrame(t, p, messageFrame(t, 0, 0))
	<-entered
	for i := 1; i <= 3; i++ {
		writeFrame(t, p, messageFrame(t, uint16(i), 0))
	}
	select {
	case <-s.transportDone:
	case <-time.After(3 * time.Second):
		t.Fatal("overflow did not close transport")
	}
	if !errors.Is(s.Err(), ErrEventQueueFull) {
		t.Fatal(s.Err())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("uncooperative callback close=%v", err)
	}
	close(blocked)
	waitClosed(t, s)
	var got []EventKind
	for {
		kind := <-events
		got = append(got, kind)
		if kind == Closed {
			break
		}
	}
	if len(got) != 5 {
		t.Fatalf("pending events/Closed lost: %v", got)
	}
}

type countedConn struct {
	net.Conn
	mu     sync.Mutex
	writes [][]byte
	short  bool
}

func (c *countedConn) Write(b []byte) (int, error) {
	c.mu.Lock()
	c.writes = append(c.writes, append([]byte(nil), b...))
	short := c.short
	c.mu.Unlock()
	if short {
		return len(b) - 1, nil
	}
	return c.Conn.Write(b)
}
func TestSessionSingleWriteAndTap(t *testing.T) {
	for _, short := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "short"}[short], func(t *testing.T) {
			a, p := net.Pipe()
			clock := newFakeClock()
			c := &countedConn{Conn: a, short: short}
			opts := DefaultOptions()
			opts.Clock = clock
			var mu sync.Mutex
			var tx, rx [][]byte
			opts.Tap = func(dir Direction, id uint64, at time.Time, b []byte) {
				mu.Lock()
				defer mu.Unlock()
				if id == 0 || at.IsZero() {
					t.Error("tap metadata")
				}
				if dir == Transmit {
					tx = append(tx, append([]byte(nil), b...))
				} else {
					rx = append(rx, append([]byte(nil), b...))
				}
				for i := range b {
					b[i] = 0
				}
			}
			s, err := NewSession(context.Background(), c, ServerRole, opts, nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = p.Close(); closeSession(t, s) })
			writeFrame(t, p, Frame{Kind: UFrame, Function: StartDTAct})
			if short {
				waitClosed(t, s)
				if !errors.Is(s.Err(), io.ErrShortWrite) {
					t.Fatal(s.Err())
				}
			} else {
				readFrame(t, p)
				writeFrame(t, p, Frame{Kind: UFrame, Function: TestFRAct})
				readFrame(t, p)
				view(t, s)
				closeSession(t, s)
			}
			c.mu.Lock()
			defer c.mu.Unlock()
			mu.Lock()
			defer mu.Unlock()
			if len(c.writes) != len(tx) || len(rx) == 0 {
				t.Fatalf("writes=%d tx=%d rx=%d", len(c.writes), len(tx), len(rx))
			}
			for i, b := range c.writes {
				if !bytes.Equal(b, tx[i]) {
					t.Fatal("tap or transport mutated bytes")
				}
				if _, err := DecodeFrame(b); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
func TestSessionClientServerLeakCheck(t *testing.T) {
	baseline := sessionStacks()
	for i := 0; i < 8; i++ {
		a, b := net.Pipe()
		clock := newFakeClock()
		opts := DefaultOptions()
		opts.Clock = clock
		left, err := NewSession(context.Background(), a, ClientRole, opts, nil)
		if err != nil {
			t.Fatal(err)
		}
		right, err := NewSession(context.Background(), b, ServerRole, opts, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := left.StartDT(testContext(t)); err != nil {
			t.Fatal(err)
		}
		closeSession(t, left)
		closeSession(t, right)
		clock.mu.Lock()
		active := len(clock.timers)
		clock.mu.Unlock()
		if active != 0 {
			t.Fatalf("%d timer registrations leaked", active)
		}
	}
	eventually(t, func() bool { return sessionStacks() <= baseline })
}
func sessionStacks() int {
	var b bytes.Buffer
	if err := pprof.Lookup("goroutine").WriteTo(&b, 2); err != nil {
		panic(err)
	}
	return bytes.Count(b.Bytes(), []byte("iec104go.(*Session)."))
}
func TestSessionOptions(t *testing.T) {
	opts := DefaultOptions()
	opts.APCI.W = 32767
	opts.APCI.K = 1
	opts.APCI.T2 = opts.APCI.T1
	if _, err := opts.normalized(); err != nil {
		t.Fatalf("relation rejected: %v", err)
	}
	for _, a := range []APCI{{}, {0, 1, 1, 1, 1, 1}, {32768, 1, 1, 1, 1, 1}, {1, 32768, 1, 1, 1, 1}, {1, 1, 0, 1, 1, 1}, {1, 1, 1, 0, 1, 1}, {1, 1, 1, 1, -1, 1}, {1, 1, 1, 1, 1, 0}} {
		if err := a.Validate(); err == nil {
			t.Fatalf("accepted %+v", a)
		}
	}
}
