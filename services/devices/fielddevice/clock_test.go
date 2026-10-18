// SPDX-License-Identifier: Apache-2.0
package fielddevice

import (
	"context"
	"testing"
	"time"
)

type fakeTimer struct {
	at      time.Time
	call    func()
	stopped bool
}

func (t *fakeTimer) Stop() bool { was := !t.stopped; t.stopped = true; return was }

type fakeClock struct {
	now    time.Time
	timers []*fakeTimer
}

func (c *fakeClock) Now() time.Time { return c.now }
func (c *fakeClock) AfterFunc(d time.Duration, f func()) Timer {
	timer := &fakeTimer{at: c.now.Add(d), call: f}
	c.timers = append(c.timers, timer)
	return timer
}
func (c *fakeClock) advance(d time.Duration) {
	end := c.now.Add(d)
	for {
		var next *fakeTimer
		for _, timer := range c.timers {
			if !timer.stopped && !timer.at.After(end) && (next == nil || timer.at.Before(next.at)) {
				next = timer
			}
		}
		if next == nil {
			break
		}
		c.now, next.stopped = next.at, true
		next.call()
	}
	c.now = end
}

var testActor = AuditContext{Peer: "10.30.30.20:43001", Protocol: "modbus", Zone: "station"}
var labActor = AuditContext{Peer: "10.99.99.10:43001", Protocol: "lab", Zone: "mgmt"}

func newTestModel(t *testing.T, kind Kind) (*Model, *fakeClock) {
	t.Helper()
	c := &fakeClock{now: time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)}
	id := map[Kind]string{Breaker: "feeder-breaker", RMU: "rmu-1", OLTC: "oltc-1"}[kind]
	m, err := NewModel(Config{Kind: kind, DeviceID: id, RMUNormal: PositionClosed, Generation: 42, Clock: c})
	if err != nil {
		t.Fatal(err)
	}
	return m, c
}
func testMeasurement(m *Model) MeasurementInput {
	t := m.Truth()
	seq := uint64(1)
	if m.hasSolve {
		seq = *m.measurements.SolveSeq + 1
	}
	return MeasurementInput{Schema: 3, DeviceID: t.DeviceID, Generation: t.Generation, Revision: t.Revision, SolverEpoch: "solver-a", SolveSeq: seq, SolvedAt: time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC).Add(time.Duration(seq) * time.Second).Format(time.RFC3339Nano), CurrentA: 41.2, VoltageKV: 19.93, SourceKV: 20, SectionLive: true, SourceLive: false}
}
func measure(t *testing.T, m *Model, edit func(*MeasurementInput)) Snapshot {
	t.Helper()
	input := testMeasurement(m)
	if edit != nil {
		edit(&input)
	}
	s, err := m.ApplyMeasurements(context.Background(), input, AuditContext{Peer: "10.50.50.20:12000", Protocol: "process", Zone: "process"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func action(t *testing.T, m *Model, name string) Snapshot {
	t.Helper()
	s, err := m.Action(context.Background(), name, labActor)
	if err != nil {
		t.Fatalf("action %s: %v", name, err)
	}
	return s
}
func write(t *testing.T, m *Model, address, value uint16) CommandResult {
	t.Helper()
	result := m.WriteHolding(context.Background(), address, value, testActor)
	if !result.Accepted {
		t.Fatalf("write HR%d=%d: %+v", address, value, result)
	}
	return result
}
