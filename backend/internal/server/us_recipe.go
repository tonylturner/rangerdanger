package server

import (
	"fmt"
	"net"
	"strings"

	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

// packageRecipe is the curriculum-specific operations of one package: the
// policies the firewall buttons apply, the probes that prove the dataplane,
// the traffic a lab generates. Recipes name topology nodes and manifest
// networks; the generation's manifest resolves them to containers and
// addresses. A package without a recipe has none of these operations.
type packageRecipe struct {
	// policies maps the names the UI applies ("weak", "improved") to
	// policy files relative to the definitions directory.
	policies      map[string]string
	resetCommands []deviceCommand
	canary        canaryProbe
	validation    []validationProbeSpec
	// validationCapture names the devices the validation report's PCAP
	// watches, and the network they sit on.
	validationCapture captureSpec
	traffic           []trafficTarget
}

// deviceCommand is one RTAC device command; value is its argument, if any.
type deviceCommand struct {
	device, command, desc string
	value                 *float64
}

// canaryProbe is the TCP connect whose verdict shows the dataplane enforces
// a canned policy: allowed under weak, denied under improved.
type canaryProbe struct {
	fromNode, label string
	toNode, network string
	port            int
}

// validationProbeSpec is one row of the validation matrix before the
// manifest resolves it.
type validationProbeSpec struct {
	fromNode, label          string
	toNode, network, port    string
	expected, note, category string
}

type captureSpec struct {
	nodes   []string
	network string
}

// trafficTarget runs command, with %s replaced by toNode's address on
// network, in fromNode's container.
type trafficTarget struct {
	fromNode, toNode, network, command, desc string
}

// usPackageID is the package the US recipe belongs to.
const usPackageID = "us-dnp3-substation"

var packageRecipes = map[string]*packageRecipe{usPackageID: usRecipe}

// recipeFor returns the recipe of the generation's package.
func recipeFor(gen *lifecycle.Generation) (*packageRecipe, error) {
	recipe, ok := packageRecipes[gen.Package.ID]
	if !ok {
		return nil, fmt.Errorf("package %s has no workshop recipe", gen.Package.ID)
	}
	return recipe, nil
}

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
	policies: map[string]string{
		"weak":     "firewall/substation-weak.json",
		"improved": "firewall/substation-improved.json",
	},
	resetCommands: resetDeviceCommands,
	canary:        canaryProbe{fromNode: "kali-1", label: "kali", toNode: "rtac-1", network: "ot_ops_net", port: 502},
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

// resolveValidation turns the recipe's matrix into probes against the
// generation's containers and addresses.
func (r *packageRecipe) resolveValidation(gen *lifecycle.Generation) ([]validationProbe, error) {
	probes := make([]validationProbe, 0, len(r.validation))
	for _, spec := range r.validation {
		from, err := nodeService(gen, spec.fromNode)
		if err != nil {
			return nil, err
		}
		dst, err := nodeIP(gen, spec.toNode, spec.network)
		if err != nil {
			return nil, err
		}
		probes = append(probes, validationProbe{
			Src: from.Container, SrcLabel: spec.label, Dst: dst, Port: spec.port,
			Expected: spec.expected, Note: spec.note, Category: spec.category,
		})
	}
	return probes, nil
}

// resolveCapture returns the capture's device addresses and the first three
// octets of their /24 network, which the PCAP summary leaves out.
func (r *packageRecipe) resolveCapture(gen *lifecycle.Generation) ([]string, []string, error) {
	var hosts []string
	for _, node := range r.validationCapture.nodes {
		ip, err := nodeIP(gen, node, r.validationCapture.network)
		if err != nil {
			return nil, nil, err
		}
		hosts = append(hosts, ip)
	}
	for _, network := range gen.Manifest.Networks {
		if network.Key != r.validationCapture.network {
			continue
		}
		_, subnet, err := net.ParseCIDR(network.Subnet)
		if err != nil || subnet.IP.To4() == nil || maskOnes(subnet) != 24 {
			return nil, nil, fmt.Errorf("capture network %s subnet %s is not an IPv4 /24", network.Key, network.Subnet)
		}
		return hosts, strings.Split(subnet.IP.To4().String(), ".")[:3], nil
	}
	return nil, nil, fmt.Errorf("package %s has no network %s", gen.Package.ID, r.validationCapture.network)
}

func maskOnes(subnet *net.IPNet) int {
	ones, _ := subnet.Mask.Size()
	return ones
}

// resolvedTraffic is a trafficTarget against the generation's containers.
type resolvedTraffic struct {
	container, cmd, desc string
}

// resolveTraffic resolves the generation's traffic recipe.
func resolveTraffic(gen *lifecycle.Generation) ([]resolvedTraffic, error) {
	recipe, err := recipeFor(gen)
	if err != nil {
		return nil, err
	}
	targets := make([]resolvedTraffic, 0, len(recipe.traffic))
	for _, t := range recipe.traffic {
		from, err := nodeService(gen, t.fromNode)
		if err != nil {
			return nil, err
		}
		to, err := nodeIP(gen, t.toNode, t.network)
		if err != nil {
			return nil, err
		}
		targets = append(targets, resolvedTraffic{container: from.Container, cmd: fmt.Sprintf(t.command, to), desc: t.desc})
	}
	return targets, nil
}
