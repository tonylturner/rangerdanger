package labs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ICS rule modes a package declares in package.yml `ics_rules`.
const (
	// ICSRulesAllowed lets policies carry containd `ics` blocks.
	ICSRulesAllowed = "allowed"
	// ICSRulesForbidden rejects any `ics` key in a policy, at any depth and
	// whatever its value, so a package cannot import ICS rules its range has
	// no decoder for.
	ICSRulesForbidden = "forbidden"
)

const icsKey = "ics"

// LintPolicy checks one containd policy document: it must be a single JSON
// object, and under ICSRulesForbidden no object anywhere in it may have an
// `ics` key, even `{}` or null. file names the document in errors; each
// forbidden key is reported by its JSON pointer. The catalog loader, the
// package lint and the custom-policy apply share this one check.
func LintPolicy(file string, data []byte, icsRules string) error {
	if icsRules != ICSRulesAllowed && icsRules != ICSRulesForbidden {
		return fmt.Errorf("%s: unknown ics_rules %q", file, icsRules)
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("%s: invalid JSON: %w", file, err)
	}
	if _, ok := document.(map[string]any); !ok {
		return fmt.Errorf("%s: policy must be a JSON object", file)
	}
	if err := CheckManagementSafety(data); err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	if icsRules == ICSRulesAllowed {
		return nil
	}
	// The document is valid JSON, so the token walk cannot fail on syntax;
	// it sees every key in order, duplicates included.
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var found []string
	if err := walkKeys(decoder, "", &found); err != nil {
		return fmt.Errorf("%s: invalid JSON: %w", file, err)
	}
	if len(found) == 0 {
		return nil
	}
	problems := make([]string, len(found))
	for index, pointer := range found {
		problems[index] = fmt.Sprintf("%s: key %q at %s is forbidden (ics_rules: %s)", file, icsKey, pointer, ICSRulesForbidden)
	}
	return fmt.Errorf("%s", strings.Join(problems, "; "))
}

// CheckManagementSafety protects the Docker-assigned control plane on every
// RangerDanger full-policy import. It is not an invariant for edits made directly
// in containd. Nil access flags retain containd's enabled-by-default semantics.
func CheckManagementSafety(data []byte) error {
	reject := func(field string) error {
		return fmt.Errorf("%s: RangerDanger keeps containd management on lan3; preserve Docker addressing, routing and HTTP :8080", field)
	}
	var doc struct {
		Interfaces []struct {
			Name, Zone, Device, Type, Parent, AddressMode, Gateway string
			VLANID                                                 int
			Members, Addresses                                     []string
			Access                                                 map[string]*bool
		}
		System struct {
			Mgmt struct {
				EnableHTTP, RedirectHTTPToHTTPS *bool
				ListenAddr, HTTPListenAddr      string
			}
		}
		Routing struct {
			Gateways, Routes, Rules []json.RawMessage
		}
		Firewall struct {
			NAT struct {
				Enabled      bool
				EgressZone   string
				SourceZones  []string
				PortForwards []struct {
					Enabled     bool
					IngressZone string
					ListenPort  int
				}
			}
		}
		Services struct {
			Proxy struct {
				Forward, Reverse struct{ Enabled bool }
			}
			VPN struct {
				Wireguard, Openvpn struct{ Enabled bool }
			}
		}
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("management policy fields: RangerDanger keeps containd management on lan3: %w", err)
	}
	count := 0
	names := make(map[string]bool)
	for index, iface := range doc.Interfaces {
		field := fmt.Sprintf("interfaces/%d", index)
		if names[iface.Name] {
			return reject(field + "/name")
		}
		names[iface.Name] = true
		if iface.Name == "lan3" {
			count++
			if iface.Zone != "lan3" {
				return reject(field + "/zone")
			}
			for _, flag := range []string{"mgmt", "http", "https", "ssh"} {
				if value := iface.Access[flag]; value != nil && !*value {
					return reject(field + "/access/" + flag)
				}
			}
		} else if iface.Zone == "lan3" {
			return reject(field + "/zone")
		}
		// Any static binding can steal the management NIC from auto-assignment;
		// bridges/VLANs and address reconciliation can alter it indirectly too.
		for _, setting := range []struct {
			name string
			set  bool
		}{
			{"device", iface.Device != ""},
			{"type", iface.Type != "" && iface.Type != "physical"},
			{"parent", iface.Parent != ""},
			{"vlanId", iface.VLANID != 0},
			{"members", len(iface.Members) != 0},
			{"addressMode", iface.AddressMode != ""},
			{"addresses", len(iface.Addresses) != 0},
			{"gateway", iface.Gateway != ""},
		} {
			if setting.set {
				return reject(field + "/" + setting.name)
			}
		}
	}
	if count != 1 {
		return reject("interfaces/lan3")
	}
	if doc.System.Mgmt.EnableHTTP != nil && !*doc.System.Mgmt.EnableHTTP {
		return reject("system/mgmt/enableHTTP")
	}
	// containd redirects API GETs too, but RangerDanger uses plain HTTP on
	// lan3 and does not trust the appliance's self-signed HTTPS certificate.
	if doc.System.Mgmt.RedirectHTTPToHTTPS != nil && *doc.System.Mgmt.RedirectHTTPToHTTPS {
		return reject("system/mgmt/redirectHTTPToHTTPS")
	}
	for _, listener := range []struct{ name, value string }{
		{"listenAddr", doc.System.Mgmt.ListenAddr},
		{"httpListenAddr", doc.System.Mgmt.HTTPListenAddr},
	} {
		switch listener.value {
		case "", ":8080", "0.0.0.0:8080", "10.99.99.2:8080":
		default:
			return reject("system/mgmt/" + listener.name)
		}
	}
	// No shipped policy needs host route manipulation. Even policy rules or
	// routes in a non-main table can redirect replies when combined.
	for _, routes := range []struct {
		name string
		rows []json.RawMessage
	}{{"gateways", doc.Routing.Gateways}, {"routes", doc.Routing.Routes}, {"rules", doc.Routing.Rules}} {
		if len(routes.rows) != 0 {
			return reject("routing/" + routes.name)
		}
	}
	nat := doc.Firewall.NAT
	if nat.Enabled {
		if strings.EqualFold(strings.TrimSpace(nat.EgressZone), "lan3") {
			return reject("firewall/nat/egressZone")
		}
		// containd defaults an empty source list to every non-egress zone.
		if len(nat.SourceZones) == 0 {
			return reject("firewall/nat/sourceZones")
		}
		for _, zone := range nat.SourceZones {
			if strings.EqualFold(strings.TrimSpace(zone), "lan3") {
				return reject("firewall/nat/sourceZones")
			}
		}
	}
	// Port forwards are compiled independently of NAT.Enabled in containd.
	for index, forward := range nat.PortForwards {
		if forward.Enabled && (strings.TrimSpace(forward.IngressZone) == "" || strings.EqualFold(strings.TrimSpace(forward.IngressZone), "lan3") ||
			forward.ListenPort == 8080 || forward.ListenPort == 8443 || forward.ListenPort == 2222) {
			return reject(fmt.Sprintf("firewall/nat/portForwards/%d", index))
		}
	}
	// These optional services can create listeners or routes outside the
	// ordinary FORWARD rules. They are not part of any shipped lab policy.
	if doc.Services.Proxy.Forward.Enabled || doc.Services.Proxy.Reverse.Enabled {
		return reject("services/proxy")
	}
	if doc.Services.VPN.Wireguard.Enabled || doc.Services.VPN.Openvpn.Enabled {
		return reject("services/vpn")
	}
	return nil
}

