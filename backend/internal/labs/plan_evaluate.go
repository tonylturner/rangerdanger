package labs

import (
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
)

// Verdict is an L4 connection outcome, not application health or DPI evidence.
type Verdict string

const (
	VerdictAllow Verdict = "allow"
	VerdictDeny  Verdict = "deny"
)

// PlanTuple contains only the wire-visible fields used by L4 policy.
type PlanTuple struct {
	SrcZone  string
	DstZone  string
	SrcIP    string
	DstIP    string
	Protocol string
	Port     int
}

// PlanRoute separates firewall reachability from ordered rule evaluation.
type PlanRoute struct {
	Tuple         PlanTuple
	SourceNetwork string
	Bridged       bool
}

// Evaluate applies ordered first-match policy to a routed tuple. Empty selector
// lists match anything. ICS and logging never affect an L4 connection verdict.
func Evaluate(rules []PlanRule, defaultAction string, tuple PlanTuple) Verdict {
	for _, rule := range rules {
		if !planZoneMatches(rule.SourceZones, tuple.SrcZone) ||
			!planZoneMatches(rule.DestZones, tuple.DstZone) ||
			!planAddressMatches(rule.Sources, tuple.SrcIP) ||
			!planAddressMatches(rule.Destinations, tuple.DstIP) ||
			!planProtocolMatches(rule.Protocols, tuple.Protocol, tuple.Port) {
			continue
		}
		return planVerdict(rule.Action)
	}
	return planVerdict(defaultAction)
}

func planVerdict(action string) Verdict {
	if action == "ALLOW" {
		return VerdictAllow
	}
	return VerdictDeny
}

func planZoneMatches(zones []string, zone string) bool {
	return len(zones) == 0 || slices.Contains(zones, zone)
}

func planAddressMatches(selectors []string, value string) bool {
	if len(selectors) == 0 {
		return true
	}
	ip, err := netip.ParseAddr(value)
	if err != nil {
		return false
	}
	for _, selector := range selectors {
		if address, err := netip.ParseAddr(selector); err == nil && address == ip {
			return true
		}
		if prefix, err := netip.ParsePrefix(selector); err == nil && prefix.Contains(ip) {
			return true
		}
	}
	return false
}

func planProtocolMatches(protocols []PlanProtocol, name string, port int) bool {
	if len(protocols) == 0 {
		return true
	}
	for _, protocol := range protocols {
		if protocol.Name != name {
			continue
		}
		if protocol.Port == "" {
			return true
		}
		low, high, err := planPortRange(protocol.Port)
		if err == nil && port >= low && port <= high {
			return true
		}
	}
	return false
}

func planPortRange(value string) (int, int, error) {
	parts := strings.Split(value, "-")
	if len(parts) > 2 {
		return 0, 0, fmt.Errorf("invalid port %q", value)
	}
	ports := make([]int, len(parts))
	for i, part := range parts {
		if part == "" || strings.IndexFunc(part, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			return 0, 0, fmt.Errorf("invalid port %q", value)
		}
		port, err := strconv.Atoi(part)
		if err != nil || port < 1 || port > 65535 {
			return 0, 0, fmt.Errorf("invalid port %q", value)
		}
		ports[i] = port
	}
	low, high := ports[0], ports[len(ports)-1]
	if low > high {
		return 0, 0, fmt.Errorf("invalid port range %q", value)
	}
	return low, high, nil
}

func planNetworkPrefix(network NetworkYAML) (netip.Prefix, error) {
	value := network.Subnet
	if value == "" {
		value = network.CIDR
	}
	return netip.ParsePrefix(value)
}

// ResolvePlanTuple derives the source network from the source's primary IP.
// Sharing an attachment is insufficient: the multihomed RTAC routes field
// traffic through the firewall from its OT address. Same-source-network traffic
// is bridged instead and must not be attributed to firewall enforcement.
func ResolvePlanTuple(pkg Package, proof PlanProof) (PlanRoute, error) {
	var source, target *NodeYAML
	for i := range pkg.Template.Nodes {
		node := &pkg.Template.Nodes[i]
		if node.ID == proof.From {
			source = node
		}
		if node.ID == proof.To {
			target = node
		}
	}
	if source == nil || target == nil {
		return PlanRoute{}, fmt.Errorf("proof %s: unknown source or destination node", proof.ID)
	}
	ip, err := netip.ParseAddr(source.IP)
	if err != nil {
		return PlanRoute{}, fmt.Errorf("proof %s: invalid source IP %q", proof.ID, source.IP)
	}
	var sourceNetworks []NetworkYAML
	var destination *NetworkYAML
	for i := range pkg.Template.Networks {
		network := &pkg.Template.Networks[i]
		prefix, err := planNetworkPrefix(*network)
		if err != nil {
			return PlanRoute{}, fmt.Errorf("network %s: invalid subnet", network.ID)
		}
		if prefix.Contains(ip) {
			sourceNetworks = append(sourceNetworks, *network)
		}
		if network.ID == proof.Network {
			destination = network
		}
	}
	if len(sourceNetworks) != 1 {
		return PlanRoute{}, fmt.Errorf("proof %s: source IP must match exactly one topology network, got %d", proof.ID, len(sourceNetworks))
	}
	src := sourceNetworks[0]
	if !slices.Contains(source.Networks, src.ID) || src.Zone == "" {
		return PlanRoute{}, fmt.Errorf("proof %s: source IP network is not an attached teaching zone", proof.ID)
	}
	if destination == nil || destination.Zone == "" || !slices.Contains(target.Networks, proof.Network) {
		return PlanRoute{}, fmt.Errorf("proof %s: unknown or unattached destination network %s", proof.ID, proof.Network)
	}
	// Topology owns one primary address per node. Do not invent a secondary
	// address for a multihomed destination.
	dstIP, err := netip.ParseAddr(target.IP)
	prefix, prefixErr := planNetworkPrefix(*destination)
	if err != nil || prefixErr != nil || !prefix.Contains(dstIP) {
		return PlanRoute{}, fmt.Errorf("proof %s: destination IP is not in network %s", proof.ID, proof.Network)
	}
	return PlanRoute{
		Tuple: PlanTuple{
			SrcZone: src.Zone, DstZone: destination.Zone,
			SrcIP: source.IP, DstIP: target.IP, Protocol: "tcp", Port: proof.Port,
		},
		SourceNetwork: src.ID,
		Bridged:       src.ID == destination.ID,
	}, nil
}

func evaluatePlanRoute(rules []PlanRule, defaultAction string, route PlanRoute) Verdict {
	if route.Bridged {
		return VerdictAllow
	}
	return Evaluate(rules, defaultAction, route.Tuple)
}
