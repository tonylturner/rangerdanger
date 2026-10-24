// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"
)

// The actor and production writer can be started independently. This places
// the finish-A/dequeue-B barrier outside production code, without test hooks.
func actorSession(t *testing.T, opts Options) (*Session, net.Conn) {
	t.Helper()
	if opts.Clock == nil {
		opts.Clock = newFakeClock()
	}
	opts, err := opts.normalized()
	if err != nil {
		t.Fatal(err)
	}
	conn, peer := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	s := &Session{conn: conn, role: ServerRole, opts: opts, ctx: ctx, cancel: cancel,
		done: make(chan struct{}), transportDone: make(chan struct{}), stop: make(chan struct{}), wake: make(chan struct{}, 1),
		ops: make(chan operation), in: make(chan incoming, 1), out: make(chan *wireWrite, opts.SendQueue+8), written: make(chan writeResult, 1),
		closeFlush: make(chan chan struct{}, 1), sendReady: make(chan struct{}), inspect: make(chan chan sessionView)}
	s.events.wake = make(chan struct{}, 1)
	go s.dispatch()
	go s.run()
	t.Cleanup(func() { cancel(); _ = peer.Close(); waitClosed(t, s) })
	return s, peer
}

// Gate the actor while it evaluates select operands, after input was disabled.
// This is an injected test Clock, not a hook in the state machine or writer.
type selectGate struct{ entered, release chan struct{} }
type selectGateClock struct {
	*fakeClock
	mu   sync.Mutex
	gate *selectGate
}
type selectGateTimer struct {
	Timer
	clock *selectGateClock
}

func (c *selectGateClock) NewTimer(at time.Time) Timer {
	return &selectGateTimer{c.fakeClock.NewTimer(at), c}
}
func (t *selectGateTimer) C() <-chan time.Time {
	t.clock.mu.Lock()
	g := t.clock.gate
	t.clock.gate = nil
	t.clock.mu.Unlock()
	if g != nil {
		close(g.entered)
		<-g.release
	}
	return t.Timer.C()
}

func TestCadenceWriterDequeueWakesActor(t *testing.T) {
	opts := DefaultOptions()
	opts.SendQueue = 1
	clock := &selectGateClock{fakeClock: newFakeClock()}
	opts.Clock = clock
	claimed := make(chan struct{}, 1)
	opts.Tap = func(d Direction, _ uint64, _ time.Time, b []byte) {
		if d == Transmit && b[2]&1 == 0 {
			claimed <- struct{}{}
		}
	}
	s, p := actorSession(t, opts)
	s.in <- incoming{frame: Frame{Kind: UFrame, Function: StartDTAct}, at: s.opts.Clock.Now()}
	waitView(t, s, func(v sessionView) bool { return v.started })
	a := s.claimWire(<-s.out)
	// A has been claimed, but no writer can claim B yet.
	aDone := make(chan writeResult, 1)
	s.workers.Add(1)
	go func() { defer s.workers.Done(); aDone <- s.writeAPDU(a) }()
	if err := s.Send(sampleMessage()); err != nil {
		t.Fatal(err)
	}
	waitView(t, s, func(v sessionView) bool { return v.sent == 1 })
	s.in <- incoming{frame: Frame{Kind: SFrame, Receive: 1}, at: s.opts.Clock.Now()}
	exactAPDU(t, p, []byte{104, 4, 11, 0, 0, 0})
	completed := <-aDone
	completed.at = s.opts.Clock.Now().Add(time.Second)
	s.written <- completed
	waitView(t, s, func(v sessionView) bool { return v.idle.Equal(completed.at.Add(opts.APCI.T3)) })
	// Force the actor to evaluate a select with incoming=nil while B is full.
	g := &selectGate{make(chan struct{}), make(chan struct{})}
	clock.mu.Lock()
	clock.gate = g
	clock.mu.Unlock()
	signal(s.wake)
	<-g.entered
	select {
	case <-s.wake:
	default:
	}
	// Queueing STOP can complete only after the buffered peer S is consumed;
	// unlike inspect polling, this observation cannot itself wake the actor.
	stopQueued := make(chan struct{})
	go func() {
		select {
		case s.in <- incoming{frame: Frame{Kind: UFrame, Function: StopDTAct}, at: s.opts.Clock.Now()}:
			close(stopQueued)
		case <-s.ctx.Done():
		}
	}()
	s.workers.Add(1)
	go s.writeLoop()
	<-claimed // B is blocked in Write; its dequeue notification is the only wake.
	close(g.release)
	select {
	case <-stopQueued:
	case <-time.After(3 * time.Second):
		t.Fatal("dequeue did not wake buffered peer S")
	}
	v := waitView(t, s, func(v sessionView) bool { return v.acked == 1 })
	if !v.t1.IsZero() {
		t.Fatal("peer S failed to cancel t1")
	}

	f := readFrame(t, p)
	if f.Kind != IFrame {
		t.Fatal(f)
	}
}

func TestCadenceMixedFloodBounds(t *testing.T) {
	opts := DefaultOptions()
	opts.APCI.W = 2
	opts.SendQueue = 2
	s, p := actorSession(t, opts)
	s.workers.Add(1)
	go s.writeLoop()
	var received, sent uint16
	for cycle := 0; cycle < 8; cycle++ {
		s.in <- incoming{frame: Frame{Kind: UFrame, Function: StartDTAct}, at: s.opts.Clock.Now()}
		exactAPDU(t, p, []byte{104, 4, 11, 0, 0, 0})
		if err := s.Send(sampleMessage()); err != nil {
			t.Fatal(err)
		}
		sent++
		waitView(t, s, func(v sessionView) bool { return v.sent == sent })
		// Keep the writer blocked on the spontaneous I while the mixed flood runs.
		base := received
		fed := make(chan struct{})
		go func() {
			for _, f := range []Frame{
				messageFrame(t, base, sent-1), messageFrame(t, base+1, sent-1),
				{Kind: UFrame, Function: TestFRAct}, {Kind: SFrame, Receive: sent},
				{Kind: UFrame, Function: StopDTAct},
			} {
				select {
				case s.in <- incoming{frame: f, at: s.opts.Clock.Now()}:
				case <-s.ctx.Done():
					return
				}
			}
			close(fed)
		}()
		received += 2
		waitView(t, s, func(v sessionView) bool { return v.received == received })
		// An inspect round trip forces quiescence with the writer still blocked.
		v := view(t, s)
		if v.received != received || s.received.Load()-s.acknowledged.Load() != 2 || len(s.out) > opts.SendQueue {
			t.Fatalf("flood bound: %+v accepted=%d acknowledged=%d queue=%d", v, s.received.Load(), s.acknowledged.Load(), len(s.out))
		}
		if f := readFrame(t, p); f.Kind != IFrame {
			t.Fatal(f)
		}
		f := readFrame(t, p)
		if f.Kind != SFrame || f.Receive != received {
			t.Fatal(f)
		}
		exactAPDU(t, p, []byte{104, 4, 131, 0, 0, 0})
		exactAPDU(t, p, []byte{104, 4, 35, 0, 0, 0})
		select {
		case <-fed:
		case <-time.After(3 * time.Second):
			t.Fatal("mixed input stalled")
		}
		waitView(t, s, func(v sessionView) bool { return !v.started && v.acked == sent })
	}
}
