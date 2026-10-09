// SPDX-License-Identifier: Apache-2.0
package feederbreaker

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

const (
	MeasurementValidity = 3 * time.Second
	AuditCapacity       = 500
)

type Position string

const (
	PositionOpen   Position = "open"
	PositionClosed Position = "closed"
)

type Command string

const (
	CommandOpen  Command = "open"
	CommandClose Command = "close"
)

type AuditContext struct {
	Peer     string `json:"peer"`
	Protocol string `json:"protocol"`
	Zone     string `json:"zone"`
}

type Truth struct {
	Schema            int       `json:"schema"`
	DeviceID          string    `json:"device_id"`
	Generation        uint32    `json:"generation"`
	Revision          uint64    `json:"revision"`
	Position          Position  `json:"position"`
	RemoteEnabled     bool      `json:"remote_enabled"`
	Lockout           bool      `json:"lockout"`
	FaultActive       bool      `json:"fault_active"`
	ProtectionTripped bool      `json:"protection_tripped"`
	EventSeq          uint64    `json:"event_seq"`
	ChangedAt         time.Time `json:"changed_at"`
}

type Measurements struct {
	CurrentA    *float64   `json:"current_a"`
	VoltageKV   *float64   `json:"voltage_kv_ll"`
	Valid       bool       `json:"valid"`
	SolverEpoch *string    `json:"solver_epoch"`
	SolveSeq    *uint64    `json:"solve_seq"`
	SolvedAt    *time.Time `json:"solved_at"`
	ReceivedAt  *time.Time `json:"received_at"`
}

type Snapshot struct {
	Truth
	Measurements Measurements `json:"measurements"`
}

type MeasurementInput struct {
	Schema      int     `json:"schema"`
	DeviceID    string  `json:"device_id"`
	Generation  uint32  `json:"generation"`
	Revision    uint64  `json:"revision"`
	SolverEpoch string  `json:"solver_epoch"`
	SolveSeq    uint64  `json:"solve_seq"`
	SolvedAt    string  `json:"solved_at"`
	CurrentA    float64 `json:"current_a"`
	VoltageKV   float64 `json:"voltage_kv_ll"`
}

type MeasurementError string

const (
	MeasurementStaleTruth MeasurementError = "stale_truth"
	MeasurementReplay     MeasurementError = "replay"
	MeasurementInvalid    MeasurementError = "invalid"
)

func (e MeasurementError) Error() string { return string(e) }

type CommandResult struct {
	Accepted bool
	Changed  bool
	Reason   string
}

type Config struct {
	DeviceID       string
	Generation     uint32
	Now            func() time.Time
	LocalContext   AuditContext
	ProcessContext AuditContext
}

type Model struct {
	mu sync.Mutex

	deviceID          string
	generation        uint32
	revision          uint64
	position          Position
	remoteEnabled     bool
	lockout           bool
	faultActive       bool
	protectionTripped bool
	eventSeq          uint64
	changedAt         time.Time

	currentA           float64
	voltageKV          float64
	measurementsKnown  bool
	measurementsValid  bool
	solverEpoch        string
	solveSeq           uint64
	solvedAt           time.Time
	receivedAt         time.Time
	receivedMono       time.Time
	hasSolve           bool
	firstSolveRevision uint64

	now            func() time.Time
	localContext   AuditContext
	processContext AuditContext
	audit          *AuditLog
}

func NewModel(cfg Config) (*Model, error) {
	if cfg.DeviceID == "" {
		return nil, errors.New("device id must not be empty")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	now := cfg.Now()
	if cfg.LocalContext.Protocol == "" {
		cfg.LocalContext = AuditContext{Peer: "protection", Protocol: "local-protection", Zone: "field"}
	}
	if cfg.ProcessContext.Protocol == "" {
		cfg.ProcessContext = AuditContext{Peer: "measurement-validity", Protocol: "process", Zone: "process"}
	}
	return &Model{
		deviceID:       cfg.DeviceID,
		generation:     cfg.Generation,
		revision:       1,
		position:       PositionClosed,
		remoteEnabled:  true,
		changedAt:      now.UTC(),
		now:            cfg.Now,
		localContext:   cfg.LocalContext,
		processContext: cfg.ProcessContext,
		audit:          NewAuditLog(AuditCapacity),
	}, nil
}

func (m *Model) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireMeasurementsLocked(m.now())
	return m.snapshotLocked()
}

func (m *Model) Truth() Truth {
	return m.Snapshot().Truth
}

