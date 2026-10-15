package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

// Lab 3 reads switch/tap snapshots without extending lab 1's breaker-only
// observation subset. Both views use the shared EU journal/audit readers.
type euLab3Snapshot struct {
	Generation   uint32 `json:"generation"`
	Revision     uint64 `json:"revision"`
	Position     string `json:"position"`
	FaultPassage bool   `json:"fault_passage"`
	Tap          int    `json:"tap"`
	TargetTap    int    `json:"target_tap"`
	Moving       bool   `json:"moving"`
	Auto         bool   `json:"auto"`
}

type euLab3AuditEntry = euAuditEntry[euLab3Snapshot, euLab3Snapshot, peerAuditContext]
type euLab3Audit = euAudit[euLab3AuditEntry]

type euLab3Command struct {
	Point      string    `json:"point"`
	Operation  string    `json:"operation"`
	Result     string    `json:"result"`
	SelectedAt time.Time `json:"selected_at"`
	ExecutedAt time.Time `json:"executed_at"`
	ConAt      time.Time `json:"con_at"`
	TermAt     time.Time `json:"term_at"`
	Feedback   *struct {
		IOA   int             `json:"ioa"`
		Value json.RawMessage `json:"value"`
		At    time.Time       `json:"at"`
	} `json:"feedback"`
}

type euLab3Journal = euJournal[json.RawMessage]
type euLab3JournalEntry = euJournalEntry[json.RawMessage]

type euLab3Evidence struct {
	start, end time.Time
	epoch      string
	journal    *euLab3Journal
	audits     map[string]*euLab3Audit
	events     []containd.Event
	err        error
}

// collectEULab3 reads only observation surfaces. A missing upstream or
// changed boot identity invalidates the collection, not just one value.
func collectEULab3(ctx context.Context, gen *lifecycle.Generation, devices ...string) euLab3Evidence {
	ev := euLab3Evidence{end: time.Now().UTC(), audits: map[string]*euLab3Audit{}}
	if gen == nil {
		ev.err = fmt.Errorf("no active range generation")
		return ev
	}
	ev.start = gen.StartedAt
	cc, ok := controlCentreURL(gen)
	if !ok {
		ev.err = fmt.Errorf("no control centre")
		return ev
	}
	ev.journal, ev.err = fetchJSON[euLab3Journal](ctx, controlCentreService, cc+"/api/journal?after=0")
	if ev.err != nil {
		return ev
	}
	state, err := fetchJSON[ccState](ctx, controlCentreService, cc+"/api/state")
	if err != nil {
		ev.err = err
		return ev
	}
	ev.epoch = state.CacheEpoch
	if ev.epoch == "" || ev.epoch != ev.journal.CacheEpoch {
		ev.err = fmt.Errorf("CC epoch changed or absent")
		return ev
	}
	for _, id := range devices {
		url, ok := nodeSimURL(gen, id)
		if !ok {
			ev.err = fmt.Errorf("no sim for %s", id)
			return ev
		}
		audit, err := fetchJSON[euLab3Audit](ctx, simService, url+"/api/audit?after=0")
		if err != nil {
			ev.err = err
			return ev
		}
		state, err := fetchJSON[euLab3Snapshot](ctx, simService, url+"/api/state")
		if err != nil {
			ev.err = err
			return ev
		}
		if audit.Generation == 0 || audit.Generation != state.Generation {
			ev.err = fmt.Errorf("%s generation changed or absent", id)
			return ev
		}
		ev.audits[id] = audit
		for _, entry := range audit.Entries {
			if entry.Operation == "reset" && entry.Result == "accepted" && entry.Context.Protocol == "lab" &&
				insideEvidenceWindow(entry.At, ev.start, ev.end) {
				ev.start = entry.At
			}
		}
	}
	// Upper bound is the start of collection: later activity cannot sneak
	// into a partially read evidence set.
	ev.events, ev.err = gen.Containd().GetEvents(ctx, containd.MaxEventLimit, ev.start)
	return ev
}

