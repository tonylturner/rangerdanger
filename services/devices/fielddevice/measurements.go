// SPDX-License-Identifier: Apache-2.0
package fielddevice

import (
	"context"
	"math"
	"strings"
	"time"
)

func validMeasurement(input MeasurementInput) (time.Time, error) {
	if input.Schema != 3 || input.DeviceID == "" || input.SolverEpoch == "" || input.SolveSeq == 0 {
		return time.Time{}, MeasurementInvalid
	}
	for _, value := range []float64{input.CurrentA, input.VoltageKV, input.SourceKV} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > math.MaxFloat32 {
			return time.Time{}, MeasurementInvalid
		}
	}
	for _, value := range []float64{input.PMW, input.QMvar} {
		if !finiteSetting(value) || math.Abs(value) > math.MaxFloat32 {
			return time.Time{}, MeasurementInvalid
		}
	}
	solved, err := time.Parse(time.RFC3339Nano, input.SolvedAt)
	if err != nil || !strings.HasSuffix(input.SolvedAt, "Z") || len(input.SolvedAt) < 20 || len(input.SolvedAt) > 40 || input.SolvedAt[10] != 'T' || input.SolvedAt[13] != ':' || input.SolvedAt[16] != ':' || strings.Contains(input.SolvedAt, ",") {
		return time.Time{}, MeasurementInvalid
	}
	return solved.UTC(), nil
}

// ApplyMeasurements runs protection before returning the post-measurement revision.
func (m *Model) ApplyMeasurements(ctx context.Context, input MeasurementInput, actor AuditContext) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return m.Snapshot(), err
	}
	solved, validation := validMeasurement(input)
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.clock.Now()
	before := m.snapshotLocked()
	reject := func(reason MeasurementError) (Snapshot, error) {
		m.recordLocked("measurements", "rejected", string(reason), before, actor)
		return m.snapshotLocked(), reason
	}
	if validation != nil || input.DeviceID != m.truth.DeviceID {
		return reject(MeasurementInvalid)
	}
	if input.Generation != m.truth.Generation || input.Revision != m.truth.Revision {
		return reject(MeasurementStaleTruth)
	}
	if m.hasSolve && input.SolverEpoch == *m.measurements.SolverEpoch && (input.SolveSeq <= *m.measurements.SolveSeq || !solved.After(*m.measurements.SolvedAt)) {
		return reject(MeasurementReplay)
	}
	m.expireLocked(now)
	if m.hasSolve && (input.SolverEpoch != *m.measurements.SolverEpoch || input.Generation != m.protectionGeneration || solved.Sub(*m.measurements.SolvedAt) > m.maxGap) {
		m.resetPickupLocked()
	}
	m.protectionGeneration = input.Generation
	received := now.UTC()
	var pf *float64
	if apparent := math.Hypot(input.PMW, input.QMvar); apparent >= 0.001 {
		v := math.Abs(input.PMW) / apparent
		pf = &v
	}
	becameValid := !m.measurements.Valid
	m.measurements = Measurements{Valid: true, CurrentA: &input.CurrentA, VoltageKV: &input.VoltageKV, SourceKV: &input.SourceKV,
		SectionLive: &input.SectionLive, SourceLive: &input.SourceLive, PMW: &input.PMW, QMvar: &input.QMvar, PF: pf,
		SolverEpoch: &input.SolverEpoch, SolveSeq: &input.SolveSeq, SolvedAt: &solved, ReceivedAt: &received}
	m.hasSolve, m.receivedMono = true, now
	if m.firstSolveRevision != m.truth.Revision {
		m.firstSolveRevision = m.truth.Revision
		m.recordLocked("first_solve", "accepted", map[string]any{"revision": input.Revision, "solver_epoch": input.SolverEpoch, "solve_seq": input.SolveSeq, "solved_at": solved, "current_a": input.CurrentA}, before, actor)
	}
	if becameValid {
		m.recordLocked("measurements_valid", "accepted", "", before, actor)
	}
	m.protection.PickedUp = input.CurrentA >= m.protection.PickupA
	if !m.protection.PickedUp {
		m.resetPickupLocked()
	} else if m.protection.PickedUpSince == nil {
		m.protection.PickedUpSince = &solved
	}
	instantaneous := m.protection.InstantaneousA != nil && input.CurrentA >= *m.protection.InstantaneousA
	if instantaneous || (m.protection.PickedUp && solved.Sub(*m.protection.PickedUpSince) >= time.Duration(m.protection.DelayMS)*time.Millisecond) {
		beforeEffect := m.snapshotLocked()
		switch m.truth.Kind {
		case Breaker:
			if m.truth.Position == PositionClosed {
				m.protection.LastTripCurrentA = copyValue(&input.CurrentA)
				m.truth.Position, m.truth.Lockout, m.truth.ProtectionTripped = PositionOpen, true, true
				m.changedLocked()
				m.recordLocked("local_protection_trip", "accepted", map[string]any{"current_a": input.CurrentA}, beforeEffect, m.localContext)
			}
		case RMU:
			if !m.truth.FaultPassage {
				m.truth.FaultPassage = true
				m.changedLocked()
				m.recordLocked("fault_passage", "accepted", map[string]any{"current_a": input.CurrentA}, beforeEffect, m.localContext)
			}
		}
	}
	return m.snapshotLocked(), nil
}

func finiteSetting(v float64) bool  { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func (m *Model) resetPickupLocked() { m.protection.PickedUp = false; m.protection.PickedUpSince = nil }
