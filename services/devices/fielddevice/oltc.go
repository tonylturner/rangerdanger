// SPDX-License-Identifier: Apache-2.0
package fielddevice

func (m *Model) energizedLocked() bool { return m.measurements.Valid && *m.measurements.SectionLive }

func (m *Model) writeOLTCLocked(address, value uint16, actor AuditContext, before Snapshot) CommandResult {
	if address > 2 {
		return CommandResult{Exception: 2, Reason: "invalid_address"}
	}
	if address == 1 {
		if value != 0 && (value < 92 || value > 108) {
			return CommandResult{Exception: 3, Reason: "invalid_value"}
		}
		return m.plcRequestLocked(value, actor, before)
	}
	if value != 1 && value != 2 {
		return CommandResult{Exception: 3, Reason: "invalid_value"}
	}
	operation := "manual_step"
	if address == 2 {
		operation = "mode"
	}
	reject := func(reason string) CommandResult {
		m.recordLocked(operation, "rejected", reason, before, actor)
		return CommandResult{Reason: reason}
	}
	if !m.truth.RemoteEnabled {
		return reject("not_remote")
	}
	if address == 2 {
		auto := value == 2
		if m.truth.Auto == auto {
			m.recordLocked(operation, "accepted", "no_op", before, actor)
			return CommandResult{Accepted: true}
		}
		m.truth.Auto = auto
		if auto {
			m.truth.Armed = false
		}
		m.changedLocked()
		m.recordLocked(operation, "accepted", "", before, actor)
		return CommandResult{Accepted: true, Changed: true}
	}
	if m.truth.Auto {
		return reject("not_manual")
	}
	if m.truth.Moving {
		return reject("moving")
	}
	if m.truth.Blocked || m.protection.PickedUp {
		return reject("blocked")
	}
	if !m.energizedLocked() {
		return reject("not_energized")
	}
	target := m.truth.Tap - 1
	if value == 2 {
		target = m.truth.Tap + 1
	}
	if target < -8 || target > 8 {
		return reject("limit")
	}
	m.startMotionLocked(target)
	m.recordLocked(operation, "accepted", "", before, actor)
	return CommandResult{Accepted: true, Changed: true}
}

func (m *Model) plcRequestLocked(value uint16, actor AuditContext, before Snapshot) CommandResult {
	m.lastRequest = value
	if value == 0 {
		m.requestResult = RequestNone
		if m.truth.RemoteEnabled && m.truth.Auto && !m.truth.Armed {
			m.truth.Armed = true
			m.changedLocked()
			// Arming is not a tap request; consumers count plc_request as motion.
			m.recordLocked("plc_arm", "accepted", "", before, actor)
		}
		return CommandResult{Accepted: true, Changed: m.truth.Revision != before.Revision}
	}
	target := int16(value) - 100
	if target == m.truth.Tap || (m.truth.Moving && target == m.truth.TargetTap) {
		m.requestResult = RequestNone
		return CommandResult{Accepted: true}
	}
	result := RequestAccepted
	switch {
	case !m.truth.RemoteEnabled:
		result = RequestNotRemote
	case !m.truth.Auto:
		result = RequestNotAuto
	case m.truth.Moving:
		result = RequestMoving
	case m.truth.Blocked || m.protection.PickedUp:
		result = RequestBlocked
	case !m.truth.Armed:
		result = RequestUnarmed
	case !m.energizedLocked():
		result = RequestNotEnergized
	case target-m.truth.Tap != 1 && target-m.truth.Tap != -1:
		result = RequestNotAdjacent
	}
	m.requestResult = result
	if result != RequestAccepted {
		if result != m.lastIgnored {
			m.recordLocked("plc_request", "ignored", result.String(), before, actor)
		}
		m.lastIgnored = result
		return CommandResult{Accepted: true, Reason: result.String()}
	}
	m.lastIgnored = RequestNone
	m.startMotionLocked(target)
	m.recordLocked("plc_request", "accepted", "", before, actor)
	return CommandResult{Accepted: true, Changed: true}
}

func (m *Model) startMotionLocked(target int16) {
	m.truth.TargetTap, m.truth.Moving = target, true
	m.changedLocked()
	m.motionToken++
	token := m.motionToken
	m.motion = m.clock.AfterFunc(MotionDuration, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if token != m.motionToken || !m.truth.Moving {
			return
		}
		m.expireLocked(m.clock.Now())
		before := m.snapshotLocked()
		m.truth.Tap, m.truth.Moving = m.truth.TargetTap, false
		m.motion = nil
		m.changedLocked()
		m.recordLocked("motion_complete", "accepted", "", before, AuditContext{Peer: "actuator", Protocol: "local", Zone: m.localContext.Zone})
	})
}
