package server

import "github.com/tturner/rangerdanger/backend/internal/containd"

// usPackageID is the package the US recipe belongs to.
const usPackageID = "us-dnp3-substation"

// resetDeviceCommands is the canonical list of (device, command) pairs the
// workshop reset and the test-runner replay against the sims to restore
// default state. Every command here must be a real handler in the
// corresponding sim's main.go switch — TestResetCommandsAreSupported pins
// that contract so a typo (or a sim-handler rename) fails CI instead of
// surfacing as a silent "success: false" at workshop time.
var resetDeviceCommands = []deviceCommand{
	{device: "relay", command: "clear_fault", desc: "Clear relay faults"},
	{device: "relay", command: "unlock", desc: "Unlock relay"},
	{device: "relay", command: "close", desc: "Close feeder breaker"},
	{device: "recloser", command: "clear_fault", desc: "Clear recloser faults"},
	{device: "recloser", command: "reset_lockout", desc: "Reset recloser lockout"},
	{device: "recloser", command: "enable_reclose", desc: "Enable auto-reclose"},
	{device: "recloser", command: "close", desc: "Close recloser"},
	{device: "regulator", command: "set_auto", desc: "Set regulator to auto mode"},
	// capbank: reset_lockout already clears the alarm flag (see
	// services/capbank-sim/main.go reset_lockout handler), so no
	// separate clear_alarm command exists.
	{device: "capbank", command: "reset_lockout", desc: "Reset capbank lockout"},
	{device: "capbank", command: "switch_in", desc: "Switch capbank in"},
	{device: "capbank", command: "set_auto", desc: "Set capbank to auto mode"},
	{device: "regulator", command: "set_tap", desc: "Reset regulator tap to 0", value: new(float64)},
}