func (ev euLab3Evidence) commands() []euLab3Command {
	var commands []euLab3Command
	if ev.journal == nil {
		return commands
	}
	for _, entry := range ev.journal.Entries {
		if entry.Kind != "command" || !insideEvidenceWindow(entry.At, ev.start, ev.end) {
			continue
		}
		var cmd euLab3Command
		if json.Unmarshal(entry.Detail, &cmd) != nil {
			continue
		}
		if !insideEvidenceWindow(cmd.SelectedAt, ev.start, entry.At) ||
			!insideEvidenceWindow(cmd.ExecutedAt, cmd.SelectedAt, entry.At) {
			continue
		}
		commands = append(commands, cmd)
	}
	return commands
}

// commandAudit proves the Modbus operation happened during this command's
// execute/confirmation window. Current switch position is not evidence.
func (ev euLab3Evidence) commandAudit(device, point, operation, result, reason string, ioa int) bool {
	audit := ev.audits[device]
	if audit == nil {
		return false
	}
	for _, cmd := range ev.commands() {
		if cmd.Point != point || cmd.Operation != operation || cmd.Result != result {
			continue
		}
		if ev.matchesCommand(audit, cmd, reason, ioa) {
			return true
		}
	}
	return false
}

func (ev euLab3Evidence) matchesCommand(audit *euLab3Audit, cmd euLab3Command, reason string, ioa int) bool {
	result, operation := cmd.Result, cmd.Operation
	end := cmd.ConAt
	if !insideEvidenceWindow(end, cmd.ExecutedAt, ev.end) {
		return false
	}
	if result == "completed" {
		if !insideEvidenceWindow(cmd.TermAt, end, ev.end) || cmd.Feedback == nil ||
			cmd.Feedback.IOA != ioa || !insideEvidenceWindow(cmd.Feedback.At, end, cmd.TermAt) {
			return false
		}
		var value string
		if json.Unmarshal(cmd.Feedback.Value, &value) != nil || value != map[string]string{"open": "open", "close": "closed"}[operation] {
			return false
		}
	}
	for _, entry := range audit.Entries {
		peer, _, _ := net.SplitHostPort(entry.Context.Peer)
		if entry.Context.Protocol != "modbus" || !insideEvidenceWindow(entry.At, cmd.ExecutedAt, end) ||
			peer != "10.30.30.20" || entry.Operation != operation {
			continue
		}
		if result == "completed" && entry.Result == "accepted" && entry.Before.Position != entry.After.Position &&
			entry.After.Position == map[string]string{"open": "open", "close": "closed"}[operation] {
			return true
		}
		var why string
		if result == "rejected" && entry.Result == "rejected" &&
			json.Unmarshal(entry.Detail, &why) == nil && why == reason {
			return true
		}
	}
	return false
}

// vendorPair is deliberately a narrow claim: an accepted vendor Modbus
// operation followed by a logged TCP/502 deny from that same source. A
// SYN deny cannot attest which payload the student intended to send.
func (ev euLab3Evidence) vendorPair(device, destination, operation string) bool {
	audit := ev.audits[device]
	if audit == nil {
		return false
	}
	for _, entry := range audit.Entries {
		peer, _, err := net.SplitHostPort(entry.Context.Peer)
		if err != nil || peer != "10.20.20.10" || entry.Context.Protocol != "modbus" ||
			entry.Operation != operation || entry.Result != "accepted" ||
			!insideEvidenceWindow(entry.At, ev.start, ev.end) {
			continue
		}
		for _, event := range ev.events {
			action, _ := event.Attributes["action"].(string)
			if event.Kind == "firewall.rule.hit" && action == "DENY" && event.Source == peer &&
				event.Dest == destination && event.DstPort == 502 &&
				event.Timestamp.After(entry.At) && !event.Timestamp.After(ev.end) {
				return true
			}
		}
	}
	return false
}

func euLab3Check(name string, ok bool, detail string) ValidationCheck {
	if ok {
		return checkPass(name, detail)
	}
	return checkFail(name, "missing attempt-window evidence: "+detail)
}
