package server

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

// Phase 2 registers these values under their slot IDs, atomically with the
// curriculum replacement. No startup registration happens in this file.
var (
	euBaselineValidator       = euWorkshopValidator("eu-baseline-assessment")
	euImplementationValidator = euWorkshopValidator("eu-firewall-implementation")
	euHardeningValidator      = euWorkshopValidator("eu-hardening-configurations")
	euVendorValidator         = euWorkshopValidator("eu-vendor-rdp-compromise")
	euValidationValidator     = euWorkshopValidator("eu-validation-evidence")
)

func euWorkshopValidator(slot string) scenarioValidator {
	return scenarioValidator{
		capabilities: []string{labs.CapabilityPolicyContaind, labs.CapabilityProtocolIEC104, labs.CapabilityProcessTruth},
		run: func(ctx context.Context, s *Server, in validatorInput) []ValidationCheck {
			return evaluateEUWorkshop(slot, s.collectEUWorkshop(ctx, in.gen, slot))
		},
	}
}

type euWorkshopEvidence struct {
	start, now, policyAt, hardeningSince, operationEnd time.Time
	policy                                             string
	policyErr                                          error
	capture                                            bool
	cc                                                 *conduitState
	journal                                            *ccJournal
	ccErr                                              error
	process                                            *euWorkshopProcess
	processErr                                         error
	devices                                            map[string]*euWorkshopDevice
	deviceErrors                                       map[string]error
	events                                             []containd.Event
	eventsErr                                          error
	denies                                             []conduitDeny
	identityErr                                        error
	conduits                                           conduitEvidence
	breakerAudit                                       *euWorkshopBreakerAudit
	auditErr                                           error
	rtuLog                                             string
	logErr                                             error
	ccIP, rtuIP                                        string
}

func (s *Server) collectEUWorkshop(ctx context.Context, gen *lifecycle.Generation, slot string) euWorkshopEvidence {
	ev := euWorkshopEvidence{start: gen.StartedAt, devices: map[string]*euWorkshopDevice{}, deviceErrors: map[string]error{}}
	s.activeConfigMu.RLock()
	ev.policy, ev.policyAt, ev.hardeningSince = s.activeConfig, s.policyChangedAt, s.policyEvidenceSince
	wantHash := s.lastAppliedHash
	s.activeConfigMu.RUnlock()
	hash, err := gen.Containd().GetFirewallHash(ctx)
	ev.policyErr = err
	if err == nil && (wantHash == "" || hash != wantHash) {
		ev.policyErr = fmt.Errorf("running policy differs from recorded policy; wait for the observer or reapply")
	}
	// Verify weak/non-weak against the document, not a label. A failed
	// comparison is missing evidence, never evidence of a custom policy.
	weakPath, weakErr := s.policyPath(gen.Package, "weak")
	if weakErr == nil {
		var data []byte
		data, weakErr = readPolicyJSONWithRetry(weakPath)
		if weakErr == nil {
			var weakHash string
			weakHash, weakErr = containd.FirewallHashFromBytes(data)
			if weakErr == nil && ((ev.policy == "weak") != (hash == weakHash)) {
				weakErr = fmt.Errorf("weak/non-weak policy identity mismatch")
			}
		}
	}
	if weakErr != nil {
		ev.policyErr = weakErr
	}
	if slot == "eu-baseline-assessment" {
		ev.capture = s.checkPcapFileExists(ctx, gen)
	}
	if slot == "eu-firewall-implementation" {
		ev.conduits = s.collectConduits(ctx, gen)
		ev.denies, ev.identityErr = euWorkshopDenies(gen, "")
		ev.events, ev.eventsErr = ev.conduits.events, ev.conduits.eventsErr
	} else {
		nodes := []string{"breaker-1"}
		if slot == "eu-vendor-rdp-compromise" || slot == "eu-validation-evidence" {
			nodes = append(nodes, "rmu-1", "rmu-2", "oltc-1")
		}
		for _, node := range nodes {
			ev.devices[node], ev.deviceErrors[node] = euDevice(ctx, gen, node)
		}
		ev.process, ev.processErr = euProcess(ctx, gen)
		if slot == "eu-baseline-assessment" || slot == "eu-validation-evidence" {
			ev.cc, ev.journal, ev.ccErr = euCC(ctx, gen)
		}
	}
	if slot == "eu-hardening-configurations" || slot == "eu-vendor-rdp-compromise" {
		ev.denies, ev.identityErr = euWorkshopDenies(gen, slot)
		ev.events, ev.eventsErr = gen.Containd().GetEvents(ctx, containd.MaxEventLimit, gen.StartedAt)
	}
	if slot == "eu-hardening-configurations" {
		// Read both streams after this cutoff; later activity cannot sneak
		// into a partially collected absence proof.
		ev.operationEnd = time.Now().UTC()
		ev.ccIP, err = roleTeachingIP(gen, "control-centre")
		if err != nil {
			ev.identityErr = err
		}
		ev.rtuIP, err = nodeIP(gen, "rtu-1", "station_net")
		if err != nil {
			ev.identityErr = err
		}
		endpoint, ok := nodeSimURL(gen, "breaker-1")
		if !ok {
			ev.auditErr = fmt.Errorf("no breaker endpoint")
		} else {
			ev.breakerAudit, ev.auditErr = fetchJSON[euWorkshopBreakerAudit](ctx, simService, endpoint+"/api/audit?after=0")
		}
		ev.rtuLog, ev.logErr = s.euRTULog(ctx, gen)
	}
	finalHash, finalErr := gen.Containd().GetFirewallHash(ctx)
	if finalErr != nil || finalHash != hash {
		ev.policyErr = fmt.Errorf("running policy changed or became unavailable during collection: %v", finalErr)
	}
	ev.now = time.Now().UTC()
	s.activeConfigMu.RLock()
	if s.lastAppliedHash != wantHash || !s.policyChangedAt.Equal(ev.policyAt) {
		ev.policyErr = fmt.Errorf("policy changed during evidence collection; repeat validation")
	}
	s.activeConfigMu.RUnlock()
	return ev
}

