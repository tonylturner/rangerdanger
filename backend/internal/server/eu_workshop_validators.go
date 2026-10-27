package server

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/client"
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
	stationPoints                                      []labs.IEC104Point
	policyHash                                         string
	report                                             *retainedValidationReport
	start, now, policyAt, hardeningSince, operationEnd time.Time
	policy                                             string
	isPlan                                             bool
	foreignBreakerAllowed                              bool
	policyErr                                          error
	capture                                            bool
	cc                                                 *conduitState
	journal                                            *ccJournal
	ccErr                                              error
	process                                            *euWorkshopProcess
	processErr                                         error
	devices                                            map[string]*euWorkshopDevice
	deviceErrors                                       map[string]error
	negative                                           []validationRow
	negativeExpected                                   int
	negativeErr                                        error
	identityErr                                        error
	conduits                                           conduitEvidence
	breakerAudit                                       *euWorkshopBreakerAudit
	auditErr                                           error
	logErr                                             error
	ccIP, rtuIP                                        string
}

func (s *Server) collectEUWorkshop(ctx context.Context, gen *lifecycle.Generation, slot string) euWorkshopEvidence {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	ev := euWorkshopEvidence{start: gen.StartedAt, stationPoints: gen.Package.StationPoints, devices: map[string]*euWorkshopDevice{}, deviceErrors: map[string]error{}}
	s.activeConfigMu.RLock()
	ev.policy, ev.policyAt, ev.hardeningSince = s.activeConfig, s.policyChangedAt, s.policyEvidenceSince
	wantHash := s.lastAppliedHash
	ev.policyHash, ev.report = wantHash, s.lastValidationReport
	ev.isPlan = s.policySource == "plan" && s.planProvenance != nil && s.appliedPlan != nil
	planIdentityOK := !ev.isPlan || s.planProvenance.PolicyHash == wantHash
	s.activeConfigMu.RUnlock()
	hash, err := gen.Containd().GetFirewallHash(ctx)
	ev.policyErr = err
	if err == nil && (wantHash == "" || hash != wantHash || !planIdentityOK) {
		ev.policyErr = fmt.Errorf("running policy differs from recorded policy; wait for the observer or reapply")
	}
	// Verify weak/non-weak against the document, not a label. A failed
	// comparison is missing evidence, never evidence of a custom policy.
	if ev.policyErr == nil {
		weakPath, weakErr := s.policyPath(gen.Package, "weak")
		if weakErr == nil {
			var data []byte
			data, weakErr = readPolicyJSONWithRetry(weakPath)
			if weakErr == nil {
				var weakHash string
				weakHash, weakErr = containd.FirewallHashFromBytes(data)
				if weakErr == nil && !ev.isPlan && ((ev.policy == "weak") != (hash == weakHash)) {
					weakErr = fmt.Errorf("weak/non-weak policy identity mismatch")
				}
			}
		}
		if weakErr != nil {
			ev.policyErr = weakErr
		}
	}
	if slot == "eu-baseline-assessment" {
		ev.capture = s.checkPcapFileExists(ctx, gen)
	}
	// Snapshot hardening's CC evidence before its active TCP matrix. The gate
	// waits for post-GI GOOD observations immediately before validation; these
	// independent probes can take several seconds and let telemetry age/change
	// before the CC snapshot is read.
	if slot == "eu-hardening-configurations" {
		ev.cc, ev.journal, ev.ccErr = euCC(ctx, gen)
	}
	// Collect current process truth after the negative probes.
	if slot == "eu-firewall-implementation" || slot == "eu-hardening-configurations" || slot == "eu-vendor-rdp-compromise" {
		expectations, matrixErr := s.judgingExpectations(gen)
		var matrix []validationProbe
		if matrixErr == nil {
			matrix, matrixErr = resolveProof(gen, expectations, slot, true)
			for _, expectation := range expectations {
				if expectation.From == "kali-1" && expectation.To == "rtu-1" && expectation.Port == 2404 {
					ev.foreignBreakerAllowed = expectation.PlanVerdict == labs.VerdictAllow
				}
			}
		}
		ev.negativeErr = matrixErr
		ev.negativeExpected = len(matrix)
		if matrixErr == nil {
			var cli *client.Client
			if s.orchestrator != nil {
				cli = s.orchestrator.DockerClient()
			}
			ev.negative = runEUProbeMatrix(ctx, matrix, func(ctx context.Context, p validationProbe) validationRow { return s.strictTCPProbe(ctx, cli, p) })
		}
	}
	if slot == "eu-firewall-implementation" {
		ev.conduits = s.collectEUWorkshopConduits(ctx, gen)
	}
	nodes := []string{"breaker-1"}
	if slot == "eu-vendor-rdp-compromise" || slot == "eu-validation-evidence" || slot == "eu-firewall-implementation" {
		nodes = append(nodes, "f2-breaker", "rmu-1", "rmu-2", "oltc-1")
	}
	for _, node := range nodes {
		ev.devices[node], ev.deviceErrors[node] = euDevice(ctx, gen, node)
	}
	ev.process, ev.processErr = euProcess(ctx, gen)
	if slot == "eu-firewall-implementation" {
		ev.cc, ev.journal, ev.ccErr = ev.conduits.state, ev.conduits.journal, ev.conduits.stateErr
		if ev.ccErr == nil {
			ev.ccErr = ev.conduits.journalErr
		}
	} else if slot == "eu-baseline-assessment" || slot == "eu-validation-evidence" {
		ev.cc, ev.journal, ev.ccErr = euCC(ctx, gen)
	}
	if slot == "eu-hardening-configurations" && (!ev.isPlan || !ev.foreignBreakerAllowed) {
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
		ev.logErr = s.euRTULog(ctx, gen, ev.hardeningSince, ev.operationEnd, ev.ccIP)
	}
	finalHash, finalErr := gen.Containd().GetFirewallHash(ctx)
	if ev.policyErr == nil && (finalErr != nil || finalHash != hash) {
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

func euWorkshopPolicy(ev euWorkshopEvidence, weak bool) ValidationCheck {
	ok := ev.policyErr == nil && ev.policy == "weak"
	detail := "running weak baseline"
	if !weak {
		ok = ev.policyErr == nil && (ev.policy == "improved" || ev.policy == "custom") && insideEvidenceWindow(ev.policyAt, ev.start, ev.now)
		detail = "recorded non-weak running policy (not a plan); judged against Hardened by the checks below"
		if ev.isPlan {
			detail = "applied plan is the judging target; Hardened acceptance is the comparison"
		}
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
	return checkPass(name, "current schema-3 device truth (not wire-attack evidence)")
}

func euWorkshopSupply(ev euWorkshopEvidence) ValidationCheck {
	present, err := euSupply(ev.process, ev.now)
	if ev.processErr != nil || err != nil || !present {
		return checkFail("Feeder supply present", fmt.Sprintf("fresh supply on bus20/S1/S2/S3 not established: %v; %v", ev.processErr, err))
	}
	return checkPass("Feeder supply present", "fresh process solve ≤3 s: bus20/S1/S2/S3 live with positive voltage")
}

// These active, connect-only observations are independent of policy logging
// settings and historical CC connection windows. No SYN payload is decoded.
func euWorkshopNegative(ev euWorkshopEvidence) ValidationCheck {
	const name = "Active TCP matrix matches judging target"
	if ev.negativeErr != nil || ev.negativeExpected == 0 || len(ev.negative) != ev.negativeExpected {
		return checkFail(name, fmt.Sprintf("incomplete negative probe matrix: %d/%d; %v", len(ev.negative), ev.negativeExpected, ev.negativeErr))
	}
	for _, row := range ev.negative {
		if row.Actual == "skipped" || row.Actual != row.Expected || !row.Pass || row.ProbeError != "" {
			return checkFail(name, fmt.Sprintf("%s (%s)→%s:%s: %s; %s", row.SrcLabel, row.Src, row.Dst, row.Port, row.Actual, row.ProbeError))
		}
	}
	return checkPass(name, fmt.Sprintf("all %d active TCP probes match the judging target; Hardened remains the comparison. No logged denies required, no decoded command or firewall-causation claim", len(ev.negative)))
}

func evaluateEUWorkshop(slot string, ev euWorkshopEvidence) []ValidationCheck {
	checks := []ValidationCheck{euWorkshopPolicy(ev, slot == "eu-baseline-assessment")}
	breaker := func() ValidationCheck {
		return euWorkshopDeviceCheck(ev, "breaker-1", "Breaker closed", func(d *euWorkshopDevice) bool { return d.Position == "closed" })
	}
	link := func() ValidationCheck {
		if ev.ccErr != nil {
			return checkFail("CC link, GI and station delivery", ev.ccErr.Error())
		}
		if slot == "eu-firewall-implementation" || slot == "eu-hardening-configurations" {
			return euWorkshopFreshLink(ev)
		}
		return euLink(ev.cc, ev.journal, ev.stationPoints, ev.start, ev.now)
	}
	switch slot {
	case "eu-baseline-assessment":
		checks = append(checks, euLab3Check("Capture exists", ev.capture, "a capture file exists; contents and exercise completion are not certified"), link(), breaker(), euWorkshopSupply(ev))
	case "eu-firewall-implementation":
		attempt := ev.conduits
		if ev.policyAt.After(attempt.rangeStart) {
			attempt.rangeStart = ev.policyAt
		}
		start, window := conduitWindow(attempt)
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
		checks = append(checks, link(), euWorkshopContinuity(ev), euWorkshopSupply(ev), euWorkshopNegative(ev))
	case "eu-hardening-configurations":
		checks = append(checks, link(), breaker(), euWorkshopSupply(ev), euWorkshopNegative(ev), euNoForeignBreaker(ev))
	case "eu-vendor-rdp-compromise":
		checks = append(checks, euWorkshopNegative(ev))
		for _, node := range []string{"rmu-1", "rmu-2"} {
			position := map[string]string{"rmu-1": "closed", "rmu-2": "open"}[node]
			checks = append(checks, euWorkshopDeviceCheck(ev, node, node+" restored", func(d *euWorkshopDevice) bool { return d.Position == position }))
		}
		checks = append(checks, euWorkshopDeviceCheck(ev, "oltc-1", "OLTC restored", func(d *euWorkshopDevice) bool { ok, err := euOLTCAuto(d); return err == nil && ok }))
	case "eu-validation-evidence":
		checks = append(checks, euWorkshopReport(ev), link(), euWorkshopDevicesFresh(ev), euWorkshopSupply(ev))
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
	for _, node := range []string{"breaker-1", "f2-breaker", "rmu-1", "rmu-2", "oltc-1"} {
		device := ev.devices[node]
		if device == nil || ev.deviceErrors[node] != nil || device.Revision == nil {
			return checkFail(name, "device unavailable: "+node)
		}
		delivered, ok := ev.process.Devices[device.DeviceID]
		if !ok || !delivered.Delivered || delivered.Revision == nil || delivered.SolvedRevision == nil || *delivered.Revision != *device.Revision || *delivered.SolvedRevision != *device.Revision {
			return checkFail(name, "solver has not delivered the current revision of "+node)
		}
	}
	return checkPass(name, "all five device revisions match solver revisions and delivery acknowledgements; CC freshness checked independently")
}

// A GI on an established pre-change connection cannot test the new policy.
func euWorkshopFreshLink(ev euWorkshopEvidence) ValidationCheck {
	const name = "Fresh CC session admitted after policy commit"
	if ev.cc == nil || ev.policyAt.IsZero() || !ev.cc.Link.Since.After(ev.policyAt) || ev.cc.Link.Since.After(ev.now) {
		return checkFail(name, "CC link.since must be later than the policy commit; POST /api/operator/reconnect, wait for reconnect/GI, then validate. A GI on the old session proves nothing")
	}
	return euLink(ev.cc, ev.journal, ev.stationPoints, ev.cc.Link.Since, ev.now)
}

func euWorkshopContinuity(ev euWorkshopEvidence) ValidationCheck {
	const name = "Breaker and RMU baseline positions preserved"
	for _, node := range []string{"breaker-1", "f2-breaker", "rmu-1", "rmu-2"} {
		position := "closed"
		if node == "rmu-2" {
			position = "open"
		}
		if check := euWorkshopDeviceCheck(ev, node, name, func(d *euWorkshopDevice) bool { return d.Position == position }); check.Status != "pass" {
			check.Detail = node + ": " + check.Detail
			return check
		}
	}
	return checkPass(name, "independent field truth: both breakers closed, RMU1 closed/RMU2 open; loss of observations is not loss of supply")
}
