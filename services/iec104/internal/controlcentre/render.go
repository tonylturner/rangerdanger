// SPDX-License-Identifier: Apache-2.0

package controlcentre

import (
	"encoding/json"

	iec "github.com/tonylturner/iec104go"
	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
)

func (c *Centre) pointJSON(i int) json.RawMessage {
	pp, p := c.pm.Points[i], c.points[i]
	var limits, reporting, ov, asdu, cot, invalid any
	if pp.HasLimits {
		limits = ordered("low", pp.LimitLow, "high", pp.LimitHigh)
	}
	if pp.Kind == pointmap.Float {
		reporting = ordered("cyclic_ms", pp.CyclicMS, "deadband", pp.Deadband)
	}
	if pp.Kind == pointmap.Float || pp.Kind == pointmap.StepPosition {
		ov = p.q&iec.Overflow != 0
	}
	if p.has {
		asdu = p.typ.String()
		cot = p.cot
	}
	if p.source != "" {
		invalid = p.invalid
	}
	return ordered("device", c.pm.Devices[pp.DeviceIndex].ID, "limits", limits, "id", pp.ID, "ioa", pp.IOA, "kind", pp.Kind, "asdu", asdu, "reporting", reporting, "value", p.value, "quality", ordered("iv", p.q&iec.Invalid != 0, "nt", p.q&iec.NotTopical != 0, "sb", p.q&iec.Substituted != 0, "bl", p.q&iec.Blocked != 0, "ov", ov), "stale_reason", nullable(p.stale), "source_time", nullable(p.source), "source_time_invalid", invalid, "received_at", nullable(p.received), "cot", cot)
}
func (c *Centre) stateJSON() json.RawMessage {
	c.snapshotSeq++
	pts := make([]json.RawMessage, len(c.points))
	for i := range pts {
		pts[i] = c.pointJSON(i)
	}
	next := ""
	if c.link.state == "started" {
		next = c.link.nextAt
	}
	return ordered("schema", 3, "snapshot_seq", c.snapshotSeq, "rendered_at", c.stamp(), "cache_epoch", c.epoch, "link", ordered("state", c.link.state, "peer", c.peer, "since", nullable(c.link.since), "last_rx_at", nullable(c.link.lastRx), "connection_seq", c.link.seq, "gi", ordered("state", c.link.gi, "started_at", nullable(c.link.started), "trigger", nullable(c.link.trigger), "last_complete_at", nullable(c.link.completed), "next_due_at", nullable(next))), "points", pts)
}
func (c *Centre) healthJSON() json.RawMessage {
	return ordered("status", "ok", "link", c.link.state, "historian", ordered("last_ok_at", nullable(c.lastOK), "failures", c.failures, "dropped_snapshots", c.dropped))
}
func (c *Centre) journalJSON(after uint64) json.RawMessage {
	entries := make([]json.RawMessage, 0, len(c.journal))
	var oldest, latest any
	for _, e := range c.journal {
		if e.seq > after {
			entries = append(entries, ordered("seq", e.seq, "at", e.at, "kind", e.kind, "detail", e.detail))
		}
	}
	if len(c.journal) > 0 {
		oldest = c.journal[0].seq
		latest = c.journalSeq
	}
	return ordered("cache_epoch", c.epoch, "entries", entries, "latest_seq", latest, "oldest_seq", oldest)
}
