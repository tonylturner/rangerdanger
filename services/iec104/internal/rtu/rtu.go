// SPDX-License-Identifier: Apache-2.0

// Package rtu implements the lab's IEC 104 to Modbus gateway.
package rtu

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	iec "github.com/tonylturner/iec104go"
	"github.com/tturner/rangerdanger/services/iec104/internal/modbus"
	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
	"github.com/tturner/rangerdanger/services/iec104/internal/stationclock"
)

// Master is the single-owner southbound transport used by a device worker.
type Master interface {
	Resolve(context.Context, string, int) modbus.Status
	Connect(time.Duration) modbus.Status
	IsConnected() bool
	Close()
	ReadInput(int, int, time.Duration) ([]uint16, modbus.Result)
	ReadHolding(int, int, time.Duration) ([]uint16, modbus.Result)
	WriteSingle(int, uint16, time.Duration) modbus.Result
}

// StationClock supplies UTC and independently reported synchronization quality.
type StationClock interface {
	Now() (time.Time, bool)
	Run(context.Context)
}

// Logger writes complete evidence lines and reports failures to the RTU.
type Logger interface{ Printf(string, ...any) error }

// Options exposes deterministic model seams. Now returns monotonic
// milliseconds, must be concurrency-safe, and must not return zero.
type Options struct {
	Now       func() int64
	Clock     StationClock
	NewMaster func(byte) Master
	Logger    Logger
}

type pointState struct {
	dp, repDP                       uint8
	sp, repSP                       bool
	f, repF                         float32
	step, repStep                   int
	transient, repTransient         bool
	unreachable, measurementInvalid bool
	valueInvalid, heartbeatStale    bool
	repQuality                      iec.Quality
	nextCyclic, lastValueEvent      int64
}

type deviceState struct {
	lastOK, pollStart, heartbeatChanged int64
	pollSeq                             uint64
	heartbeat                           uint16
	unreachable                         bool
}

type connection interface {
	ID() uint64
	RemoteAddr() net.Addr
	LocalAddr() net.Addr
	Send(iec.Message) error
}

type startedPeer struct {
	conn connection
	peer string
}

// stationServer keeps lifecycle ordering injectable without substituting the
// station's model or production protocol callbacks.
type stationServer interface {
	Serve(context.Context, net.Listener) error
	Shutdown(context.Context) error
}

type selection struct {
	active               bool
	index, value, qu     int
	id                   uint64
	peer                 string
	deadline, selectedAt int64
	pollSeq              uint64
}

type commandPhase uint8

const (
	commandStart commandPhase = iota
	commandWrite
	commandWriting
	commandFeedback
)

type pendingCommand struct {
	active                 bool
	index, value, expected int
	id                     uint64
	peer                   string
	request                iec.Message
	phase                  commandPhase
	afterPoll              uint64
	afterTime, deadline    int64
}

// Counters counts failed admissions per recipient, not per ASDU. Reporting
// baselines still advance without recipients, just as in the reference.
type Counters struct{ SendFailures, BuildFailures, LogFailures uint64 }

// RTU owns the model lock; device workers alone own their Modbus connections.
// No network operation that waits for a peer is performed under this lock.
type RTU struct {
	pm          *pointmap.Map
	opts        Options
	server      stationServer
	mu          sync.Mutex
	points      []pointState
	devices     []deviceState
	started     []startedPeer
	sel         selection
	pending     pendingCommand
	sendFailed  atomic.Uint64
	buildFailed atomic.Uint64
	logFailed   atomic.Uint64
}

// New constructs a station from an already validated, immutable point map.
func New(pm *pointmap.Map, opts Options) (*RTU, error) {
	if opts.Now == nil {
		origin := time.Now()
		opts.Now = func() int64 { return time.Since(origin).Milliseconds() + 1000 }
	}
	if opts.NewMaster == nil {
		opts.NewMaster = func(unit byte) Master { return modbus.New(unit) }
	}
	r := &RTU{pm: pm, opts: opts, points: make([]pointState, len(pm.Points)), devices: make([]deviceState, len(pm.Devices))}
	if r.opts.Clock == nil {
		r.opts.Clock = stationclock.New(pm.Clock, stationclock.Options{Logf: r.printf})
	}
	for i := range r.points {
		r.points[i] = pointState{dp: 3, repDP: 3, unreachable: true, repQuality: iec.Invalid | iec.NotTopical}
	}
	o := iec.DefaultOptions()
	t := pm.Timers
	o.APCI = iec.APCI{K: t.K, W: t.W, T0: seconds(t.T0), T1: seconds(t.T1), T2: seconds(t.T2), T3: seconds(t.T3)}
	o.EventQueue = 1024
	o.Logf = r.printf
	server, err := iec.NewServer(o, r.handle)
	if err != nil {
		return nil, err
	}
	r.server = server
	return r, nil
}

func seconds(n int) time.Duration { return time.Duration(n) * time.Second }
func millis(n int) time.Duration  { return time.Duration(n) * time.Millisecond }

// Counters snapshots observability counters without taking the model lock.
func (r *RTU) Counters() Counters {
	return Counters{r.sendFailed.Load(), r.buildFailed.Load(), r.logFailed.Load()}
}

func (r *RTU) printf(format string, args ...any) {
	if r.opts.Logger != nil {
		if err := r.opts.Logger.Printf(format, args...); err != nil {
			r.logFailed.Add(1)
		}
	}
}

func (r *RTU) buildError(err error) {
	if err != nil {
		r.buildFailed.Add(1)
		r.printf("ASDU build error: %v", err)
	}
}

// Serve takes ownership of listener and joins all owned workers on shutdown.
// Cancellation does not retry or interrupt a possibly applied FC6: that
// worker completes its bounded transaction before its transport is closed.
func (r *RTU) Serve(ctx context.Context, listener net.Listener) error {
	// Shutdown can beat the server goroutine to startup. The stack then
	// rejects Serve without taking the listener, so ownership stays here.
	defer listener.Close()
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var workers sync.WaitGroup
	workers.Add(1)
	go func() { defer workers.Done(); r.opts.Clock.Run(runCtx) }()
	for d := range r.pm.Devices {
		workers.Add(1)
		go func(d int) { defer workers.Done(); r.deviceWorker(runCtx, d) }(d)
	}
	served := make(chan error, 1)
	go func() {
		err := r.server.Serve(runCtx, listener)
		// Classify at the failure boundary, before cleanup cancels workers.
		// Only a caller-requested stop makes local close/cancellation clean;
		// a failure observed before that stop must survive later teardown.
		if ctx.Err() != nil && (errors.Is(err, iec.ErrClosed) ||
			errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			err = nil
		}
		served <- err
	}()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	var result error
	var serveFinished bool
selectLoop:
	for {
		select {
		case <-ctx.Done():
			break selectLoop
		case result = <-served:
			serveFinished = true
			break selectLoop
		case <-ticker.C:
			r.tick()
		}
	}
	r.printf("stopping")
	cancel()
	workers.Wait()
	shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	if err := r.server.Shutdown(shutdown); err != nil {
		return fmt.Errorf("IEC shutdown: %w", err)
	}
	if !serveFinished {
		result = <-served
	}
	return result
}

func (r *RTU) tick() {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.opts.Now()
	if r.sel.active && now >= r.sel.deadline {
		r.clearSelection("select timeout elapsed")
	}
	r.evaluate(now)
	r.advanceCommand(now)
}

func pause(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
