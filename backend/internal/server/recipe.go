package server

import (
	"context"
	"fmt"
	"slices"
	"strconv"

	"github.com/tturner/rangerdanger/backend/internal/labs"

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
	check      func(*Server, context.Context, *lifecycle.Generation, map[string]any) []StepActionResult
	// commandExecutor is the package's device-control transport. It never
	// falls back to another package's devices.
	commandExecutor func(*Server, context.Context, *lifecycle.Generation, *packageRecipe, string, string, string, *float64) StepActionResult
	// sessionCuts binds named topology flows to their wire networks.
	sessionCuts map[string]sessionCutSpec
	// resetCommands are RTAC device commands the reset replays.
	resetCommands []deviceCommand
	// resetRestartNodes ends package sessions after policy and device reset.
	// These are topology identities, never container names.
	resetRestartNodes []string
	// resetTruth restores simulation truth and waits for consistent delivery.
	resetTruth func(context.Context, *lifecycle.Generation, *packageRecipe) []resetAction
	canary     canaryProbe
	// captureFilter is the tcpdump filter of /api/pcap/start; empty
	// captures everything.
	captureFilter string
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
}

// deviceCommand is one RTAC device command; value is its argument, if any.
type deviceCommand struct {
	device, command, desc string
	value                 *float64
}

// canaryProbe is the connect-only tuple used to check reconciliation against
// the hash-verified committed policy, independent of its display label.
type canaryProbe struct {
	fromNode, label string
	toNode, network string
	port            int
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

var packageRecipes = map[string]*packageRecipe{usPackageID: usRecipe, euPackageID: euRecipe, sraPackageID: sraRecipe}

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

// defaultPolicy is the policy every range of the package starts on, the
// one the workshop reset restores.
func defaultPolicy(gen *lifecycle.Generation) (string, error) {
	name, ok := gen.Package.PolicyName(gen.Package.FirewallConfigPath)
	if !ok {
		return "", fmt.Errorf("package %s default policy %s is not a named policy", gen.Package.ID, gen.Package.FirewallConfigPath)
	}
	return name, nil
}

// resolveProof selects explicit activity membership, or the whole universe for an empty id.
func resolveProof(gen *lifecycle.Generation, expectations []labs.PlanExpectation, slot string, againstPlan bool) ([]validationProbe, error) {
	probes := []validationProbe{}
	if slot != "" {
		if _, ok := gen.Package.Course.Activity(slot); !ok {
			return nil, fmt.Errorf("unknown course activity %s", slot)
		}
	}
	for _, expectation := range expectations {
		if slot != "" && !slices.Contains(expectation.Activities, slot) {
			continue
		}
		from, err := nodeService(gen, expectation.From)
		if err != nil {
			return nil, err
		}
		route, err := labs.ResolvePlanTuple(gen.Package, labs.PlanProof{ID: expectation.ID, From: expectation.From, To: expectation.To, Network: expectation.Network, Port: expectation.Port})
		if err != nil {
			return nil, err
		}
		label := ""
		for _, node := range gen.Package.Template.Nodes {
			if node.ID == expectation.From {
				label = node.Name
			}
		}
		if label == "" {
			return nil, fmt.Errorf("source %s has no display name", expectation.From)
		}
		dst, err := nodeIP(gen, expectation.To, expectation.Network)
		if err != nil {
			return nil, err
		}
		expected := expectation.HardenedVerdict
		if againstPlan {
			expected = expectation.PlanVerdict
		}
		note := fmt.Sprintf("%s (topology source %s on %s)", expectation.Note, route.Tuple.SrcIP, route.SourceNetwork)
		probes = append(probes, validationProbe{Src: from.Container, SrcLabel: label, Dst: dst, Port: strconv.Itoa(expectation.Port), Expected: string(expected), Note: note, Category: expectation.Category,
			ID: expectation.ID, Route: route})
	}
	return probes, nil
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