func (m *Model) snapshotLocked() Snapshot {
	truth := Truth{
		Schema:            1,
		DeviceID:          m.deviceID,
		Generation:        m.generation,
		Revision:          m.revision,
		Position:          m.position,
		RemoteEnabled:     m.remoteEnabled,
		Lockout:           m.lockout,
		FaultActive:       m.faultActive,
		ProtectionTripped: m.protectionTripped,
		EventSeq:          m.eventSeq,
		ChangedAt:         m.changedAt,
	}
	measurements := Measurements{Valid: m.measurementsValid}
	if m.measurementsKnown {
		currentA := m.currentA
		voltageKV := m.voltageKV
		epoch := m.solverEpoch
		seq := m.solveSeq
		solvedAt := m.solvedAt
		receivedAt := m.receivedAt
		measurements.CurrentA = &currentA
		measurements.VoltageKV = &voltageKV
		measurements.SolverEpoch = &epoch
		measurements.SolveSeq = &seq
		measurements.SolvedAt = &solvedAt
		measurements.ReceivedAt = &receivedAt
	}
	return Snapshot{Truth: truth, Measurements: measurements}
}

func (m *Model) Command(ctx context.Context, command Command, actor AuditContext) CommandResult {
	if err := ctx.Err(); err != nil {
		return CommandResult{Reason: err.Error()}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireMeasurementsLocked(m.now())
	before := m.snapshotLocked()
	reject := func(reason string) CommandResult {
		after := m.snapshotLocked()
		m.audit.Add(AuditEntry{At: m.now().UTC(), Operation: string(command), Result: "rejected", Before: before, After: after, Context: actor, Detail: reason})
		return CommandResult{Reason: reason}
	}
	if command != CommandOpen && command != CommandClose {
		return reject("invalid_command")
	}
	if !m.remoteEnabled {
		return reject("remote_disabled")
	}
	if (command == CommandOpen && m.position == PositionOpen) || (command == CommandClose && m.position == PositionClosed) {
		m.audit.Add(AuditEntry{At: m.now().UTC(), Operation: string(command), Result: "accepted", Before: before, After: before, Context: actor, Detail: "no_op"})
		return CommandResult{Accepted: true}
	}
	if command == CommandClose && m.lockout {
		return reject("lockout")
	}
	if command == CommandClose && m.faultActive {
		return reject("fault_active")
	}

	if command == CommandOpen {
		m.position = PositionOpen
	} else {
		m.position = PositionClosed
	}
	m.truthChangedLocked(m.now())
	after := m.snapshotLocked()
	m.audit.Add(AuditEntry{At: after.ChangedAt, Operation: string(command), Result: "accepted", Before: before, After: after, Context: actor})
	return CommandResult{Accepted: true, Changed: true}
}

func (m *Model) InjectFault(ctx context.Context, active bool, actor AuditContext) (CommandResult, error) {
	if err := ctx.Err(); err != nil {
		return CommandResult{Reason: err.Error()}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireMeasurementsLocked(m.now())
	before := m.snapshotLocked()
	if m.faultActive == active {
		m.audit.Add(AuditEntry{
			At: m.now().UTC(), Operation: "fault", Result: "accepted",
			Before: before, After: before, Context: actor, Detail: "no_op",
		})
		return CommandResult{Accepted: true}, nil
	}

	m.faultActive = active
	if active {
		m.position = PositionOpen
		m.lockout = true
		m.protectionTripped = true
	}
	m.truthChangedLocked(m.now())
	after := m.snapshotLocked()
	m.audit.Add(AuditEntry{At: after.ChangedAt, Operation: "fault", Result: "accepted", Before: before, After: after, Context: actor})
	if active {
		m.audit.Add(AuditEntry{
			At: after.ChangedAt, Operation: "local_protection_trip", Result: "accepted",
			Before: before, After: after, Context: m.localContext,
			Detail: map[string]any{"cause": "fault_active"},
		})
	}
	return CommandResult{Accepted: true, Changed: true}, nil
}

func (m *Model) ClearFault(ctx context.Context, actor AuditContext) (CommandResult, error) {
	return m.InjectFault(ctx, false, actor)
}

func (m *Model) Reset(ctx context.Context, actor AuditContext) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireMeasurementsLocked(m.now())
	before := m.snapshotLocked()
	m.position = PositionClosed
	m.remoteEnabled = true
	m.lockout = false
	m.faultActive = false
	m.protectionTripped = false
	// Reset is an explicit new truth epoch even when already at defaults.
	m.truthChangedLocked(m.now())
	after := m.snapshotLocked()
	m.audit.Add(AuditEntry{At: after.ChangedAt, Operation: "reset", Result: "accepted", Before: before, After: after, Context: actor})
	return nil
}

func (m *Model) ApplyMeasurements(ctx context.Context, input MeasurementInput, actor AuditContext) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return m.Snapshot(), err
	}
	solvedAt, err := validMeasurement(input)
	if err != nil {
		m.recordMeasurementRejection(actor, string(MeasurementInvalid), err.Error())
		return m.Snapshot(), MeasurementInvalid
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.expireMeasurementsLocked(now)
	before := m.snapshotLocked()
	reject := func(reason MeasurementError) (Snapshot, error) {
		after := m.snapshotLocked()
		m.audit.Add(AuditEntry{
			At: now.UTC(), Operation: "measurements", Result: "rejected",
			Before: before, After: after, Context: actor, Detail: string(reason),
		})
		return after, reason
	}
	if input.Schema != 1 || input.DeviceID != m.deviceID {
		return reject(MeasurementInvalid)
	}
	if input.Generation != m.generation || input.Revision != m.revision {
		return reject(MeasurementStaleTruth)
	}
	if m.hasSolve && input.SolverEpoch == m.solverEpoch && input.SolveSeq <= m.solveSeq {
		return reject(MeasurementReplay)
	}

	m.currentA = input.CurrentA
	m.voltageKV = input.VoltageKV
	m.solverEpoch = input.SolverEpoch
	m.solveSeq = input.SolveSeq
	m.solvedAt = solvedAt
	m.receivedAt = now.UTC()
	m.receivedMono = now
	m.hasSolve = true
	m.measurementsKnown = true
	becameValid := !m.measurementsValid
	m.measurementsValid = true
	after := m.snapshotLocked()

	if m.firstSolveRevision != m.revision {
		m.firstSolveRevision = m.revision
		m.audit.Add(AuditEntry{
			At: now.UTC(), Operation: "first_solve", Result: "accepted",
			Before: before, After: after, Context: actor,
			Detail: map[string]any{
				"revision":     m.revision,
				"solver_epoch": input.SolverEpoch,
				"solve_seq":    input.SolveSeq,
				"solved_at":    solvedAt,
				"current_a":    input.CurrentA,
			},
		})
	}
	if becameValid {
		m.audit.Add(AuditEntry{At: now.UTC(), Operation: "measurements_valid", Result: "accepted", Before: before, After: after, Context: actor})
	}
	return after, nil
}

