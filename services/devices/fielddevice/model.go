// SPDX-License-Identifier: Apache-2.0
package fielddevice

import (
	"errors"
	"sync"
	"time"
)

// Model serializes all remote operations, actions, measurement effects and motion.
type Model struct {
	mu                           sync.Mutex
	truth                        Truth
	normal                       Position
	measurements                 Measurements
	receivedMono                 time.Time
	hasSolve                     bool
	firstSolveRevision           uint64
	clock                        Clock
	started                      time.Time
	motion                       Timer
	motionToken                  uint64
	lastRequest                  uint16
	requestResult                RequestResult
	lastIgnored                  RequestResult
	localContext, processContext AuditContext
	audit                        *AuditLog
}

func NewModel(cfg Config) (*Model, error) {
	switch cfg.Kind {
	case Breaker:
		if cfg.DeviceID != "feeder-breaker" {
			return nil, errors.New("breaker DEVICE_ID must be feeder-breaker")
		}
	case RMU:
		if cfg.DeviceID != "rmu-1" && cfg.DeviceID != "rmu-2" {
			return nil, errors.New("RMU DEVICE_ID must be rmu-1 or rmu-2")
		}
		if cfg.RMUNormal != PositionOpen && cfg.RMUNormal != PositionClosed {
			return nil, errors.New("DEVICE_RMU_NORMAL must be closed or open")
		}
	case OLTC:
		if cfg.DeviceID != "oltc-1" {
			return nil, errors.New("OLTC DEVICE_ID must be oltc-1")
		}
	default:
		return nil, errors.New("DEVICE_KIND must be breaker, rmu or oltc")
	}
	if cfg.Clock == nil {
		cfg.Clock = systemClock{}
	}
	if cfg.LocalContext.Protocol == "" {
		cfg.LocalContext = AuditContext{Peer: "protection", Protocol: "local-protection", Zone: "field"}
	}
	if cfg.ProcessContext.Protocol == "" {
		cfg.ProcessContext = AuditContext{Peer: "measurement-validity", Protocol: "process", Zone: "process"}
	}
	now := cfg.Clock.Now()
	m := &Model{normal: cfg.RMUNormal, clock: cfg.Clock, started: now, localContext: cfg.LocalContext, processContext: cfg.ProcessContext, audit: NewAuditLog(AuditCapacity)}
	m.truth = Truth{Schema: 2, DeviceID: cfg.DeviceID, Kind: cfg.Kind, Generation: cfg.Generation, Revision: 1, ChangedAt: now.UTC(), RemoteEnabled: true}
	m.initialLocked()
	return m, nil
}

func (m *Model) initialLocked() {
	t := &m.truth
	t.RemoteEnabled = true
	switch t.Kind {
	case Breaker:
		t.Position, t.Lockout, t.ProtectionTripped = PositionClosed, false, false
	case RMU:
		t.Position, t.EarthClosed, t.FaultPassage = m.normal, false, false
	case OLTC:
		t.Tap, t.TargetTap, t.Moving, t.Auto, t.Blocked, t.Armed = 0, 0, false, true, false, false
		m.lastRequest, m.requestResult, m.lastIgnored = 0, RequestNone, RequestNone
	}
}
func (m *Model) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked(m.clock.Now())
	return m.snapshotLocked()
}
func (m *Model) Truth() Truth { return m.Snapshot().Truth }
func (m *Model) snapshotLocked() Snapshot {
	return Snapshot{Truth: m.truth, Measurements: m.measurements.clone(), PLCRequestResult: m.requestResult,
		Heartbeat: uint16(m.clock.Now().Sub(m.started) / (250 * time.Millisecond)), LastRequest: m.lastRequest}
}
func (m *Model) changedLocked() {
	m.truth.Revision++
	m.truth.EventSeq++
	m.truth.ChangedAt = m.clock.Now().UTC()
	m.measurements.Valid = false
}
func (m *Model) recordLocked(operation, result string, detail any, before Snapshot, actor AuditContext) {
	m.audit.Add(AuditEntry{At: m.clock.Now().UTC(), Operation: operation, Result: result, Detail: detail, Before: before, After: m.snapshotLocked(), Context: actor})
}
func (m *Model) expireLocked(now time.Time) {
	if !m.measurements.Valid || now.Sub(m.receivedMono) < MeasurementValidity {
		return
	}
	before := m.snapshotLocked()
	m.measurements.Valid = false
	m.recordLocked("measurements_invalid", "accepted", "validity_expired", before, m.processContext)
}
func (m *Model) MeasurementStatus() (bool, *int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.clock.Now()
	m.expireLocked(now)
	if !m.hasSolve {
		return false, nil
	}
	age := max(int64(0), now.Sub(m.receivedMono).Milliseconds())
	return m.measurements.Valid, &age
}
func (m *Model) AuditAfter(after uint64) ([]AuditEntry, *uint64, *uint64) {
	return m.audit.After(after)
}
func (m *Model) Generation() uint32 { m.mu.Lock(); defer m.mu.Unlock(); return m.truth.Generation }
