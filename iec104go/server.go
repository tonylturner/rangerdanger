// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"context"
	"errors"
	"net"
	"sort"
	"sync"
)

// BroadcastResult reports admission separately for every snapshotted recipient.
type BroadcastResult struct {
	ConnID uint64
	Err    error
}

// Server owns independent sessions, not an active/standby redundancy group.
type Server struct {
	opts            Options
	handler         Handler
	mu              sync.Mutex
	listener        net.Listener
	serving, closed bool
	serveEnded      bool
	sessions        map[uint64]*Session
	done            chan struct{}
	doneOnce        sync.Once
}

// NewServer creates a server; Serve takes ownership of its listener.
func NewServer(opts Options, handler Handler) (*Server, error) {
	opts, err := opts.normalized()
	if err != nil {
		return nil, err
	}
	return &Server{opts: opts, handler: handler, sessions: make(map[uint64]*Session), done: make(chan struct{})}, nil
}

// Serve accepts until cancellation, Shutdown, or a terminal listener error.
// The ninth connection with default limits is accepted and immediately closed.
func (s *Server) Serve(ctx context.Context, listener net.Listener) error {
	s.mu.Lock()
	if s.closed || s.serving {
		s.mu.Unlock()
		return ErrClosed
	}
	s.serving = true
	s.listener = listener
	s.mu.Unlock()
	// One cancellation watcher belongs to this Serve invocation and is joined
	// before return; it never outlives a supplied external listener.
	stop := make(chan struct{})
	joined := make(chan struct{})
	go func() {
		defer close(joined)
		select {
		case <-ctx.Done():
			_ = listener.Close()
		case <-stop:
		}
	}()
	defer func() {
		close(stop)
		<-joined
		s.mu.Lock()
		s.serveEnded = true
		s.checkDone()
		s.mu.Unlock()
	}()
	for {
		conn, err := listener.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed || errors.Is(err, net.ErrClosed) {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if err := s.attach(ctx, conn); err != nil {
			_ = conn.Close()
			s.opts.Logf("iec104go rejected %s: %v", conn.RemoteAddr(), err)
		}
	}
}
func (s *Server) attach(ctx context.Context, conn net.Conn) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	active := 0
	for _, session := range s.sessions {
		session.mu.Lock()
		gone := session.gone
		session.mu.Unlock()
		if !gone {
			active++
		}
	}
	if active >= s.opts.MaxConnections {
		return ErrQueueFull
	}
	// Retirement may run before publication, but cannot acquire this mutex
	// until publication completes, so a fast EOF never resurrects an old ID.
	var session *Session
	var err error
	session, err = newSession(ctx, conn, ServerRole, s.opts, s.handler, func() { s.mu.Lock(); delete(s.sessions, session.id); s.checkDone(); s.mu.Unlock() })
	if err != nil {
		return err
	}
	s.sessions[session.id] = session
	return nil
}

// Send admits to exactly id, never a recycled connection or another peer.
func (s *Server) Send(id uint64, m Message) error {
	s.mu.Lock()
	session := s.sessions[id]
	s.mu.Unlock()
	if session == nil {
		return ErrConnectionGone
	}
	return session.Send(m)
}

// Broadcast snapshots started identities once, then enqueues in ConnID order.
// A recipient stopping after the snapshot returns ErrNotStarted individually.
func (s *Server) Broadcast(m Message) []BroadcastResult {
	s.mu.Lock()
	var sessions []*Session
	for _, session := range s.sessions {
		if session.IsStarted() {
			sessions = append(sessions, session)
		}
	}
	s.mu.Unlock()
	sort.Slice(sessions, func(i, j int) bool { return sessions[i].id < sessions[j].id })
	results := make([]BroadcastResult, 0, len(sessions))
	for _, session := range sessions {
		results = append(results, BroadcastResult{session.id, session.Send(m)})
	}
	return results
}
func (s *Server) checkDone() {
	if s.closed && (!s.serving || s.serveEnded) && len(s.sessions) == 0 {
		s.doneOnce.Do(func() { close(s.done) })
	}
}

// Shutdown closes admission and the listener, cancels all sessions, and joins
// transport and callback goroutines. Cancellation-ignoring handlers are bounded
// by ctx, not forcibly terminated. Do not call this blocking method in a handler.
func (s *Server) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	if s.listener != nil {
		_ = s.listener.Close()
	}
	for _, session := range s.sessions {
		session.RequestClose()
	}
	s.checkDone()
	s.mu.Unlock()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
