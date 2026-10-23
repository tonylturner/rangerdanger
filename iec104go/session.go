// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"context"
	"fmt"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Role specifies which endpoint may initiate STARTDT/STOPDT.
type Role uint8

const (
	// ClientRole initiates transfer control.
	ClientRole Role = iota
	// ServerRole answers transfer control.
	ServerRole
)

// EventKind identifies an ordered callback notification.
type EventKind uint8

const (
	// Opened is the first callback on an established transport.
	Opened EventKind = iota
	// Started follows successful STARTDT, never a duplicate act.
	Started
	// Stopped follows completed STOPDT draining.
	Stopped
	// ASDUReceived carries one independently owned decoded message.
	ASDUReceived
	// Closed is delivered exactly once and last, with the terminal reason.
	Closed
)

// Event identifies a connection and either a lifecycle change or ASDU.
type Event struct {
	Kind   EventKind
	ConnID uint64
	// Session is fully initialized even if Opened runs before NewSession returns.
	// It permits handler-safe Send/RequestClose without capturing an unpublished pointer.
	Session *Session
	ASDU    Message
	Err     error
}

// Handler runs serially on one dedicated goroutine per connection, never on
// APCI progress goroutines. It must honor ctx cancellation to permit shutdown.
// Send/Broadcast are safe here; blocking lifecycle methods must be called elsewhere.
type Handler func(context.Context, Event)

var connectionIDs atomic.Uint64

func nextConnID() (uint64, error) {
	for {
		old := connectionIDs.Load()
		if old == math.MaxUint64 {
			return 0, fmt.Errorf("%w: connection identities exhausted", ErrConnectionGone)
		}
		if connectionIDs.CompareAndSwap(old, old+1) {
			return old + 1, nil
		}
	}
}

type operation struct {
	start  bool
	result chan error
}
type incoming struct {
	frame Frame
	err   error
	at    time.Time
	leg   *procedureLeg
}
type queuedASDU struct {
	bytes []byte
	leg   *procedureLeg
}
type wireWrite struct {
	bytes      []byte
	closeAfter bool
	leg        *procedureLeg
}
type writeResult struct {
	err        error
	closeAfter bool
	at         time.Time
}

// Session owns one established transport. It starts STOPPED and never reconnects.
// NewSession is also the net.Pipe/custom-transport entry point; normal TCP users
// use Dial or Server.Serve. All successful Send admissions own encoded bytes.
type Session struct {
	id                  uint64
	conn                net.Conn
	role                Role
	opts                Options
	ctx                 context.Context
	cancel              context.CancelFunc
	done, transportDone chan struct{}
	stopOnce            sync.Once
	stop                chan struct{}
	wake                chan struct{}
	ops                 chan operation
	in                  chan incoming
	out                 chan *wireWrite
	written             chan writeResult
	workers             sync.WaitGroup
	tapMu               sync.Mutex
	wireMu              sync.Mutex
	pendingAck          *wireWrite
	pendingTestCon      *wireWrite
	received            atomic.Uint32
	mu                  sync.Mutex
	accepting, gone     bool
	reason              error
	sends               []queuedASDU
	procedure           *transaction
	procedureUncertain  bool
	events              eventQueue
	handler             Handler
	retired             func()
	inspect             chan chan sessionView
}

