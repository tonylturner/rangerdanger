package containd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/tturner/rangerdanger/backend/internal/labs"
)

// ImportConfig sends a full JSON config to containd using the candidate/commit flow.
// Flow: POST /api/v1/config/candidate → POST /api/v1/config/commit
// Both endpoints must succeed; a staged candidate is not an applied policy.
// Before staging, appliance-owned NIC device bindings are copied from the
// running configuration by logical interface name. No legacy import is tried.
//
// The lab assumes dataplane enforcement is on — without it, containd commits the
// config to the running store but engine.ApplyRules silently no-ops (compiler is
// nil when cfg.Enforce.Enabled is false), so the kernel's nft table is never
// touched. The pre-canned substation policies and student-authored custom
// policies generally don't bother setting `dataplane.enforcement: true` because
// it's a containd implementation detail. Inject it here as a lab invariant.
// ImportConfig returns any warnings surfaced by containd's commit handler
// in the X-Containd-Warnings response header (one warning per line). These
// cover auxiliary infrastructure failures that don't abort the commit.
// Ruleset/dataplane and management service-apply failures return HTTP 500
// with recovery outcomes (restored/reapplied), not successful commits with
// warnings. A non-2xx does not guarantee the live firewall is unchanged.
// Callers must propagate errors and warnings, and recheck health after apply;
// recovery success certifies the firewall snapshot, not every side effect.
func (c *Client) ImportConfig(ctx context.Context, configJSON []byte) ([]string, error) {
	if err := labs.CheckManagementSafety(configJSON); err != nil {
		return nil, err
	}
	running, err := c.GetConfigDocument(ctx)
	if err != nil {
		return nil, fmt.Errorf("read appliance bindings: %w", err)
	}
	bound, err := mergeApplianceBindings(running, configJSON)
	if err != nil {
		return nil, fmt.Errorf("merge appliance bindings: %w", err)
	}
	patched, err := ensureEnforcementOn(bound)
	if err != nil {
		return nil, fmt.Errorf("ensure enforcement on: %w", err)
	}

	return c.importViaCandidate(ctx, patched)
}

// ensureEnforcementOn parses a containd policy JSON and returns a copy with
// `dataplane.enforcement = true`. Required because engine.ApplyRules silently
// no-ops when enforcement is off — the running config commits cleanly but no
// nft rules are actually pushed. See pkg/dp/engine/engine.go:113-129 in
// containd: compiler/applier are only constructed when cfg.Enforce.Enabled.
//
// The function preserves all other fields verbatim. If `dataplane` is missing
// it's created; if `enforcement` is already true it's a no-op (modulo
// re-marshalling); if explicitly false it's overridden (with a log at warn).
func ensureEnforcementOn(configJSON []byte) ([]byte, error) {
	if len(configJSON) == 0 {
		return configJSON, nil
	}
	var doc map[string]any
	if err := json.Unmarshal(configJSON, &doc); err != nil {
		return nil, fmt.Errorf("parse config JSON: %w", err)
	}

	dpRaw, ok := doc["dataplane"]
	dp, _ := dpRaw.(map[string]any)
	if dp == nil {
		dp = map[string]any{}
	}
	if cur, hasCur := dp["enforcement"]; hasCur {
		if b, isBool := cur.(bool); isBool && !b {
			log.Printf("containd: policy had dataplane.enforcement=false, overriding to true (lab invariant)")
		}
	}
	dp["enforcement"] = true
	doc["dataplane"] = dp
	_ = ok

	return json.Marshal(doc)
}

// importViaCandidate uses the appliance candidate/commit flow:
// 1. POST /api/v1/config/candidate — stages the config
// 2. POST /api/v1/config/commit — applies it (triggers nftables compilation)
//
// Returns warnings parsed from the commit response's X-Containd-Warnings
// header on success. Empty warnings do not certify continuing enforcement
// health. A failed apply returns an error retaining the response's recovery
// outcomes; it must never trigger an import fallback or automatic retry.
func (c *Client) importViaCandidate(ctx context.Context, configJSON []byte) ([]string, error) {
	// Stage the candidate config
	resp, err := c.doRequestWithBody(ctx, "POST", c.BaseURL+"/api/v1/config/candidate", configJSON)
	if err != nil {
		return nil, fmt.Errorf("post candidate config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("post candidate returned %d: %s", resp.StatusCode, string(body))
	}

	// Commit the staged config
	resp2, err := c.doRequestWithBody(ctx, "POST", c.BaseURL+"/api/v1/config/commit", nil)
	if err != nil {
		return nil, fmt.Errorf("commit config: %w", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK && resp2.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp2.Body)
		return nil, fmt.Errorf("commit returned %d: %s", resp2.StatusCode, string(body))
	}

	return collectWarnings(resp2.Header.Values("X-Containd-Warnings")), nil
}

// collectWarnings flattens repeated X-Containd-Warnings header values
// into a single list. Modern containd (v0.1.26+) emits one header line
// per warning via http.Header.Add — Header.Values returns each as a
// separate slice entry. Older containd builds (pre-fix) joined warnings
// with "\n" into a single header value, which Go silently mangles in
// transit; we still split on "\n" defensively to handle that case.
// Empty/whitespace-only entries are dropped. Returns nil (not []) when
// there are no warnings so callers can len()-check cleanly.
func collectWarnings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		for _, part := range strings.Split(v, "\n") {
			if t := strings.TrimSpace(part); t != "" {
				out = append(out, t)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