var usRecipe = &packageRecipe{
	commandExecutor: (*Server).executeRTACCommand,
	resetCommands:   resetDeviceCommands,
	canary: canaryProbe{fromNode: "kali-1", label: "kali", toNode: "rtac-1", network: "ot_ops_net", port: 502,
		allowUnder: "weak", denyUnder: "improved"},
	testSuite:    true,
	compare:      usCompare,
	staticRules:  usStaticRules,
	workshopName: "Distribution Substation Segmentation",
	policyLabels: map[string]string{"weak": "weak baseline", "improved": "hardened (RTAC-only field access)"},
	// Mirrors the matrix in scripts/validation-report.sh (kept narrow and
	// listener-backed so verdicts are reliable on Docker Desktop).
	validation: []validationProbeSpec{
		{"rtac-1", "rtac-sim", "relay-1", "field_net", "502", "allow", "RTAC Modbus poll to relay", "authorized"},
		{"rtac-1", "rtac-sim", "recloser-1", "field_net", "20000", "allow", "RTAC DNP3 poll to recloser", "authorized"},
		{"rtac-1", "rtac-sim", "regulator-1", "field_net", "20000", "allow", "RTAC DNP3 poll to regulator", "authorized"},
		{"rtac-1", "rtac-sim", "openplc-1", "ot_ops_net", "8080", "allow", "RTAC HTTP API to OpenPLC (intra-zone)", "authorized"},
		{"hmi-1", "fuxa-hmi", "rtac-1", "ot_ops_net", "8080", "allow", "HMI to RTAC HTTP intra-zone", "authorized"},
		{"historian-1", "historian-sim", "rtac-1", "ot_ops_net", "8080", "allow", "Historian to RTAC intra-zone", "authorized"},
		{"vendor-jump-1", "vendor-jump", "rtac-1", "ot_ops_net", "22", "allow", "Vendor SSH mgmt to RTAC", "authorized"},
		{"vendor-jump-1", "vendor-jump", "rtac-1", "ot_ops_net", "443", "allow", "Vendor HTTPS mgmt to RTAC", "authorized"},
		{"kali-1", "kali", "relay-1", "field_net", "502", "deny", "Enterprise Modbus to field relay", "unauthorized"},
		{"kali-1", "kali", "relay-1", "field_net", "20000", "deny", "Enterprise DNP3 to field relay", "unauthorized"},
		{"kali-1", "kali", "openplc-1", "ot_ops_net", "8080", "deny", "Enterprise HTTP to OpenPLC", "unauthorized"},
		{"kali-1", "kali", "rtac-1", "ot_ops_net", "8080", "deny", "Enterprise HTTP to RTAC", "unauthorized"},
		{"kali-1", "kali", "rtac-1", "ot_ops_net", "502", "deny", "Enterprise Modbus to RTAC", "unauthorized"},
		{"eng-ws-1", "eng-ws", "recloser-1", "field_net", "502", "deny", "Vendor Modbus to field recloser", "unauthorized"},
		{"eng-ws-1", "eng-ws", "recloser-1", "field_net", "20000", "deny", "Vendor DNP3 to field recloser", "unauthorized"},
		{"eng-ws-1", "eng-ws", "openplc-1", "ot_ops_net", "8080", "deny", "Vendor HTTP to OpenPLC (only 443/22 allowed)", "unauthorized"},
		{"vendor-jump-1", "vendor-jump", "rtac-1", "ot_ops_net", "502", "deny", "Vendor Modbus to RTAC (improved blocks non-mgmt)", "unauthorized"},
		{"historian-1", "historian-sim", "regulator-1", "field_net", "502", "deny", "Non-RTAC OT (historian) to field regulator (Modbus)", "unauthorized"},
		{"historian-1", "historian-sim", "regulator-1", "field_net", "20000", "deny", "Non-RTAC OT (historian) to field regulator (DNP3)", "unauthorized"},
	},
	validationCapture: captureSpec{nodes: []string{"relay-1", "recloser-1", "regulator-1", "capbank-1"}, network: "field_net"},
	// Scenario-driven traffic only; see runTrafficGeneration.
	traffic: []trafficTarget{
		// Engineering workstation (vendor zone). It has mbpoll, dnp3poll,
		// dnp3cmd, nc, ssh, nmap, curl and python3, so the generated
		// traffic is what a human engineer or attacker would produce.
		{"eng-ws-1", "rtac-1", "ot_ops_net", "curl -sf http://%s:8080/api/state > /dev/null 2>&1 || true", "eng-ws→rtac http"},
		{"eng-ws-1", "rtac-1", "ot_ops_net", "curl -sf http://%s:8080/api/health > /dev/null 2>&1 || true", "eng-ws→rtac http health"},
		{"eng-ws-1", "openplc-1", "ot_ops_net", "curl -sf http://%s:8080/ > /dev/null 2>&1 || true", "eng-ws→openplc http"},
		{"eng-ws-1", "hmi-1", "ot_ops_net", "curl -sf http://%s:1881/ > /dev/null 2>&1 || true", "eng-ws→hmi fuxa"},
		// Real ICS protocol traffic to the field devices: the weak baseline
		// allows it, the hardened policy blocks it. Modbus FC03 reads
		// (5 holding registers, 1 poll, 1 s timeout), then DNP3 class 0 polls.
		{"eng-ws-1", "relay-1", "field_net", "mbpoll -m tcp -a 1 -r 1 -c 5 -1 -t 1 %s > /dev/null 2>&1 || true", "eng-ws→relay modbus FC03 (WEAK)"},
		{"eng-ws-1", "recloser-1", "field_net", "mbpoll -m tcp -a 1 -r 1 -c 5 -1 -t 1 %s > /dev/null 2>&1 || true", "eng-ws→recloser modbus FC03 (WEAK)"},
		{"eng-ws-1", "regulator-1", "field_net", "mbpoll -m tcp -a 1 -r 1 -c 5 -1 -t 1 %s > /dev/null 2>&1 || true", "eng-ws→regulator modbus FC03 (WEAK)"},
		{"eng-ws-1", "capbank-1", "field_net", "mbpoll -m tcp -a 1 -r 1 -c 5 -1 -t 1 %s > /dev/null 2>&1 || true", "eng-ws→capbank modbus FC03 (WEAK)"},
		{"eng-ws-1", "relay-1", "field_net", "dnp3poll %s:20000 -a 1 > /dev/null 2>&1 || true", "eng-ws→relay dnp3 poll (WEAK)"},
		{"eng-ws-1", "recloser-1", "field_net", "dnp3poll %s:20000 -a 2 > /dev/null 2>&1 || true", "eng-ws→recloser dnp3 poll (WEAK)"},
		{"eng-ws-1", "regulator-1", "field_net", "dnp3poll %s:20000 -a 3 > /dev/null 2>&1 || true", "eng-ws→regulator dnp3 poll (WEAK)"},
		{"eng-ws-1", "capbank-1", "field_net", "dnp3poll %s:20000 -a 4 > /dev/null 2>&1 || true", "eng-ws→capbank dnp3 poll (WEAK)"},
		// No HTTP/8080 to field devices: real relays, reclosers, regulators
		// and cap banks don't run web servers. The sims' HTTP API is a lab
		// convenience; generating traffic to it would teach the wrong model.

		// Vendor jump box (vendor zone): remote monitoring and support.
		{"vendor-jump-1", "hmi-1", "ot_ops_net", "curl -sf http://%s:1881/ > /dev/null 2>&1 || true", "vendor→hmi fuxa"},
		{"vendor-jump-1", "rtac-1", "ot_ops_net", "curl -sf http://%s:8080/api/health > /dev/null 2>&1 || true", "vendor→rtac http health"},
	},
}