// walkKeys reads one JSON value and appends the pointer of every `ics`
// object key inside it to found.
func walkKeys(decoder *json.Decoder, pointer string, found *[]string) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, _ := keyToken.(string)
			child := pointer + "/" + escapePointer(key)
			if key == icsKey {
				*found = append(*found, child)
			}
			if err := walkKeys(decoder, child, found); err != nil {
				return err
			}
		}
	case '[':
		for index := 0; decoder.More(); index++ {
			if err := walkKeys(decoder, pointer+"/"+strconv.Itoa(index), found); err != nil {
				return err
			}
		}
	}
	_, err = decoder.Token() // the closing delimiter
	return err
}

// escapePointer escapes one RFC 6901 reference token.
func escapePointer(token string) string {
	return strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1")
}

// resolvePolicies resolves package.yml `policies` from the package
// directory, keeps every file inside the definitions directory, lints each
// one, and returns the name → definitions-relative path map. Two names may
// not share a file: the active policy name is found by exact path match.
func resolvePolicies(definitionsDir, packageDir string, declared map[string]string, icsRules string) (map[string]string, error) {
	if len(declared) == 0 {
		return nil, fmt.Errorf("policies must name at least one policy file")
	}
	names := make([]string, 0, len(declared))
	for name := range declared {
		names = append(names, name)
	}
	sort.Strings(names)

	resolved := make(map[string]string, len(declared))
	owners := make(map[string]string, len(declared))
	var problems []string
	for _, name := range names {
		value := declared[name]
		if !slugPattern.MatchString(name) {
			problems = append(problems, fmt.Sprintf("policy name %q must be %s", name, slugRule))
			continue
		}
		if value == "" {
			problems = append(problems, fmt.Sprintf("policy %s: path must not be empty", name))
			continue
		}
		path := filepath.Join(packageDir, value)
		relative, err := definitionsRelative(definitionsDir, path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("policy %s %q: %v", name, value, err))
			continue
		}
		if owner, taken := owners[relative]; taken {
			problems = append(problems, fmt.Sprintf("policies %s and %s name the same file %s", owner, name, relative))
			continue
		}
		owners[relative] = name
		data, err := os.ReadFile(path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("policy %s: %v", name, err))
			continue
		}
		if err := LintPolicy(relative, data, icsRules); err != nil {
			problems = append(problems, fmt.Sprintf("policy %s: %v", name, err))
			continue
		}
		resolved[name] = relative
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("invalid policies:\n%s", strings.Join(problems, "\n"))
	}
	return resolved, nil
}

// definitionsRelative returns path relative to the definitions directory,
// failing when it leaves it.
func definitionsRelative(definitionsDir, path string) (string, error) {
	relative, err := filepath.Rel(definitionsDir, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("must stay inside the definitions directory")
	}
	return filepath.ToSlash(relative), nil
}