func euWorkshopDenies(gen *lifecycle.Generation, slot string) ([]conduitDeny, error) {
	rows := []conduitDeny{}
	for _, spec := range euRecipe.validation {
		if spec.expected != "deny" {
			continue
		}
		if slot == "eu-hardening-configurations" && !(spec.toNode == "rtu-1" && spec.fromNode == "kali-1") {
			continue
		}
		if slot == "eu-vendor-rdp-compromise" && !(spec.toNode == "vendor-jump-1" || spec.fromNode == "vendor-jump-1" && spec.network == "field_net") {
			continue
		}
		network := "vendor_net"
		if spec.fromNode == "kali-1" {
			network = "enterprise_net"
		}
		source, err := nodeIP(gen, spec.fromNode, network)
		if err != nil {
			return nil, err
		}
		dest, err := nodeIP(gen, spec.toNode, spec.network)
		if err != nil {
			return nil, err
		}
		port, err := strconv.Atoi(spec.port)
		if err != nil {
			return nil, err
		}
		rows = append(rows, conduitDeny{source, dest, port})
	}
	return rows, nil
}

func euWorkshopPolicy(ev euWorkshopEvidence, weak bool) ValidationCheck {
	ok := ev.policyErr == nil && ev.policy == "weak"
	detail := "running weak baseline"
	if !weak {
		ok = ev.policyErr == nil && (ev.policy == "improved" || ev.policy == "custom") && insideEvidenceWindow(ev.policyAt, ev.start, ev.now)
		detail = "recorded non-weak running policy; correctness is established only by the checks below"
	}
	if !ok {
		return checkFail("Running policy identity", fmt.Sprintf("active %q; changed %s; %v", ev.policy, ev.policyAt.Format(time.RFC3339Nano), ev.policyErr))
	}
	return checkPass("Running policy identity", detail)
}

func euWorkshopDeviceCheck(ev euWorkshopEvidence, node, name string, test func(*euWorkshopDevice) bool) ValidationCheck {
	state := ev.devices[node]
	if ev.deviceErrors[node] != nil || state == nil {
		return checkFail(name, fmt.Sprintf("missing device truth: %v", ev.deviceErrors[node]))
	}
	if !test(state) {
		return checkFail(name, "device is not in the required restored state")
	}
	return checkPass(name, "current schema-2 device truth (not wire-attack evidence)")
}

func euWorkshopSupply(ev euWorkshopEvidence) ValidationCheck {
	present, err := euSupply(ev.process, ev.now)
	if ev.processErr != nil || err != nil || !present {
		return checkFail("Feeder supply present", fmt.Sprintf("fresh supply on bus20/S1/S2/S3 not established: %v; %v", ev.processErr, err))
	}
	return checkPass("Feeder supply present", "fresh process solve ≤3 s: bus20/S1/S2/S3 live with positive voltage")
}

// Require the latest retained attempt per tuple, not any convenient historical
// deny. This checks L4 refusal only: denied SYNs disclose no command payload.
func euWorkshopNegative(ev euWorkshopEvidence, start time.Time) ValidationCheck {
	const name = "Latest negative TCP matrix denied"
	if start.IsZero() {
		return checkFail(name, "missing current policy/connection attempt")
	}
	if start.Before(ev.now.Add(-conduitAttemptLimit)) {
		start = ev.now.Add(-conduitAttemptLimit)
	}
	if ev.eventsErr != nil || ev.identityErr != nil || len(ev.denies) == 0 || !insideEvidenceWindow(start, ev.start, ev.now) {
		return checkFail(name, fmt.Sprintf("missing window, events or matrix identities: %v; %v", ev.eventsErr, ev.identityErr))
	}
	for _, row := range ev.denies {
		var latest *containd.Event
		ambiguous := false
		for i := range ev.events {
			event := &ev.events[i]
			if event.Kind != "firewall.rule.hit" || event.Transport != "tcp" || event.Source != row.source || event.Dest != row.dest || event.DstPort != row.port || !insideEvidenceWindow(event.Timestamp, start, ev.now) {
				continue
			}
			action, _ := event.Attributes["action"].(string)
			if latest == nil || event.Timestamp.After(latest.Timestamp) {
				latest, ambiguous = event, false
			} else if event.Timestamp.Equal(latest.Timestamp) {
				previous, _ := latest.Attributes["action"].(string)
				if action != previous {
					ambiguous = true
				}
			}
		}
		if latest == nil || latest.Attributes["action"] != "DENY" || ambiguous {
			return checkFail(name, fmt.Sprintf("no unambiguous latest TCP DENY within ten minutes for %s→%s:%d", row.source, row.dest, row.port))
		}
	}
	return checkPass(name, fmt.Sprintf("latest retained attempts for all %d tuples are logged TCP DENY after policy change and within ten minutes; no denied payload decoded", len(ev.denies)))
}

