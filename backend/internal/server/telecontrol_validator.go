package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// Evidence collection of the eu-iec104-telecontrol validator. Every
// address and URL comes from the generation's manifest; every source is
// read once and keeps its own error.

// validateTelecontrol collects the evidence and evaluates it.
func (s *Server) validateTelecontrol(ctx context.Context, gen *lifecycle.Generation, activePolicy string) []ValidationCheck {
	return evaluateTelecontrol(s.collectTelecontrolEvidence(ctx, gen, activePolicy))
}

func (s *Server) collectTelecontrolEvidence(ctx context.Context, gen *lifecycle.Generation, activePolicy string) telecontrolEvidence {
	ev := telecontrolEvidence{activePolicy: activePolicy, rangeStart: gen.StartedAt}
	ev.policyErr = s.runningPolicyIs(ctx, gen, telecontrolPolicy)

	var rtuErr, ccErr error
	ev.rtuIP, rtuErr = roleTeachingIP(gen, manifest.RoleRTU)
	ev.ccIP, ccErr = roleTeachingIP(gen, manifest.RoleControlCentre)
	switch {
	case rtuErr != nil:
		ev.eventsErr = rtuErr
	case ccErr != nil:
		ev.eventsErr = ccErr
	default:
		ev.events, ev.eventsErr = gen.Containd().GetEvents(ctx, containd.MaxEventLimit, gen.StartedAt)
	}

	if cc, ok := controlCentreURL(gen); ok {
		ev.ccState, ev.ccStateErr = fetchJSON[ccState](ctx, controlCentreService, cc+"/api/state")
		ev.journal, ev.journalErr = fetchJSON[ccJournal](ctx, controlCentreService, cc+"/api/journal?after=0")
	} else {
		ev.ccStateErr = fmt.Errorf("package %s has no control centre", gen.Package.ID)
		ev.journalErr = ev.ccStateErr
	}
	if device, ok := simURL(gen); ok {
		ev.audit, ev.auditErr = fetchJSON[deviceAudit](ctx, simService, device+"/api/audit?after=0")
		ev.device, ev.deviceErr = fetchJSON[deviceTruth](ctx, simService, device+"/api/state")
	} else {
		ev.deviceErr = fmt.Errorf("package %s has no sim device", gen.Package.ID)
		ev.auditErr = ev.deviceErr
	}
	return ev
}

// runningPolicyIs reports, as an error, whether containd's running firewall
// differs from the named policy file.
func (s *Server) runningPolicyIs(ctx context.Context, gen *lifecycle.Generation, name string) error {
	path, err := s.policyPath(gen.Package, name)
	if err != nil {
		return err
	}
	data, err := readPolicyJSONWithRetry(path)
	if err != nil {
		return err
	}
	want, err := containd.FirewallHashFromBytes(data)
	if err != nil {
		return err
	}
	got, err := gen.Containd().GetFirewallHash(ctx)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("running firewall hash %s, policy file hash %s", got, want)
	}
	return nil
}

// roleTeachingIP is the address of the role's service on its one teaching
// network (a manifest network with a zone).
func roleTeachingIP(gen *lifecycle.Generation, role manifest.Role) (string, error) {
	svc, err := roleService(gen, role)
	if err != nil {
		return "", err
	}
	zones := map[string]bool{}
	for _, network := range gen.Manifest.Networks {
		if network.Zone != "" {
			zones[network.Key] = true
		}
	}
	var ips []string
	for _, iface := range svc.Interfaces {
		if zones[iface.Network] {
			ips = append(ips, iface.IPv4)
		}
	}
	if len(ips) != 1 {
		return "", fmt.Errorf("service %s has %d teaching addresses, want 1", svc.Key, len(ips))
	}
	return ips[0], nil
}

// fetchJSON GETs url, which must answer a JSON object of shape T.
func fetchJSON[T any](ctx context.Context, service, url string) (*T, error) {
	body, err := fetchUpstreamObject(ctx, service, url)
	if err != nil {
		return nil, err
	}
	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("%s: decode %s: %w", service, url, err)
	}
	return &out, nil
}