// NewSession takes ownership of conn only on success. Parent cancellation
// closes the transport. Close waits for the ordered callback goroutine too.
func NewSession(ctx context.Context, conn net.Conn, role Role, opts Options, handler Handler) (*Session, error) {
	return newSession(ctx, conn, role, opts, handler, nil)
}
func newSession(ctx context.Context, conn net.Conn, role Role, opts Options, handler Handler, retired func()) (*Session, error) {
	opts, err := opts.normalized()
	if err != nil {
		return nil, err
	}
	if conn == nil || role > ServerRole {
		return nil, fmt.Errorf("%w: transport/role", ErrMalformed)
	}
	id, err := nextConnID()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Session{id: id, conn: conn, role: role, opts: opts, ctx: ctx, cancel: cancel, done: make(chan struct{}), transportDone: make(chan struct{}), stop: make(chan struct{}), wake: make(chan struct{}, 1), ops: make(chan operation), in: make(chan incoming, 1), out: make(chan *wireWrite, opts.SendQueue+8), written: make(chan writeResult, 1), handler: handler, retired: retired, inspect: make(chan chan sessionView)}
	s.events.wake = make(chan struct{}, 1)
	s.queueEvent(Event{Kind: Opened})
	s.workers.Add(2)
	go s.readLoop()
	go s.writeLoop()
	go s.dispatch()
	go s.run()
	return s, nil
}

// ID returns a process-wide monotonic identity, never reused, including on reconnect.
func (s *Session) ID() uint64 { return s.id }

// Done closes after all transport workers and the final Closed callback exit.
func (s *Session) Done() <-chan struct{} { return s.done }

// Err returns the terminal reason, or nil while the transport is alive.
func (s *Session) Err() error { s.mu.Lock(); defer s.mu.Unlock(); return s.reason }

// Send immediately admits a byte-owned ASDU to a bounded queue. It never waits
// for the wire, a handler, an acknowledgement or the k window.
func (s *Session) Send(m Message) error {
	// Check identity before encoding, so even an invalid message to a retired
	// identity consistently reports ConnectionGone rather than a codec error.
	s.mu.Lock()
	gone := s.gone
	s.mu.Unlock()
	if gone {
		return ErrConnectionGone
	}
	owned, err := cloneMessage(m)
	if err != nil {
		return err
	}
	b, err := owned.MarshalBinary(s.opts.Params)
	if err != nil {
		return err
	}
	return s.admit(b)
}
func (s *Session) admit(b []byte) error {
	return s.admitLeg(b, nil)
}
func (s *Session) admitLeg(b []byte, leg *procedureLeg) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gone {
		return ErrConnectionGone
	}
	if !s.accepting {
		return ErrNotStarted
	}
	if len(s.sends) == s.opts.SendQueue {
		return ErrQueueFull
	}
	s.sends = append(s.sends, queuedASDU{append([]byte(nil), b...), leg})
	signal(s.wake)
	return nil
}

// IsStarted reports current Send admission, which stops immediately on STOPDT request.
func (s *Session) IsStarted() bool  { s.mu.Lock(); defer s.mu.Unlock(); return s.accepting && !s.gone }
func (s *Session) admission(v bool) { s.mu.Lock(); s.accepting = v; s.mu.Unlock() }

// StartDT requests client transfer and waits for confirmation, context cancellation
// or t1 failure. The protocol exchange continues if only the caller cancels.
func (s *Session) StartDT(ctx context.Context) error { return s.transfer(ctx, true) }

// StopDT freezes new admission, drains previously admitted objects to the wire,
// sends STOPDT act and waits for con. Counters are never reset.
func (s *Session) StopDT(ctx context.Context) error { return s.transfer(ctx, false) }
func (s *Session) transfer(ctx context.Context, start bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.role != ClientRole {
		return fmt.Errorf("%w: only client initiates transfer", ErrProtocol)
	}
	op := operation{start: start, result: make(chan error, 1)}
	select {
	case s.ops <- op:
	case <-s.transportDone:
		return ErrConnectionGone
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-op.result:
		return err
	case <-s.transportDone:
		return s.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RequestClose initiates local closure without waiting, including from a handler.
func (s *Session) RequestClose() { s.stopOnce.Do(func() { close(s.stop) }) }

// Close waits for all owned goroutines, including callbacks. A handler ignoring
// cancellation cannot be forcibly killed by Go; ctx bounds that application error.
func (s *Session) Close(ctx context.Context) error {
	s.RequestClose()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func signal(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}
