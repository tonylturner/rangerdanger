// SPDX-License-Identifier: Apache-2.0

package rtu

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	iec "github.com/tonylturner/iec104go"
	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
)

type fixedClock struct{ synced bool }

func (c fixedClock) Now() (time.Time, bool) {
	return time.Date(2026, 10, 23, 12, 34, 56, 789000000, time.UTC), c.synced
}
func (fixedClock) Run(ctx context.Context) { <-ctx.Done() }

type testAddr string

func (testAddr) Network() string  { return "tcp" }
func (a testAddr) String() string { return string(a) }

type captureConn struct {
	id       uint64
	addr     net.Addr
	messages []iec.Message
	err      error
}

func (c *captureConn) ID() uint64           { return c.id }
func (c *captureConn) RemoteAddr() net.Addr { return c.addr }
func (c *captureConn) LocalAddr() net.Addr  { return testAddr("127.0.0.1:2404") }
func (c *captureConn) Send(m iec.Message) error {
	if _, err := m.MarshalBinary(iec.DefaultParams()); err != nil {
		panic(err)
	}
	owned, err := iec.Mirror(m, m.GetHeader().Cause, m.GetHeader().Negative)
	if err != nil {
		panic(err)
	}
	c.messages = append(c.messages, owned)
	return c.err
}

type captureLog struct {
	lines []string
	err   error
}

func (l *captureLog) Printf(f string, a ...any) error {
	l.lines = append(l.lines, fmt.Sprintf(f, a...))
	return l.err
}
func newModel(t *testing.T, count int) (*RTU, *int64, *captureConn) {
	t.Helper()
	now := int64(100000)
	pm := &pointmap.Map{CA: 1, Timers: pointmap.Timers{T0: 10, T1: 10, T2: 5, T3: 5, K: 12, W: 8}, SelectTimeoutMS: 5000, ReconnectIntervalMS: 2000,
		Devices: []pointmap.Device{{Unit: 1, StaleAfterMS: 3000, TimeoutMS: 1000, PollMS: 300}}}
	for i := 0; i < count; i++ {
		pm.Points = append(pm.Points, pointmap.Point{Kind: pointmap.Float, IOA: i + 1, CyclicMS: 60000, Deadband: .5, Measurement: true})
	}
	r, err := New(pm, Options{Now: func() int64 { return now }, Clock: fixedClock{true}})
	if err != nil {
		t.Fatal(err)
	}
	for i := range r.points {
		r.points[i] = pointState{f: 10, repF: 10, nextCyclic: now + 60000}
	}
	r.devices[0].lastOK = now
	c := &captureConn{id: 1, addr: testAddr("192.0.2.1:10000")}
	r.connectionEvent(c, iec.Started)
	return r, &now, c
}
func wantFrames(t *testing.T, c *captureConn, cot iec.COT, counts ...int) {
	t.Helper()
	if len(c.messages) != len(counts) {
		t.Fatalf("frames=%d want %v", len(c.messages), counts)
	}
	ioa := uint32(1)
	for i, m := range c.messages {
		a := m.(*iec.ASDU)
		h := a.GetHeader()
		if h.Sequence || h.Type != iec.M_ME_NC_1 || h.Cause != cot || len(a.Objects) != counts[i] {
			t.Fatalf("frame %d: %+v", i, a)
		}
		for _, o := range a.Objects {
			if o.Address != ioa {
				t.Fatalf("ioa=%d want %d", o.Address, ioa)
			}
			ioa++
		}
	}
}

