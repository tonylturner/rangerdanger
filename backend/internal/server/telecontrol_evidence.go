package server

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
)

// Evaluation of the eu-iec104-telecontrol validator: pure functions over
// the evidence telecontrol_validator.go collects. The validator passes only
// on real, temporally ordered evidence that the student ran the stale-view
// exercise: a logged deny of a foreign IEC 104 session, a control-centre
// outage with a stale position point, a local protection trip and the
// solver's response inside that outage, the recovery GI, and an operator
// view that agrees with the device again.

const (
	// telecontrolPolicy is the EU package policy the exercise ends on.
	telecontrolPolicy = "improved"
	// iec104Port is the IANA port of IEC 60870-5-104.
	iec104Port = 2404
	// positionPoint is the CC point of the breaker position.
	positionPoint = "feeder-breaker-position"
)

// telecontrolPoints is deliberately only the lab-1 breaker subset
// (IOAs 1001, 1002, 2001, 2002). RMU, OLTC and PLC quality does not
// change the evidence required by this exercise.
var telecontrolPoints = []string{positionPoint, "feeder-protection-trip", "feeder-current", "feeder-voltage"}

// telecontrolEvidence is everything the validator reads. Each source keeps
// its fetch error; a source that failed is missing evidence, never empty.
type telecontrolEvidence struct {
	activePolicy string
	// policyErr is non-nil unless containd's running firewall is the
	// telecontrol policy file.
	policyErr   error
	rangeStart  time.Time
	collectedAt time.Time
	rtuIP       string // the RTU's teaching address
	ccIP        string // the CC's teaching address

	events    []containd.Event
	eventsErr error

	ccState    *ccState
	ccStateErr error
	journal    *ccJournal
	journalErr error

	device    *deviceTruth
	deviceErr error
	audit     *deviceAudit
	auditErr  error
}

// ccState is the part of the CC's GET /api/state the validator reads.
type ccState struct {
	Schema     int       `json:"schema"`
	CacheEpoch string    `json:"cache_epoch"`
	Points     []ccPoint `json:"points"`
}

type ccPoint struct {
	ID      string          `json:"id"`
	Value   json.RawMessage `json:"value"`
	Quality struct {
		IV bool  `json:"iv"`
		NT bool  `json:"nt"`
		SB bool  `json:"sb"`
		BL bool  `json:"bl"`
		OV *bool `json:"ov"`
	} `json:"quality"`
	StaleReason *string   `json:"stale_reason"`
	ReceivedAt  time.Time `json:"received_at"`
}

// ccJournal is the CC's GET /api/journal.
type ccJournal struct {
	CacheEpoch string         `json:"cache_epoch"`
	Entries    []journalEntry `json:"entries"`
	LatestSeq  uint64         `json:"latest_seq"`
	OldestSeq  uint64         `json:"oldest_seq"`
}

type journalEntry struct {
	Seq    uint64    `json:"seq"`
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Detail struct {
		ConnectionSeq *uint64 `json:"connection_seq"`
		Point         string  `json:"point"`
	} `json:"detail"`
}

// deviceTruth is the part of the device's GET /api/state the validator reads.
type deviceTruth struct {
	Schema     int    `json:"schema"`
	Generation uint32 `json:"generation"`
	Position   string `json:"position"`
}

// deviceAudit is the device's GET /api/audit.
type deviceAudit struct {
	Generation uint32       `json:"generation"`
	Entries    []auditEntry `json:"entries"`
}

type auditEntry struct {
	Seq       uint64    `json:"seq"`
	At        time.Time `json:"at"`
	Operation string    `json:"operation"`
	Result    string    `json:"result"`
	After     struct {
		Revision uint64 `json:"revision"`
	} `json:"after"`
	Context struct {
		Protocol string `json:"protocol"`
	} `json:"context"`
	Detail json.RawMessage `json:"detail"`
}

// outageWindow is one CC outage in the journal: the link_down L, the stale
// position point after it, and the GI G that completed on a later
// connection.
type outageWindow struct {
	down, stale, up, gi journalEntry
}

func (w outageWindow) String() string {
	return fmt.Sprintf("link_down seq %d at %s to gi_complete seq %d at %s",
		w.down.Seq, w.down.At.Format(time.RFC3339Nano), w.gi.Seq, w.gi.At.Format(time.RFC3339Nano))
}