func validMeasurement(input MeasurementInput) (time.Time, error) {
	if input.Schema != 1 {
		return time.Time{}, errors.New("schema must be 1")
	}
	if input.DeviceID == "" || input.SolverEpoch == "" {
		return time.Time{}, errors.New("device_id and solver_epoch are required")
	}
	if input.SolveSeq == 0 {
		return time.Time{}, errors.New("solve_seq must be at least 1")
	}
	if math.IsNaN(input.CurrentA) || math.IsInf(input.CurrentA, 0) || input.CurrentA < 0 ||
		math.IsNaN(input.VoltageKV) || math.IsInf(input.VoltageKV, 0) || input.VoltageKV < 0 ||
		input.CurrentA > math.MaxFloat32 || input.VoltageKV > math.MaxFloat32 {
		return time.Time{}, errors.New("measurements must be finite, non-negative float32 values")
	}
	solvedAt, err := time.Parse(time.RFC3339Nano, input.SolvedAt)
	if err != nil {
		return time.Time{}, fmt.Errorf("solved_at must be RFC3339Nano: %w", err)
	}
	_, offset := solvedAt.Zone()
	if offset != 0 {
		return time.Time{}, errors.New("solved_at must be UTC")
	}
	return solvedAt.UTC(), nil
}

func (m *Model) recordMeasurementRejection(actor AuditContext, reason, detail string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireMeasurementsLocked(m.now())
	snapshot := m.snapshotLocked()
	m.audit.Add(AuditEntry{
		At: m.now().UTC(), Operation: "measurements", Result: "rejected",
		Before: snapshot, After: snapshot, Context: actor,
		Detail: map[string]any{"reason": reason, "message": detail},
	})
}

func (m *Model) expireMeasurementsLocked(now time.Time) {
	if !m.measurementsValid || !m.hasSolve || now.Sub(m.receivedMono) < MeasurementValidity {
		return
	}
	before := m.snapshotLocked()
	m.measurementsValid = false
	after := m.snapshotLocked()
	m.audit.Add(AuditEntry{
		At: now.UTC(), Operation: "measurements_invalid", Result: "accepted",
		Before: before, After: after, Context: m.processContext,
		Detail: map[string]any{"reason": "validity_expired"},
	})
}

func (m *Model) truthChangedLocked(now time.Time) {
	m.revision++
	m.eventSeq++
	m.changedAt = now.UTC()
}

func (m *Model) MeasurementStatus() (bool, *int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	m.expireMeasurementsLocked(now)
	if !m.hasSolve {
		return m.measurementsValid, nil
	}
	age := now.Sub(m.receivedMono).Milliseconds()
	if age < 0 {
		age = 0
	}
	return m.measurementsValid, &age
}

func (m *Model) AuditAfter(after uint64) ([]AuditEntry, *uint64, *uint64) {
	return m.audit.After(after)
}

func (m *Model) Generation() uint32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.generation
}
