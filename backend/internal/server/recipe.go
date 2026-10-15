package server

import (
	"context"
	"fmt"
	"net"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

// packageRecipe is the curriculum-specific operations of one package: how
// the workshop reset restores its devices, the probes that prove the
// dataplane, the traffic a lab generates, and the US-shaped panels it
// supports. Recipes name topology nodes and manifest networks; the
// generation's manifest resolves them to containers and addresses. A
// package without a recipe, or a recipe without an operation, answers that
// operation with 404 "not supported by this package". Named policies are
// package data (package.yml `policies`), not recipe code.
type packageRecipe struct {
	// simDevices binds the simulation vocabulary to authored topology nodes.
	simDevices map[string]string
	// commandExecutor is the package's device-control transport. It never
	// falls back to another package's devices.
	commandExecutor func(*Server, context.Context, *lifecycle.Generation, *packageRecipe, string, string, string, *float64) StepActionResult
	// sessionCuts binds named topology flows to their wire networks.
	sessionCuts map[string]sessionCutSpec
	// resetCommands are RTAC device commands the reset replays.
	resetCommands []deviceCommand
	// resetSim makes the reset restore the sim device
	// (the service with a sim endpoint) through its reset.
	resetSim bool
	canary   canaryProbe
	// captureFilter is the tcpdump filter of /api/pcap/start; empty
	// captures everything.
	captureFilter string
	// validation is the validation report's probe matrix; nil when the
	// package has no report.
	validation []validationProbeSpec
	// validationCapture names the devices the validation report's PCAP
	// watches, and the network they sit on.
	validationCapture captureSpec
	// traffic is the traffic generator's flows; nil when it has none.
	traffic []trafficTarget
	// testSuite marks a package whose scenarios /workshop/test-suite runs.
	testSuite bool
	// compare is the zone-pair table of /firewall/compare; nil when the
	// package has none.
	compare *compareSpec
	// staticRules answer /firewall/rules while containd is unreachable;
	// without them the route fails.
	staticRules []containd.ZoneRuleSummary
	// workshopName overrides the topology name in /workshop/status.
	workshopName string
	// policyLabels describe named policies in step results; a policy
	// without one is shown by name.
	policyLabels map[string]string
}

// deviceCommand is one RTAC device command; value is its argument, if any.
type deviceCommand struct {
	device, command, desc string
	value                 *float64
}

// canaryProbe is the TCP connect whose verdict shows the dataplane enforces
// a canned policy: allowed under allowUnder, denied under denyUnder.
type canaryProbe struct {
	fromNode, label       string
	toNode, network       string
	port                  int
	allowUnder, denyUnder string
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

// zonePair is a containd source and destination zone.
type zonePair struct{ src, dst string }

// compareSpec is the policy comparison's rows, in order, and the labels of
// their zones.
type compareSpec struct {
	weakPolicy, improvedPolicy string
	pairs                      []zonePair
	labels                     map[string]string
}

// label names a zone pair for people; unknown zones keep their name.
func (c *compareSpec) label(src, dst string) string {
	s := c.labels[src]
	if s == "" {
		s = src
	}
	d := c.labels[dst]
	if d == "" {
		d = dst
	}
	return s + " → " + d
}

var packageRecipes = map[string]*packageRecipe{usPackageID: usRecipe, euPackageID: euRecipe}

// recipeFor returns the recipe of the generation's package.
func recipeFor(gen *lifecycle.Generation) (*packageRecipe, error) {
	recipe, ok := packageRecipes[gen.Package.ID]
	if !ok {
		return nil, fmt.Errorf("package %s has no workshop recipe", gen.Package.ID)
	}
	return recipe, nil
}

// supportedRecipe returns the request range's recipe when it supports an
// operation, and otherwise answers 404 "not supported by this package".
func supportedRecipe(c *gin.Context, supports func(*packageRecipe) bool) (*packageRecipe, bool) {
	recipe, err := recipeFor(rangeOf(c))
	if err != nil || !supports(recipe) {
		notSupported(c)
		return nil, false
	}
	return recipe, true
}

// policyLabel describes a named policy in step results.
func (r *packageRecipe) policyLabel(name string) string {
	if label := r.policyLabels[name]; label != "" {
		return label
	}
	return name
}

// defaultPolicy is the policy every range of the package starts on, the
// one the workshop reset restores.
func defaultPolicy(gen *lifecycle.Generation) (string, error) {
	name, ok := gen.Package.PolicyName(gen.Package.FirewallConfigPath)
	if !ok {
		return "", fmt.Errorf("package %s default policy %s is not a named policy", gen.Package.ID, gen.Package.FirewallConfigPath)
	}
	return name, nil
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

// resolveTraffic resolves the recipe's traffic against the generation.
func (r *packageRecipe) resolveTraffic(gen *lifecycle.Generation) ([]resolvedTraffic, error) {
	targets := make([]resolvedTraffic, 0, len(r.traffic))
	for _, t := range r.traffic {
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
