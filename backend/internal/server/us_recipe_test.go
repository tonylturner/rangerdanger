package server

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// The US recipe used to hard-code containers and addresses. Resolved
// against the real US manifest it must produce exactly those values: the
// migration to the manifest changes no US behaviour.

func TestUSValidationMatrixResolvesToTheFrozenMatrix(t *testing.T) {
	want := []validationProbe{
		{"rangerdanger-rtac-sim", "rtac-sim", "10.40.40.20", "502", "allow", "RTAC Modbus poll to relay", "authorized"},
		{"rangerdanger-rtac-sim", "rtac-sim", "10.40.40.21", "20000", "allow", "RTAC DNP3 poll to recloser", "authorized"},
		{"rangerdanger-rtac-sim", "rtac-sim", "10.40.40.22", "20000", "allow", "RTAC DNP3 poll to regulator", "authorized"},
		{"rangerdanger-rtac-sim", "rtac-sim", "10.30.30.30", "8080", "allow", "RTAC HTTP API to OpenPLC (intra-zone)", "authorized"},
		{"rangerdanger-fuxa-hmi", "fuxa-hmi", "10.30.30.20", "8080", "allow", "HMI to RTAC HTTP intra-zone", "authorized"},
		{"rangerdanger-historian-sim", "historian-sim", "10.30.30.20", "8080", "allow", "Historian to RTAC intra-zone", "authorized"},
		{"rangerdanger-vendor-jump", "vendor-jump", "10.30.30.20", "22", "allow", "Vendor SSH mgmt to RTAC", "authorized"},
		{"rangerdanger-vendor-jump", "vendor-jump", "10.30.30.20", "443", "allow", "Vendor HTTPS mgmt to RTAC", "authorized"},
		{"rangerdanger-kali", "kali", "10.40.40.20", "502", "deny", "Enterprise Modbus to field relay", "unauthorized"},
		{"rangerdanger-kali", "kali", "10.40.40.20", "20000", "deny", "Enterprise DNP3 to field relay", "unauthorized"},
		{"rangerdanger-kali", "kali", "10.30.30.30", "8080", "deny", "Enterprise HTTP to OpenPLC", "unauthorized"},
		{"rangerdanger-kali", "kali", "10.30.30.20", "8080", "deny", "Enterprise HTTP to RTAC", "unauthorized"},
		{"rangerdanger-kali", "kali", "10.30.30.20", "502", "deny", "Enterprise Modbus to RTAC", "unauthorized"},
		{"rangerdanger-eng-ws", "eng-ws", "10.40.40.21", "502", "deny", "Vendor Modbus to field recloser", "unauthorized"},
		{"rangerdanger-eng-ws", "eng-ws", "10.40.40.21", "20000", "deny", "Vendor DNP3 to field recloser", "unauthorized"},
		{"rangerdanger-eng-ws", "eng-ws", "10.30.30.30", "8080", "deny", "Vendor HTTP to OpenPLC (only 443/22 allowed)", "unauthorized"},
		{"rangerdanger-vendor-jump", "vendor-jump", "10.30.30.20", "502", "deny", "Vendor Modbus to RTAC (improved blocks non-mgmt)", "unauthorized"},
		{"rangerdanger-historian-sim", "historian-sim", "10.40.40.22", "502", "deny", "Non-RTAC OT (historian) to field regulator (Modbus)", "unauthorized"},
		{"rangerdanger-historian-sim", "historian-sim", "10.40.40.22", "20000", "deny", "Non-RTAC OT (historian) to field regulator (DNP3)", "unauthorized"},
	}
	got, err := usRecipe.resolveValidation(usGeneration(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("resolved matrix differs from the frozen US matrix:\n got %v\nwant %v", got, want)
	}

	hosts, network, err := usRecipe.resolveCapture(usGeneration(nil))
	if err != nil || strings.Join(hosts, " ") != "10.40.40.20 10.40.40.21 10.40.40.22 10.40.40.23" || strings.Join(network, ".") != "10.40.40" {
		t.Errorf("capture = %q %q %v", hosts, network, err)
	}
}

func TestUSTrafficResolvesToTheFrozenTargets(t *testing.T) {
	want := []resolvedTraffic{
		{"rangerdanger-eng-ws", "curl -sf http://10.30.30.20:8080/api/state > /dev/null 2>&1 || true", "eng-ws→rtac http"},
		{"rangerdanger-eng-ws", "curl -sf http://10.30.30.20:8080/api/health > /dev/null 2>&1 || true", "eng-ws→rtac http health"},
		{"rangerdanger-eng-ws", "curl -sf http://10.30.30.30:8080/ > /dev/null 2>&1 || true", "eng-ws→openplc http"},
		{"rangerdanger-eng-ws", "curl -sf http://10.30.30.10:1881/ > /dev/null 2>&1 || true", "eng-ws→hmi fuxa"},
		{"rangerdanger-eng-ws", "mbpoll -m tcp -a 1 -r 1 -c 5 -1 -t 1 10.40.40.20 > /dev/null 2>&1 || true", "eng-ws→relay modbus FC03 (WEAK)"},
		{"rangerdanger-eng-ws", "mbpoll -m tcp -a 1 -r 1 -c 5 -1 -t 1 10.40.40.21 > /dev/null 2>&1 || true", "eng-ws→recloser modbus FC03 (WEAK)"},
		{"rangerdanger-eng-ws", "mbpoll -m tcp -a 1 -r 1 -c 5 -1 -t 1 10.40.40.22 > /dev/null 2>&1 || true", "eng-ws→regulator modbus FC03 (WEAK)"},
		{"rangerdanger-eng-ws", "mbpoll -m tcp -a 1 -r 1 -c 5 -1 -t 1 10.40.40.23 > /dev/null 2>&1 || true", "eng-ws→capbank modbus FC03 (WEAK)"},
		{"rangerdanger-eng-ws", "dnp3poll 10.40.40.20:20000 -a 1 > /dev/null 2>&1 || true", "eng-ws→relay dnp3 poll (WEAK)"},
		{"rangerdanger-eng-ws", "dnp3poll 10.40.40.21:20000 -a 2 > /dev/null 2>&1 || true", "eng-ws→recloser dnp3 poll (WEAK)"},
		{"rangerdanger-eng-ws", "dnp3poll 10.40.40.22:20000 -a 3 > /dev/null 2>&1 || true", "eng-ws→regulator dnp3 poll (WEAK)"},
		{"rangerdanger-eng-ws", "dnp3poll 10.40.40.23:20000 -a 4 > /dev/null 2>&1 || true", "eng-ws→capbank dnp3 poll (WEAK)"},
		{"rangerdanger-vendor-jump", "curl -sf http://10.30.30.10:1881/ > /dev/null 2>&1 || true", "vendor→hmi fuxa"},
		{"rangerdanger-vendor-jump", "curl -sf http://10.30.30.20:8080/api/health > /dev/null 2>&1 || true", "vendor→rtac http health"},
	}
	got, err := resolveTraffic(usGeneration(nil))
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("resolveTraffic = %v, %v\nwant %v", got, err, want)
	}
}