// These are the five groups from check/rtu_model_checks.c, using the real
// codec and packing logic rather than a parallel model of reporting policy.
func TestReportingModel(t *testing.T) {
	t.Run("packed cyclic and GI", func(t *testing.T) {
		r, now, c := newModel(t, 64)
		for i := range r.points {
			r.points[i].nextCyclic = *now
		}
		if err := r.reportImage(nil, 0, true, *now); err != nil {
			t.Fatal(err)
		}
		wantFrames(t, c, iec.Periodic, 30, 30, 4)
		c.messages = nil
		if err := r.reportImage(c, 17, false, 0); err != nil {
			t.Fatal(err)
		}
		wantFrames(t, c, iec.InterrogatedStation, 30, 30, 4)
		for _, m := range c.messages {
			if m.GetHeader().Originator != 17 {
				t.Fatal("lost OA")
			}
		}
	})
	t.Run("limiter quality and baselines", func(t *testing.T) {
		r, now, c := newModel(t, 1)
		tick := func(n int64, frames int, cot iec.COT) {
			t.Helper()
			*now = n
			r.devices[0].lastOK = n
			c.messages = nil
			r.evaluate(n)
			if frames == 0 {
				wantFrames(t, c, cot)
			} else {
				wantFrames(t, c, cot, 1)
			}
		}
		r.points[0].f = 11
		tick(100000, 1, iec.Spontaneous)
		r.points[0].f = 12
		tick(100100, 0, 0)
		r.points[0].f = 11.2
		tick(101000, 0, 0)
		if r.points[0].repF != 11 {
			t.Fatal("held value changed baseline")
		}
		r.points[0].f = 12
		tick(101001, 1, iec.Spontaneous)
		r.points[0].measurementInvalid = true
		tick(101010, 1, iec.Spontaneous)
		if r.points[0].repQuality != iec.Invalid {
			t.Fatal("quality did not bypass limiter")
		}
		r.points[0].measurementInvalid = false
		tick(101020, 1, iec.Spontaneous)
		if r.points[0].repQuality != 0 || r.points[0].lastValueEvent != 101001 {
			t.Fatal("quality changed limiter")
		}
		r.points[0].f = 12.2
		tick(101400, 0, 0)
		if err := r.reportImage(c, 0, false, 0); err != nil {
			t.Fatal(err)
		}
		if r.points[0].repF != 12 {
			t.Fatal("GI moved baseline")
		}
		r.points[0].f = 12.6
		tick(102002, 1, iec.Spontaneous)
		r.points[0].f = 13.2
		tick(102010, 0, 0)
		r.points[0].nextCyclic = 102020
		tick(102020, 1, iec.Periodic)
		if r.points[0].repF != 13.2 {
			t.Fatal("cyclic did not move baseline")
		}
		tick(103010, 0, 0)
	})
}

func TestSendFailuresArePerRecipient(t *testing.T) {
	r, _, c := newModel(t, 1)
	log := &captureLog{}
	r.opts.Logger = log
	a := &captureConn{id: 2, addr: testAddr("192.0.2.2:20000"), err: iec.ErrQueueFull}
	b := &captureConn{id: 3, addr: testAddr("192.0.2.3:30000"), err: iec.ErrConnectionGone}
	// Put a healthy recipient after both failures, proving no early return.
	r.started = []startedPeer{{a, a.addr.String()}, {b, b.addr.String()}, {c, c.addr.String()}}
	r.points[0].f = 12
	r.reportPoint(0, iec.Spontaneous)
	if r.Counters().SendFailures != 2 || len(c.messages) != 1 || len(a.messages) != 1 || len(b.messages) != 1 || r.points[0].repF != 12 {
		t.Fatalf("counters=%+v", r.Counters())
	}
	var errors int
	for _, line := range log.lines {
		if strings.HasPrefix(line, "send failed conn=") {
			errors++
		}
	}
	if errors != 2 || len(r.started) != 3 {
		t.Fatalf("logs=%v", log.lines)
	}
}

func TestPointTypesQualityAndTime(t *testing.T) {
	r, _, c := newModel(t, 4)
	kinds := []pointmap.Kind{pointmap.Float, pointmap.StepPosition, pointmap.DoublePoint, pointmap.SinglePoint}
	for i, k := range kinds {
		r.pm.Points[i].Kind = k
		r.pm.Points[i].IOA = 100 + i
		r.points[i] = pointState{dp: 2, sp: true, step: -64, transient: true, f: 12.5, unreachable: true}
	}
	r.opts.Clock = fixedClock{false}
	for i, typ := range []iec.TypeID{iec.M_ME_NC_1, iec.M_ST_TB_1, iec.M_DP_TB_1, iec.M_SP_TB_1} {
		r.reportPoint(i, iec.Spontaneous)
		a := c.messages[i].(*iec.ASDU)
		if a.Header.Type != typ {
			t.Fatal(a.Header)
		}
		if i != 0 {
			timed, ok := a.Objects[0].Element.(iec.Timed)
			if !ok || !timed.Time.Invalid {
				t.Fatal("status time must carry unsynchronized IV")
			}
		}
	}
	c.messages = nil
	if err := r.reportImage(c, 17, false, 0); err != nil {
		t.Fatal(err)
	}
	for i, typ := range []iec.TypeID{iec.M_SP_NA_1, iec.M_DP_NA_1, iec.M_ST_NA_1, iec.M_ME_NC_1} {
		a := c.messages[i].(*iec.ASDU)
		if a.Header.Type != typ || a.Objects[0].Address != uint32(103-i) || a.Header.Cause != iec.InterrogatedStation {
			t.Fatalf("GI kind order: %+v", a)
		}
	}
}