// usCompare is the US policy comparison: every cross-zone relationship in
// both directions, plus the OT intra-zone pairs.
var usCompare = &compareSpec{
	weakPolicy:     "weak",
	improvedPolicy: "improved",
	// Every entry here is rendered as a row in the Segmentation drawer
	// even if neither policy has an explicit rule — the implicit
	// default-deny is the pedagogical point of the panel (per the Lab 1.3
	// review: students need to see that "no explicit rule" still means
	// "denied at the policy edge"). Both shipping configs have
	// defaultAction: DENY, so an absent rule resolves to DENY there.
	//
	// We list BOTH directions for each cross-zone relationship so the
	// drawer surfaces implicit-deny rows even under the weak baseline
	// (which has explicit ALLOWs for the 8 "lower-to-higher" pairs but
	// no rules for the reverse). The first half is the forward
	// direction (lower-trust → higher-trust, the segmentation lesson);
	// the second half is the reverse (higher-trust → lower-trust,
	// which falls through to default-deny for new connections —
	// conntrack still permits established return traffic).
	pairs: []zonePair{
		// Forward (lower trust → higher trust)
		{"wan", "dmz"}, {"wan", "lan1"}, {"wan", "lan2"},
		{"dmz", "lan1"}, {"dmz", "lan2"},
		{"lan1", "lan2"},
		// Intra-zone
		{"lan1", "lan1"}, {"lan2", "lan2"},
		// Reverse (higher trust → lower trust). These are
		// implicit-deny under both shipping configs; their value is
		// pedagogical: showing the student that the policy has zero
		// explicit rules permitting OT-to-corporate or field-to-OT
		// new connections, even though conntrack happily allows
		// established return traffic.
		{"lan2", "lan1"}, {"lan2", "dmz"}, {"lan2", "wan"},
		{"lan1", "dmz"}, {"lan1", "wan"},
		{"dmz", "wan"},
	},
	labels: map[string]string{"wan": "Enterprise", "dmz": "Vendor", "lan1": "OT Ops", "lan2": "Field"},
}

// usStaticRules are the rule summaries /firewall/rules shows while
// containd is unavailable.
var usStaticRules = []containd.ZoneRuleSummary{
	{SourceZone: "wan", DestZone: "dmz", Summary: "SSH/HTTP/S", Action: "ALLOW", RuleDetails: []string{"Enterprise to Vendor: Standard protocols"}},
	{SourceZone: "wan", DestZone: "lan1", Summary: "WEAK ALLOW", Action: "ALLOW", RuleDetails: []string{"WEAK: Enterprise direct access to OT Ops (should be blocked)"}},
	{SourceZone: "wan", DestZone: "lan2", Summary: "WEAK ALLOW", Action: "ALLOW", RuleDetails: []string{"WEAK: Enterprise direct access to Field Devices (should be blocked)"}},
	{SourceZone: "dmz", DestZone: "lan1", Summary: "WEAK ALLOW", Action: "ALLOW", RuleDetails: []string{"WEAK: Vendor broad access to OT Ops (should be narrowed)"}},
	{SourceZone: "dmz", DestZone: "lan2", Summary: "WEAK ALLOW", Action: "ALLOW", RuleDetails: []string{"WEAK: Vendor direct access to Field Devices (should be blocked)"}},
	{SourceZone: "lan1", DestZone: "lan2", Summary: "WEAK ALLOW", Action: "ALLOW", RuleDetails: []string{"WEAK: All OT nodes can reach Field Devices (should be RTAC only)"}},
	{SourceZone: "lan1", DestZone: "lan1", Summary: "OT Internal", Action: "ALLOW", RuleDetails: []string{"OT Operations internal communication"}},
}