func TestUSIdentitiesFromTheManifest(t *testing.T) {
	gen := usGeneration(nil)
	if got, err := rtacURL(gen); err != nil || got != "http://10.99.99.20:8080" {
		t.Errorf("rtacURL = %q, %v; want the RTAC mgmt endpoint", got, err)
	}
	if got, err := firewallContainer(gen); err != nil || got != "rangerdanger-firewall" {
		t.Errorf("firewallContainer = %q, %v", got, err)
	}
	fw, _ := nodeService(gen, "fw-1")
	rtac, _ := nodeService(gen, "rtac-1")
	if !isFirewall(fw) || isFirewall(rtac) {
		t.Error("isFirewall does not follow the firewall role")
	}
}

func TestRecipesAreUSOnly(t *testing.T) {
	gen := lifecycle.NewGeneration(1, labs.Package{ID: "eu-iec104-substation"}, &manifest.Manifest{}, nil)
	if _, err := recipeFor(gen); err == nil || !strings.Contains(err.Error(), "eu-iec104-substation has no workshop recipe") {
		t.Errorf("recipeFor(EU) = %v, want no recipe", err)
	}
	if _, err := resolveTraffic(gen); err == nil {
		t.Error("traffic resolved for a package without a recipe")
	}
}

func TestWorkshopGraphInterfaceIPsFromManifest(t *testing.T) {
	s := routedServer(t)
	// The topology lists the RTAC on two networks; the old lookup keyed
	// "rtac" never matched node "rtac-1" and reported only the first.
	s.rng.(*fakeRange).gen.Package.Template = labs.LabYAML{ID: testTemplateID, Nodes: []labs.NodeYAML{
		{ID: "rtac-1", Type: "rtac_sim", Networks: []string{"ot_ops_net", "field_net"}, IP: "10.30.30.20", Container: "rangerdanger-rtac-sim"},
	}}
	rec := serve(s, http.MethodGet, "/api/workshop/graph")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/workshop/graph = %d %s", rec.Code, rec.Body)
	}
	var body struct {
		Nodes []graphNode `json:"nodes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	var zones []string
	var rtac *graphNode
	for i, n := range body.Nodes {
		if n.Type == "zone" {
			zones = append(zones, n.Data.Zone)
		}
		if n.ID == "rtac-1" {
			rtac = &body.Nodes[i]
		}
	}
	if strings.Join(zones, ",") != "enterprise_net,vendor_net,ot_ops_net,field_net" {
		t.Errorf("zones = %q, want the manifest's teaching zones in order", zones)
	}
	want := map[string]string{"ot_ops_net": "10.30.30.20", "field_net": "10.40.40.10"}
	if rtac == nil || !reflect.DeepEqual(rtac.Data.InterfaceIPs, want) {
		t.Errorf("rtac-1 interface IPs = %+v, want %v", rtac, want)
	}
}

func TestWorkshopExecUnknownNode(t *testing.T) {
	s := newTestServer(t, "http://127.0.0.1:1")
	rec, body := invoke(s, s.handleWorkshopExec, "POST", "/api/workshop/nodes/plc-9/exec", map[string]string{"command": "ls"})
	if rec.Code != http.StatusNotFound || !strings.Contains(body["error"].(string), "node") {
		t.Errorf("exec on an unknown node = %d %v", rec.Code, body)
	}
}

func TestNetworkEventsFilterUsesTheZoneSubnets(t *testing.T) {
	zones := zoneSubnets(usGeneration(nil))
	for address, want := range map[string]bool{
		"10.10.10.50": true, "10.20.20.20": true, "10.30.30.20": true, "10.40.40.23": true,
		"10.50.50.10": false, "10.99.99.2": false, "not-an-ip": false,
	} {
		if got := inAnySubnet(zones, address); got != want {
			t.Errorf("inAnySubnet(%s) = %v, want %v", address, got, want)
		}
	}
}