func checkPass(name, detail string) ValidationCheck { return ValidationCheck{name, "pass", detail} }
func checkFail(name, detail string) ValidationCheck { return ValidationCheck{name, "fail", detail} }

func insideEvidenceWindow(at, start, end time.Time) bool {
	return !at.IsZero() && !at.Before(start) && !at.After(end)
}

const (
	checkDeny     = "Source-pinned boundary and logged foreign IEC 104 denial"
	checkOutage   = "Control-centre outage and recovery in the journal"
	checkTrip     = "Local trip and solver response during the outage"
	checkOperator = "Operator view agrees with the device"
)

// evaluateTelecontrol runs every check over the evidence.
func evaluateTelecontrol(ev telecontrolEvidence) []ValidationCheck {
	windows, outage := outageCheck(ev)
	return []ValidationCheck{denyCheck(ev), outage, tripCheck(ev, windows), operatorCheck(ev, windows)}
}

func denyCheck(ev telecontrolEvidence) ValidationCheck {
	// A policy hash alone is not a protocol proof. Grade it together with
	// the real denied foreign attempt, rather than as a separate green check.
	if ev.activePolicy != telecontrolPolicy {
		return checkFail(checkDeny, fmt.Sprintf("missing evidence: the active policy is %q, not the package's %s policy", ev.activePolicy, telecontrolPolicy))
	}
	if ev.policyErr != nil {
		return checkFail(checkDeny, "missing evidence: containd's running firewall is not the improved policy file: "+ev.policyErr.Error())
	}
	if ev.eventsErr != nil {
		return checkFail(checkDeny, "missing evidence: firewall events unavailable: "+ev.eventsErr.Error())
	}
	for _, event := range ev.events {
		action, _ := event.Attributes["action"].(string)
		if event.Kind == "firewall.rule.hit" && action == "DENY" && event.Dest == ev.rtuIP && event.DstPort == iec104Port &&
			event.Source != "" && event.Source != ev.ccIP && event.Transport == "tcp" &&
			insideEvidenceWindow(event.Timestamp, ev.rangeStart, ev.collectedAt) {
			return checkPass(checkDeny, fmt.Sprintf("containd denied %s → %s:%d at %s", event.Source, ev.rtuIP, iec104Port, event.Timestamp.Format(time.RFC3339Nano)))
		}
	}
	return checkFail(checkDeny, fmt.Sprintf("missing evidence: no firewall.rule.hit DENY to %s:%d from a source other than the control centre %s since the range started at %s",
		ev.rtuIP, iec104Port, ev.ccIP, ev.rangeStart.Format(time.RFC3339Nano)))
}

// outageCheck finds every complete outage window in the CC journal.
func outageCheck(ev telecontrolEvidence) ([]outageWindow, ValidationCheck) {
	switch {
	case ev.journalErr != nil:
		return nil, checkFail(checkOutage, "missing evidence: CC journal unavailable: "+ev.journalErr.Error())
	case ev.ccStateErr != nil:
		return nil, checkFail(checkOutage, "missing evidence: CC state unavailable to confirm the journal's cache epoch: "+ev.ccStateErr.Error())
	case ev.journal == nil || ev.ccState == nil:
		return nil, checkFail(checkOutage, "missing evidence: CC journal or state is absent")
	case ev.ccState.Schema != 2:
		return nil, checkFail(checkOutage, "missing evidence: CC state is not schema 2")
	case ev.journal.CacheEpoch == "" || ev.journal.CacheEpoch != ev.ccState.CacheEpoch:
		return nil, checkFail(checkOutage, fmt.Sprintf("missing evidence: the CC cache epoch changed (journal %q, state %q): the control centre restarted and its journal no longer covers the exercise",
			ev.journal.CacheEpoch, ev.ccState.CacheEpoch))
	}
	entries := append([]journalEntry(nil), ev.journal.Entries...)
	sort.Slice(entries, func(i, j int) bool { return entries[i].Seq < entries[j].Seq })

	var windows []outageWindow
	progress := "no link_down entry"
	for i, down := range entries {
		if down.Kind != "link_down" || down.Detail.ConnectionSeq == nil {
			continue
		}
		window, missing := outageAfter(down, entries[i+1:])
		if missing != "" {
			if len(windows) == 0 {
				progress = fmt.Sprintf("link_down seq %d has %s", down.Seq, missing)
			}
			continue
		}
		if !insideEvidenceWindow(window.down.At, ev.rangeStart, ev.collectedAt) ||
			!insideEvidenceWindow(window.stale.At, window.down.At, window.up.At) ||
			!insideEvidenceWindow(window.up.At, window.stale.At, window.gi.At) ||
			!insideEvidenceWindow(window.gi.At, window.up.At, ev.collectedAt) {
			progress = "outage is outside this range attempt or has nonchronological timestamps"
			continue
		}
		windows = append(windows, window)
	}
	if len(windows) == 0 {
		return nil, checkFail(checkOutage, "missing evidence: "+progress)
	}
	// The window must be retained whole: nothing before its link_down may
	// have been overwritten in the ring.
	latest := windows[len(windows)-1]
	if ev.journal.OldestSeq > latest.down.Seq {
		return nil, checkFail(checkOutage, fmt.Sprintf("missing evidence: the CC journal is truncated: its oldest entry is seq %d, after the link_down seq %d", ev.journal.OldestSeq, latest.down.Seq))
	}
	return []outageWindow{latest}, checkPass(checkOutage, fmt.Sprintf("%s: %s went stale, connection %d came up and completed its GI",
		latest, positionPoint, *latest.gi.Detail.ConnectionSeq))
}

