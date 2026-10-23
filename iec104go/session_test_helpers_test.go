// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"context"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"
)

type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers map[*fakeTimer]bool
}
type fakeTimer struct {
	clock    *fakeClock
	ch       chan time.Time
	deadline time.Time
	active   bool
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(1000, 0), timers: make(map[*fakeTimer]bool)}
}
func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *fakeClock) NewTimer(at time.Time) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{clock: c, ch: make(chan time.Time, 1), deadline: at, active: true}
	c.timers[t] = true
	c.fire(t)
	return t
}
func (c *fakeClock) fire(t *fakeTimer) {
	if t.active && !c.now.Before(t.deadline) {
		t.active = false
		select {
		case t.ch <- c.now:
		default:
		}
	}
}
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	for t := range c.timers {
		c.fire(t)
	}
}
func (t *fakeTimer) C() <-chan time.Time { return t.ch }
func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	old := t.active
	t.active = false
	delete(t.clock.timers, t)
	return old
}
func (t *fakeTimer) Reset(at time.Time) bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	old := t.active
	t.active = true
	t.deadline = at
	t.clock.timers[t] = true
	t.clock.fire(t)
	return old
}
func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func eventually(t *testing.T, p func() bool) {
	t.Helper()
	limit := time.Now().Add(3 * time.Second)
	for !p() {
		if time.Now().After(limit) {
			t.Fatal("condition did not become true")
		}
		runtime.Gosched()
	}
}
func view(t *testing.T, s *Session) sessionView {
	t.Helper()
	result := make(chan sessionView, 1)
	select {
	case s.inspect <- result:
	case <-s.transportDone:
		t.Fatalf("session closed: %v", s.Err())
	case <-time.After(3 * time.Second):
		t.Fatal("inspect timed out")
	}
	return <-result
}
func waitView(t *testing.T, s *Session, p func(sessionView) bool) sessionView {
	t.Helper()
	var v sessionView
	eventually(t, func() bool { v = view(t, s); return p(v) })
	return v
}
func closeSession(t *testing.T, s *Session) {
	t.Helper()
	if err := s.Close(testContext(t)); err != nil {
		t.Fatal(err)
	}
}
func manualSession(t *testing.T, role Role, opts Options, handler Handler) (*Session, net.Conn, *fakeClock) {
	t.Helper()
	clock := newFakeClock()
	opts.Clock = clock
	conn, peer := net.Pipe()
	s, err := NewSession(context.Background(), conn, role, opts, handler)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = peer.Close(); closeSession(t, s) })
	return s, peer, clock
}
func writeFrame(t *testing.T, peer net.Conn, f Frame) {
	t.Helper()
	b, err := f.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if err := peer.SetWriteDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := peer.Write(b); err != nil {
		t.Fatal(err)
	}
}
func readFrame(t *testing.T, peer net.Conn) Frame {
	t.Helper()
	if err := peer.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	f, err := NewAPDUReader(peer).ReadFrame()
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func startServerSession(t *testing.T, s *Session, peer net.Conn) {
	t.Helper()
	writeFrame(t, peer, Frame{Kind: UFrame, Function: StartDTAct})
	if f := readFrame(t, peer); f.Function != StartDTCon {
		t.Fatalf("start=%+v", f)
	}
	waitView(t, s, func(v sessionView) bool { return v.started })
}
func startClientSession(t *testing.T, s *Session, peer net.Conn) {
	t.Helper()
	done := make(chan error, 1)
	ctx := testContext(t)
	go func() { done <- s.StartDT(ctx) }()
	if f := readFrame(t, peer); f.Function != StartDTAct {
		t.Fatalf("start=%+v", f)
	}
	writeFrame(t, peer, Frame{Kind: UFrame, Function: StartDTCon})
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
func sampleMessage() ASDU { return request(1, 0, M_SP_NA_1, 1, SinglePoint{SIQ: 1}) }
func messageFrame(t *testing.T, send, receive uint16) Frame {
	t.Helper()
	b, err := sampleMessage().MarshalBinary(DefaultParams())
	if err != nil {
		t.Fatal(err)
	}
	return Frame{Kind: IFrame, Send: send, Receive: receive, ASDU: b}
}
func waitClosed(t *testing.T, s *Session) {
	t.Helper()
	select {
	case <-s.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("close did not join")
	}
}
