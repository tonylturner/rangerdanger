// SPDX-License-Identifier: Apache-2.0
package fielddevice

import "context"

type ActionError string

func (e ActionError) Error() string { return string(e) }

const ActionUnknown ActionError = "unknown_action"

// Action is the only lab control route; electrical faults belong to the process.
func (m *Model) Action(ctx context.Context, action string, actor AuditContext) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return m.Snapshot(), err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.expireLocked(m.clock.Now())
	before := m.snapshotLocked()
	reject := func(reason ActionError) (Snapshot, error) {
		m.recordLocked(action, "rejected", string(reason), before, actor)
		return m.snapshotLocked(), reason
	}
	if action == "reset" {
		m.motionToken++
		if m.motion != nil {
			m.motion.Stop()
			m.motion = nil
		}
		m.initialLocked()
	} else {
		switch m.truth.Kind {
		case Breaker:
			if action != "reset_lockout" {
				return reject(ActionUnknown)
			}
			m.truth.Position = PositionOpen
			m.truth.Lockout, m.truth.ProtectionTripped = false, false
		case RMU:
			switch action {
			case "set_local":
				m.truth.RemoteEnabled = false
			case "set_remote":
				m.truth.RemoteEnabled = true
			case "earth_close":
				if m.truth.Position != PositionOpen {
					return reject("way_closed")
				}
				if !m.measurements.Valid {
					return reject("no_measurement")
				}
				if *m.measurements.SectionLive {
					return reject("section_live")
				}
				m.truth.EarthClosed = true
			case "earth_open":
				m.truth.EarthClosed = false
			case "reset_fault_passage":
				m.truth.FaultPassage = false
			default:
				return reject(ActionUnknown)
			}
		case OLTC:
			switch action {
			case "set_local":
				m.truth.RemoteEnabled, m.truth.Armed = false, false
			case "set_remote":
				if !m.truth.RemoteEnabled && m.truth.Auto {
					m.truth.Armed = false
				}
				m.truth.RemoteEnabled = true
			case "block":
				m.truth.Blocked = true
			case "unblock":
				m.truth.Blocked = false
			default:
				return reject(ActionUnknown)
			}
		}
	}
	changed := action == "reset" || m.truth != before.Truth
	if changed {
		m.changedLocked()
	}
	detail := ""
	if !changed {
		detail = "no_op"
	}
	m.recordLocked(action, "accepted", detail, before, actor)
	return m.snapshotLocked(), nil
}
