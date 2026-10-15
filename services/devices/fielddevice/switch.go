// SPDX-License-Identifier: Apache-2.0
package fielddevice

import "context"

// WriteHolding validates and applies a single register write under the core lock.
func (m *Model) WriteHolding(ctx context.Context, address, value uint16, actor AuditContext) CommandResult {
	if err := ctx.Err(); err != nil {
		return CommandResult{Reason: err.Error()}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked(m.clock.Now())
	before := m.snapshotLocked()
	if m.truth.Kind == OLTC {
		return m.writeOLTCLocked(address, value, actor, before)
	}
	if address != 0 {
		return CommandResult{Exception: 2, Reason: "invalid_address"}
	}
	if value != 1 && value != 2 {
		return CommandResult{Exception: 3, Reason: "invalid_value"}
	}
	operation := "open"
	target := PositionOpen
	if value == 2 {
		operation, target = "close", PositionClosed
	}
	reject := func(reason string) CommandResult {
		m.recordLocked(operation, "rejected", reason, before, actor)
		return CommandResult{Reason: reason}
	}
	if !m.truth.RemoteEnabled {
		return reject("not_remote")
	}
	if value == 2 {
		if m.truth.Kind == Breaker && m.truth.Lockout {
			return reject("lockout")
		}
		if m.truth.Kind == RMU && m.truth.EarthClosed {
			return reject("earth_closed")
		}
		if !m.measurements.Valid {
			return reject("no_measurement")
		}
		if *m.measurements.SourceLive && *m.measurements.SectionLive {
			return reject("live_live")
		}
	} else if m.truth.Kind == RMU && m.measurements.Valid && *m.measurements.FaultCurrent {
		return reject("fault_current")
	}
	if m.truth.Position == target {
		m.recordLocked(operation, "accepted", "no_op", before, actor)
		return CommandResult{Accepted: true}
	}
	m.truth.Position = target
	m.changedLocked()
	m.recordLocked(operation, "accepted", "", before, actor)
	return CommandResult{Accepted: true, Changed: true}
}
