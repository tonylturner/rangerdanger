// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"context"
	"errors"
	"net"
	"sort"
	"sync"
	"testing"
	"time"
)

func attachPipe(t *testing.T, s *Server) (*Session, net.Conn) {
	t.Helper()
	a, p := net.Pipe()
	if err := s.attach(context.Background(), a); err != nil {
		_ = a.Close()
		_ = p.Close()
		t.Fatal(err)
	}
	s.mu.Lock()
	var session *Session
	for _, candidate := range s.sessions {
		if session == nil || candidate.id > session.id {
			session = candidate
		}
	}
	s.mu.Unlock()
	t.Cleanup(func() { _ = p.Close(); closeSession(t, session) })
	return session, p
}
func TestServerBroadcastConcurrentStart(t *testing.T) {
	server, err := NewServer(Options{Clock: newFakeClock()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := server.Shutdown(testContext(t)); err != nil {
			t.Error(err)
		}
	})
	var sessions []*Session
	var peers []net.Conn
	for i := 0; i < 3; i++ {
		s, p := attachPipe(t, server)
		sessions = append(sessions, s)
		peers = append(peers, p)
		if i < 2 {
			startServerSession(t, s, p)
		}
	}
	gate := make(chan struct{})
	started := make(chan struct{})
	go func() {
		<-gate
		writeFrame(t, peers[2], Frame{Kind: UFrame, Function: StartDTAct})
		readFrame(t, peers[2])
		close(started)
	}()
	close(gate)
	results := server.Broadcast(sampleMessage())
	<-started
	if len(results) < 2 || len(results) > 3 {
		t.Fatalf("snapshot=%v", results)
	}
	var ids []uint64
	for _, r := range results {
		if r.Err != nil {
			t.Fatal(r.Err)
		}
		ids = append(ids, r.ConnID)
	}
	if !sort.SliceIsSorted(ids, func(i, j int) bool { return ids[i] < ids[j] }) {
		t.Fatal("broadcast order")
	}
	for _, r := range results {
		for i, s := range sessions {
			if s.id == r.ConnID {
				f := readFrame(t, peers[i])
				if f.Kind != IFrame {
					t.Fatalf("recipient %+v", f)
				}
			}
		}
	}
	if err := server.Send(^uint64(0), sampleMessage()); !errors.Is(err, ErrConnectionGone) {
		t.Fatal(err)
	}
}
func TestServerBroadcastSlowRecipient(t *testing.T) {
	opts := DefaultOptions()
	opts.Clock = newFakeClock()
	opts.SendQueue = 1
	opts.APCI.K = 1
	server, err := NewServer(opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, p := attachPipe(t, server)
	second, q := attachPipe(t, server)
	startServerSession(t, first, p)
	startServerSession(t, second, q)
	if err := first.Send(sampleMessage()); err != nil {
		t.Fatal(err)
	}
	readFrame(t, p)
	if err := first.Send(sampleMessage()); err != nil {
		t.Fatal(err)
	}
	results := server.Broadcast(sampleMessage())
	if len(results) != 2 || !errors.Is(results[0].Err, ErrQueueFull) || results[1].Err != nil {
		t.Fatalf("partial broadcast=%+v", results)
	}
	if f := readFrame(t, q); f.Kind != IFrame {
		t.Fatal("slow recipient blocked other")
	}
	closeSession(t, first)
	third, r := attachPipe(t, server)
	startServerSession(t, third, r)
	if third.id <= second.id || third.id <= first.id {
		t.Fatal("identity reused")
	}
	if err := server.Send(first.id, sampleMessage()); !errors.Is(err, ErrConnectionGone) {
		t.Fatal(err)
	}
	if err := server.Shutdown(testContext(t)); err != nil {
		t.Fatal(err)
	}
}
func TestServerClientTCPMaxConnections(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	logs := make(chan string, 2)
	opts := DefaultOptions()
	opts.Clock = newFakeClock()
	opts.Logf = func(format string, args ...any) { logs <- format }
	server, err := NewServer(opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := testContext(t)
	served := make(chan error, 1)
	go func() { served <- server.Serve(ctx, listener) }()
	clients := make(chan *Client, 8)
	failures := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, err := Dial(ctx, listener.Addr().String(), Options{Clock: newFakeClock(), LocalAddress: &net.TCPAddr{IP: net.ParseIP("127.0.0.1")}}, nil)
			if err == nil {
				err = c.StartDT(ctx)
			}
			if err != nil {
				failures <- err
				if c != nil {
					c.RequestClose()
				}
				return
			}
			clients <- c
		}()
	}
	wg.Wait()
	close(clients)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	var all []*Client
	for c := range clients {
		all = append(all, c)
	}
	if len(all) != 8 {
		t.Fatalf("connected %d", len(all))
	}
	ninth, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if err := ninth.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var one [1]byte
	if _, err := ninth.Read(one[:]); err == nil {
		t.Fatal("ninth stayed open")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("ninth connection was not closed immediately")
	}
	_ = ninth.Close()
	select {
	case <-logs:
	case <-ctx.Done():
		t.Fatal("rejection was not logged")
	}
	results := server.Broadcast(sampleMessage())
	if len(results) != 8 {
		t.Fatalf("active recipients=%d", len(results))
	}
	if err := server.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-served; err != nil {
		t.Fatal(err)
	}
	for _, c := range all {
		if err := c.Close(ctx); err != nil {
			t.Fatal(err)
		}
	}
}
func TestClientDialCancellationAndSource(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Dial(ctx, "127.0.0.1:1", Options{}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("dial cancellation=%v", err)
	}
	server, err := NewServer(Options{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Shutdown(testContext(t)); err != nil {
		t.Fatal(err)
	}
}