func evaluateEUWorkshop(slot string, ev euWorkshopEvidence) []ValidationCheck {
	checks := []ValidationCheck{euWorkshopPolicy(ev, slot == "eu-baseline-assessment")}
	breaker := func() ValidationCheck {
		return euWorkshopDeviceCheck(ev, "breaker-1", "Breaker closed", func(d *euWorkshopDevice) bool { return d.Position == "closed" })
	}
	link := func() ValidationCheck {
		if ev.ccErr != nil {
			return checkFail("Fresh CC link, GI and station observations", ev.ccErr.Error())
		}
		return euLink(ev.cc, ev.journal, ev.start, ev.now)
	}
	switch slot {
	case "eu-baseline-assessment":
		checks = append(checks, euLab3Check("Capture exists", ev.capture, "a capture file exists; contents and exercise completion are not certified"), link(), breaker(), euWorkshopSupply(ev))
	case "eu-firewall-implementation":
		start, window := conduitWindow(ev.conduits)
		if !start.IsZero() && ev.policyAt.After(start) {
			start = ev.policyAt
		}
		checks = append(checks, window, conduitPolls(ev.conduits, start), conduitPLC(ev.conduits, start), conduitFeed(ev.conduits, start))
		for _, read := range ev.conduits.reads[:min(2, len(ev.conduits.reads))] {
			if start.IsZero() || !insideEvidenceWindow(ev.conduits.readStart, start, ev.conduits.collectedAt) || !insideEvidenceWindow(ev.conduits.readEnd, ev.conduits.readStart, ev.conduits.collectedAt) {
				read = checkFail(read.Name, "teaching-source read is outside the policy/connection attempt")
			}
			checks = append(checks, read)
		}
		if len(ev.conduits.reads) < 2 {
			checks = append(checks, checkFail("Teaching-source HTTP conduits", "missing historian and PLC GET evidence"))
		}
		checks = append(checks, euWorkshopNegative(ev, start))
	case "eu-hardening-configurations":
		checks = append(checks, breaker(), euWorkshopSupply(ev), euWorkshopNegative(ev, ev.policyAt), euNoForeignBreaker(ev))
	case "eu-vendor-rdp-compromise":
		checks = append(checks, euWorkshopNegative(ev, ev.policyAt))
		for _, node := range []string{"rmu-1", "rmu-2"} {
			position := map[string]string{"rmu-1": "closed", "rmu-2": "open"}[node]
			checks = append(checks, euWorkshopDeviceCheck(ev, node, node+" restored", func(d *euWorkshopDevice) bool { return d.Position == position }))
		}
		checks = append(checks, euWorkshopDeviceCheck(ev, "oltc-1", "OLTC restored", func(d *euWorkshopDevice) bool { ok, err := euOLTCAuto(d); return err == nil && ok }))
	case "eu-validation-evidence":
		checks = append(checks, link(), euWorkshopDevicesFresh(ev), euWorkshopSupply(ev))
	default:
		checks = append(checks, checkFail("Known workshop slot", "unknown validator key "+slot))
	}
	return checks
}

func euWorkshopDevicesFresh(ev euWorkshopEvidence) ValidationCheck {
	const name = "All field devices delivered at current revisions"
	if ev.processErr != nil || ev.process == nil {
		return checkFail(name, "process delivery evidence unavailable")
	}
	for _, node := range []string{"breaker-1", "rmu-1", "rmu-2", "oltc-1"} {
		device := ev.devices[node]
		if device == nil || ev.deviceErrors[node] != nil || device.Revision == nil {
			return checkFail(name, "device unavailable: "+node)
		}
		delivered, ok := ev.process.Devices[device.DeviceID]
		if !ok || !delivered.Delivered || delivered.Revision == nil || delivered.SolvedRevision == nil || *delivered.Revision != *device.Revision || *delivered.SolvedRevision != *device.Revision {
			return checkFail(name, "solver has not delivered the current revision of "+node)
		}
	}
	return checkPass(name, "all four device revisions match solver revisions and delivery acknowledgements; CC freshness checked independently")
}