// outageAfter completes the window that starts at down from the entries
// after it, or names what is missing: the position point going stale, then
// the first GI completed on a later connection, whose link_up lies between
// down and that GI.
func outageAfter(down journalEntry, later []journalEntry) (outageWindow, string) {
	window := outageWindow{down: down}
	staleAt := -1
	for i, entry := range later {
		if entry.Kind == "point_stale" && entry.Detail.Point == positionPoint {
			window.stale, staleAt = entry, i
			break
		}
	}
	if staleAt < 0 {
		return window, "no point_stale for " + positionPoint + " after it"
	}
	giAt := -1
	for i, entry := range later[staleAt+1:] {
		if entry.Kind == "gi_complete" && laterConnection(entry, down) {
			window.gi, giAt = entry, staleAt+1+i
			break
		}
	}
	if giAt < 0 {
		return window, "no later gi_complete on a connection with a greater connection_seq"
	}
	for _, entry := range later[:giAt] {
		if entry.Kind == "link_up" && entry.Detail.ConnectionSeq != nil && *entry.Detail.ConnectionSeq == *window.gi.Detail.ConnectionSeq {
			window.up = entry
			return window, ""
		}
	}
	return window, fmt.Sprintf("no link_up for connection %d before its gi_complete", *window.gi.Detail.ConnectionSeq)
}

func laterConnection(entry, down journalEntry) bool {
	return entry.Detail.ConnectionSeq != nil && *entry.Detail.ConnectionSeq > *down.Detail.ConnectionSeq
}

// tripCheck needs a local protection trip T inside an outage window
// (L.at < T.at < G.at) and the solver's first solve for T's revision after
// it and before the GI.
func tripCheck(ev telecontrolEvidence, windows []outageWindow) ValidationCheck {
	switch {
	case ev.auditErr != nil:
		return checkFail(checkTrip, "missing evidence: device audit unavailable: "+ev.auditErr.Error())
	case ev.deviceErr != nil:
		return checkFail(checkTrip, "missing evidence: device state unavailable to confirm the audit's generation: "+ev.deviceErr.Error())
	case ev.audit == nil || ev.device == nil:
		return checkFail(checkTrip, "missing evidence: device audit or state is absent")
	case ev.device.Schema != 2:
		return checkFail(checkTrip, "missing evidence: device state is not schema 2")
	case ev.audit.Generation != ev.device.Generation:
		return checkFail(checkTrip, fmt.Sprintf("missing evidence: the device generation changed (audit %d, state %d): the device restarted", ev.audit.Generation, ev.device.Generation))
	case len(windows) == 0:
		return checkFail(checkTrip, "missing evidence: no control-centre outage window to place a trip in (see the journal check)")
	}
	var trips []auditEntry
	for _, entry := range ev.audit.Entries {
		if entry.Operation == "local_protection_trip" && entry.Context.Protocol == "local-protection" && entry.Result == "accepted" {
			trips = append(trips, entry)
		}
	}
	if len(trips) == 0 {
		return checkFail(checkTrip, "missing evidence: the device audit has no local-protection trip")
	}
	inside := false
	for _, window := range windows {
		for _, trip := range trips {
			if !(window.down.At.Before(trip.At) && trip.At.Before(window.gi.At)) {
				continue
			}
			inside = true
			if solve, ok := firstSolveBetween(ev.audit.Entries, trip.After.Revision, trip.At, window.gi.At); ok {
				return checkPass(checkTrip, fmt.Sprintf("local protection tripped at %s (revision %d) and the solver's first solve for it came at %s, inside %s",
					trip.At.Format(time.RFC3339Nano), trip.After.Revision, solve.At.Format(time.RFC3339Nano), window))
			}
		}
	}
	if !inside {
		return checkFail(checkTrip, "missing evidence: no local-protection trip between a link_down and its recovery GI")
	}
	return checkFail(checkTrip, "missing evidence: no first_solve for the tripped revision between the trip and the recovery GI")
}

