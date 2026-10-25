// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"reflect"
	"sync"
	"time"

	iec "github.com/tonylturner/iec104go"
)

type response struct {
	header  iec.Header
	objects []iec.Object
}

// Only response envelopes enter the ring; monitoring bursts cannot evict them.
// The receive callback never blocks on an operation that is sleeping between SBO legs.
type responses struct {
	mu         sync.Mutex
	ring       [32]response
	count      uint64
	closed     bool
	quietClose bool
	changed    chan struct{}
	started    chan struct{}
	stopped    chan struct{}
}

func newResponses() *responses {
	return &responses{changed: make(chan struct{}, 1), started: make(chan struct{}, 1), stopped: make(chan struct{}, 1)}
}

func notify(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

func (r *responses) handler(o *output, monitor bool) iec.Handler {
	return func(_ context.Context, e iec.Event) {
		switch e.Kind {
		case iec.Opened:
			o.logf("link opened")
		case iec.Started:
			o.logf("link startdt-con")
			notify(r.started)
		case iec.Stopped:
			o.logf("link stopdt-con")
			notify(r.stopped)
		case iec.Closed:
			r.mu.Lock()
			quiet := r.quietClose
			r.mu.Unlock()
			if !quiet {
				o.logf("link closed")
			}
			r.mu.Lock()
			r.closed = true
			r.mu.Unlock()
			notify(r.changed)
		case iec.ASDUReceived:
			o.asdu(e.ASDU, monitor)
			h := e.ASDU.GetHeader()
			if h.Test || h.Cause != iec.ActivationCon && h.Cause != iec.ActivationTerm {
				return
			}
			a, ok := e.ASDU.(*iec.ASDU)
			if !ok {
				return
			}
			r.mu.Lock()
			r.ring[r.count%32] = response{header: h, objects: append([]iec.Object(nil), a.Objects...)}
			r.count++
			r.mu.Unlock()
			notify(r.changed)
		}
	}
}

func (r *responses) cursor() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.count
}
func (r *responses) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

func (r *responses) suppressCloseLog() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.quietClose = true
}

func (r *responses) wait(ctx context.Context, cursor *uint64, expected iec.ASDU, term bool, timeout time.Duration, clock iec.Clock) (response, bool) {
	timer := clock.NewTimer(clock.Now().Add(timeout))
	defer timer.Stop()
	for {
		r.mu.Lock()
		if r.count > *cursor+32 {
			*cursor = r.count - 32
		}
		for *cursor < r.count {
			v := r.ring[*cursor%32]
			*cursor++
			wantCause := iec.ActivationCon
			if term {
				wantCause = iec.ActivationTerm
			}
			if responseMatches(expected, v, wantCause) {
				r.mu.Unlock()
				return v, true
			}
		}
		closed := r.closed
		r.mu.Unlock()
		if closed {
			return response{}, false
		}
		select {
		case <-ctx.Done():
			return response{}, false
		case <-timer.C():
			return response{}, false
		case <-r.changed:
		}
	}
}

func responseMatches(expected iec.ASDU, actual response, cause iec.COT) bool {
	h, want := actual.header, expected.Header
	if h.Type != want.Type || h.CommonAddress != want.CommonAddress || h.Originator != want.Originator || h.Test || h.Cause != cause || len(actual.objects) != len(expected.Objects) {
		return false
	}
	for i, obj := range actual.objects {
		if obj.Address != expected.Objects[i].Address || !reflect.DeepEqual(obj.Element, expected.Objects[i].Element) {
			return false
		}
	}
	return true
}
