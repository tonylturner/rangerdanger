// SPDX-License-Identifier: Apache-2.0

package iec104go

import (
	"errors"
	"fmt"
	"log"
	"net"
	"time"
)

var (
	// ErrConnectionGone means a retired identity can never route another send.
	ErrConnectionGone = errors.New("iec104go: connection gone")
	// ErrNotStarted means transfer admission is stopped or stopping.
	ErrNotStarted = errors.New("iec104go: transfer not started")
	// ErrQueueFull means bounded admission failed; no message was accepted.
	ErrQueueFull = errors.New("iec104go: queue full")
	// ErrProtocol identifies invalid sequence/state/control exchanges.
	ErrProtocol = errors.New("iec104go: protocol error")
	// ErrT1 identifies expired transmission or U-confirmation supervision.
	ErrT1 = errors.New("iec104go: t1 expired")
	// ErrClosed identifies intentional local closure.
	ErrClosed = errors.New("iec104go: locally closed")
	// ErrEventQueueFull closes a connection whose callback backlog is exhausted.
	ErrEventQueueFull = errors.New("iec104go: callback backlog full")
)

// APCI configures windows and timers. Relations between them are not enforced:
// stress profiles intentionally use equal timers and may select w greater than k.
type APCI struct {
	K, W           int
	T0, T1, T2, T3 time.Duration
}

// DefaultAPCI returns k=12, w=8 and t0/t1/t2/t3=30/15/10/20 seconds.
func DefaultAPCI() APCI {
	return APCI{12, 8, 30 * time.Second, 15 * time.Second, 10 * time.Second, 20 * time.Second}
}

// Validate checks ranges only.
func (a APCI) Validate() error {
	if a.K < 1 || a.K > 32767 || a.W < 1 || a.W > 32767 || a.T0 <= 0 || a.T1 <= 0 || a.T2 <= 0 || a.T3 <= 0 {
		return fmt.Errorf("%w: APCI ranges", ErrMalformed)
	}
	return nil
}

// Clock supplies monotonic time and absolute-deadline timers. Absolute deadlines
// prevent a concurrent fake-clock advance between Now and Reset from extending
// a protocol timeout. Past deadlines must fire immediately. Implementations
// must permit concurrent calls from session reader/writer and state goroutines.
type Clock interface {
	Now() time.Time
	NewTimer(time.Time) Timer
}

// Timer must support Go timer semantics; Reset is called only after Stop/drain.
type Timer interface {
	C() <-chan time.Time
	Stop() bool
	Reset(time.Time) bool
}
type wallClock struct{}

func (wallClock) Now() time.Time              { return time.Now() }
func (wallClock) NewTimer(at time.Time) Timer { return wallTimer{time.NewTimer(time.Until(at))} }

type wallTimer struct{ *time.Timer }

func (t wallTimer) C() <-chan time.Time     { return t.Timer.C }
func (t wallTimer) Reset(at time.Time) bool { return t.Timer.Reset(time.Until(at)) }

// Direction identifies a tap's direction relative to this endpoint.
type Direction uint8

const (
	// Receive observes a complete received APDU.
	Receive Direction = iota
	// Transmit observes an APDU immediately before its single Write attempt.
	Transmit
)

// Tap runs synchronously, serialized per connection. It must return promptly
// and must not call blocking session methods; its byte slice is independently owned.
type Tap func(Direction, uint64, time.Time, []byte)

// Options configures one endpoint. Zero groups/limits select defaults. For
// partial APCI customization start with DefaultOptions: a wholly zero APCI
// selects defaults, but individual zero timers/windows in a custom APCI fail.
type Options struct {
	APCI                  APCI
	Files                 FileOptions
	Params                Params
	SendQueue, EventQueue int
	MaxConnections        int
	Clock                 Clock
	Tap                   Tap
	LocalAddress          *net.TCPAddr
	Logf                  func(string, ...any)
}

// DefaultOptions selects bounded queues (64 sends, 256 callback events) and
// eight independent server connections. There is no implicit originator address.
func DefaultOptions() Options {
	return Options{Files: DefaultFileOptions(), APCI: DefaultAPCI(), Params: DefaultParams(), SendQueue: 64, EventQueue: 256, MaxConnections: 8, Clock: wallClock{}, Logf: log.Printf}
}
func (o Options) normalized() (Options, error) {
	if o.APCI == (APCI{}) {
		o.APCI = DefaultAPCI()
	}
	if o.Params == (Params{}) {
		o.Params = DefaultParams()
	}
	if o.SendQueue == 0 {
		o.SendQueue = 64
	}
	if o.EventQueue == 0 {
		o.EventQueue = 256
	}
	if o.MaxConnections == 0 {
		o.MaxConnections = 8
	}
	if o.Clock == nil {
		o.Clock = wallClock{}
	}
	if o.Logf == nil {
		o.Logf = log.Printf
	}
	var fileErr error
	o.Files, fileErr = o.Files.normalized()
	if fileErr != nil {
		return o, fileErr
	}
	if err := o.APCI.Validate(); err != nil {
		return o, err
	}
	if err := o.Params.Validate(); err != nil {
		return o, err
	}
	if o.Files.SegmentSize > (FileOptions{}).segmentSize(o.Params) {
		return o, fmt.Errorf("%w: file segment size exceeds ASDU capacity", ErrMalformed)
	}
	if o.SendQueue < 1 || o.EventQueue < 1 || o.MaxConnections < 1 {
		return o, fmt.Errorf("%w: queue/connection limits", ErrMalformed)
	}
	return o, nil
}
