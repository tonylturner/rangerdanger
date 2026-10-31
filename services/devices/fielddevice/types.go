// SPDX-License-Identifier: Apache-2.0
// Package fielddevice owns the shared truth, measurement and audit core for EU devices.
package fielddevice

import (
	"encoding/json"
	"time"
)

const (
	MeasurementValidity = 3 * time.Second
	MotionDuration      = 2 * time.Second
	AuditCapacity       = 500
)

type Kind string

const (
	Breaker Kind = "breaker"
	RMU     Kind = "rmu"
	OLTC    Kind = "oltc"
)

type Position string

const (
	PositionOpen   Position = "open"
	PositionClosed Position = "closed"
)

type AuditContext struct {
	Peer     string `json:"peer"`
	Protocol string `json:"protocol"`
	Zone     string `json:"zone"`
}

// Truth is a typed internal union; wireFields emits only the selected kind's keys.
type Truth struct {
	Schema                       int
	DeviceID                     string
	Kind                         Kind
	Generation                   uint32
	Revision                     uint64
	EventSeq                     uint64
	ChangedAt                    time.Time
	RemoteEnabled                bool
	Position                     Position
	Lockout, ProtectionTripped   bool
	EarthClosed, FaultPassage    bool
	Tap, TargetTap               int16
	Moving, Auto, Blocked, Armed bool
}

func (t Truth) wireFields() map[string]any {
	fields := map[string]any{"schema": t.Schema, "device_id": t.DeviceID, "kind": t.Kind,
		"generation": t.Generation, "revision": t.Revision, "event_seq": t.EventSeq,
		"changed_at": t.ChangedAt, "remote_enabled": t.RemoteEnabled}
	switch t.Kind {
	case Breaker:
		fields["position"], fields["lockout"], fields["protection_tripped"] = t.Position, t.Lockout, t.ProtectionTripped
	case RMU:
		fields["position"], fields["earth_closed"], fields["fault_passage"] = t.Position, t.EarthClosed, t.FaultPassage
	case OLTC:
		fields["tap"], fields["target_tap"], fields["moving"] = t.Tap, t.TargetTap, t.Moving
		fields["auto"], fields["blocked"], fields["armed"] = t.Auto, t.Blocked, t.Armed
	}
	return fields
}
func (t Truth) MarshalJSON() ([]byte, error) { return json.Marshal(t.wireFields()) }

type Measurements struct {
	Valid       bool       `json:"valid"`
	CurrentA    *float64   `json:"current_a"`
	VoltageKV   *float64   `json:"voltage_kv_ll"`
	SourceKV    *float64   `json:"source_kv_ll"`
	SectionLive *bool      `json:"section_live"`
	SourceLive  *bool      `json:"source_live"`
	PMW         *float64   `json:"p_mw"`
	QMvar       *float64   `json:"q_mvar"`
	PF          *float64   `json:"pf"`
	SolverEpoch *string    `json:"solver_epoch"`
	SolveSeq    *uint64    `json:"solve_seq"`
	SolvedAt    *time.Time `json:"solved_at"`
	ReceivedAt  *time.Time `json:"received_at"`
}

func copyValue[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func (m Measurements) clone() Measurements {
	m.CurrentA = copyValue(m.CurrentA)
	m.VoltageKV = copyValue(m.VoltageKV)
	m.SourceKV = copyValue(m.SourceKV)
	m.SectionLive = copyValue(m.SectionLive)
	m.SourceLive = copyValue(m.SourceLive)
	m.PMW = copyValue(m.PMW)
	m.QMvar = copyValue(m.QMvar)
	m.PF = copyValue(m.PF)
	m.SolverEpoch = copyValue(m.SolverEpoch)
	m.SolveSeq = copyValue(m.SolveSeq)
	m.SolvedAt = copyValue(m.SolvedAt)
	m.ReceivedAt = copyValue(m.ReceivedAt)
	return m
}

type Snapshot struct {
	Truth
	Protection       Protection
	Measurements     Measurements
	PLCRequestResult RequestResult
	Heartbeat        uint16
	LastRequest      uint16
}

func (s Snapshot) MarshalJSON() ([]byte, error) {
	fields := s.Truth.wireFields()
	fields["measurements"] = s.Measurements
	fields["protection"] = s.Protection
	if s.Kind == OLTC {
		fields["plc_request_result"] = s.PLCRequestResult.String()
		fields["heartbeat"] = s.Heartbeat
	}
	return json.Marshal(fields)
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
	SourceKV    float64 `json:"source_kv_ll"`
	SectionLive bool    `json:"section_live"`
	SourceLive  bool    `json:"source_live"`
	PMW         float64 `json:"p_mw"`
	QMvar       float64 `json:"q_mvar"`
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
	// Exception is nonzero only for malformed register addresses or values.
	Exception byte
}

type RequestResult uint16

const (
	RequestNone RequestResult = iota
	RequestAccepted
	RequestNotRemote
	RequestNotAuto
	RequestMoving
	RequestNotAdjacent
	RequestLimit // Reserved by the wire map; HR1 encoding rejects out-of-range targets with exception 03.
	RequestBlocked
	RequestUnarmed
	RequestNotEnergized
)

func (r RequestResult) String() string {
	return [...]string{"none", "accepted", "not_remote", "not_auto", "moving", "not_adjacent", "limit", "blocked", "unarmed", "not_energized"}[r]
}

// Clock supplies both elapsed time and non-blocking motion timers for deterministic tests.
type Clock interface {
	Now() time.Time
	AfterFunc(time.Duration, func()) Timer
}
type Timer interface{ Stop() bool }
type systemClock struct{}

func (systemClock) Now() time.Time                            { return time.Now() }
func (systemClock) AfterFunc(d time.Duration, f func()) Timer { return time.AfterFunc(d, f) }

// Protection is the sample-quantised protection state exposed with measurements.
type Protection struct {
	Function         string     `json:"function"`
	PickupA          float64    `json:"pickup_a"`
	DelayMS          int64      `json:"delay_ms"`
	InstantaneousA   *float64   `json:"instantaneous_a"`
	PickedUp         bool       `json:"picked_up"`
	PickedUpSince    *time.Time `json:"picked_up_since"`
	LastTripCurrentA *float64   `json:"last_trip_current_a"`
}

// ProtectionSettings are device-owned thresholds and accepted-sample timing.
type ProtectionSettings struct {
	PickupA        float64
	DelayMS        int64
	InstantaneousA *float64
	MaxGapMS       int64
}

// DefaultProtection returns the EU teaching profile for the device function.
func DefaultProtection(kind Kind) ProtectionSettings {
	s := ProtectionSettings{MaxGapMS: 2500}
	switch kind {
	case Breaker:
		v := 2000.0
		s.PickupA = 400
		s.DelayMS = 1000
		s.InstantaneousA = &v
	case RMU:
		s.PickupA = 300
	case OLTC:
		s.PickupA = 866
	}
	return s
}

type Config struct {
	Protection     *ProtectionSettings
	Kind           Kind
	DeviceID       string
	RMUNormal      Position
	Generation     uint32
	Clock          Clock
	LocalContext   AuditContext
	ProcessContext AuditContext
}