// firstSolveBetween finds the first_solve audit of revision strictly
// between after and before.
func firstSolveBetween(entries []auditEntry, revision uint64, after, before time.Time) (auditEntry, bool) {
	for _, entry := range entries {
		if entry.Operation != "first_solve" || entry.Result != "accepted" {
			continue
		}
		var detail struct {
			Revision *uint64 `json:"revision"`
		}
		if json.Unmarshal(entry.Detail, &detail) != nil || detail.Revision == nil || *detail.Revision != revision {
			continue
		}
		if after.Before(entry.At) && entry.At.Before(before) {
			return entry, true
		}
	}
	return auditEntry{}, false
}

// operatorCheck needs the four points GOOD now and the operator's breaker
// position equal to the device's.
func operatorCheck(ev telecontrolEvidence, windows []outageWindow) ValidationCheck {
	switch {
	case ev.ccStateErr != nil:
		return checkFail(checkOperator, "missing evidence: CC state unavailable: "+ev.ccStateErr.Error())
	case ev.deviceErr != nil:
		return checkFail(checkOperator, "missing evidence: device state unavailable: "+ev.deviceErr.Error())
	case ev.ccState == nil || ev.device == nil:
		return checkFail(checkOperator, "missing evidence: CC or device state is absent")
	case ev.ccState.Schema != 2 || ev.device.Schema != 2:
		return checkFail(checkOperator, "missing evidence: CC/device state must use schema 2")
	case len(windows) == 0:
		return checkFail(checkOperator, "missing evidence: no control-centre outage window for recovery observations")
	}
	recoveredAt := windows[len(windows)-1].up.At
	points := map[string]ccPoint{}
	for _, point := range ev.ccState.Points {
		points[point.ID] = point
	}
	var problems []string
	for _, id := range telecontrolPoints {
		point, ok := points[id]
		if !ok {
			problems = append(problems, id+" is missing")
			continue
		}
		if flags := badQuality(point); flags != "" {
			problems = append(problems, id+" is not GOOD ("+flags+")")
		}
		if len(point.Value) == 0 || string(point.Value) == "null" {
			problems = append(problems, id+" has no observed value")
		}
		if !insideEvidenceWindow(point.ReceivedAt, recoveredAt, ev.collectedAt) {
			problems = append(problems, id+" has no protocol receipt inside the recovery attempt")
		}
	}
	if len(problems) > 0 {
		return checkFail(checkOperator, "missing evidence: "+strings.Join(problems, "; "))
	}
	var position string
	if err := json.Unmarshal(points[positionPoint].Value, &position); err != nil || position != ev.device.Position {
		return checkFail(checkOperator, fmt.Sprintf("missing evidence: the operator's breaker position %s differs from the device's %q", points[positionPoint].Value, ev.device.Position))
	}
	return checkPass(checkOperator, fmt.Sprintf("all four points are GOOD and the operator's breaker position %q matches the device", position))
}

// badQuality lists what keeps a point from being GOOD, or "".
func badQuality(point ccPoint) string {
	var flags []string
	for _, flag := range []struct {
		set  bool
		name string
	}{{point.Quality.IV, "IV"}, {point.Quality.NT, "NT"}, {point.Quality.SB, "SB"}, {point.Quality.BL, "BL"}, {point.Quality.OV != nil && *point.Quality.OV, "OV"}} {
		if flag.set {
			flags = append(flags, flag.name)
		}
	}
	if point.StaleReason != nil {
		flags = append(flags, "stale_reason "+*point.StaleReason)
	}
	return strings.Join(flags, ", ")
}
