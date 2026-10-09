package containd

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

// ImportConfig sends a full JSON config to containd using the candidate/commit flow.
// Flow: POST /api/v1/config/candidate → POST /api/v1/config/commit
// Falls back to legacy /api/v1/config/import if candidate endpoint is unavailable.
//
// The lab assumes dataplane enforcement is on — without it, containd commits the
// config to the running store but engine.ApplyRules silently no-ops (compiler is
// nil when cfg.Enforce.Enabled is false), so the kernel's nft table is never
// touched. The pre-canned substation policies and student-authored custom
// policies generally don't bother setting `dataplane.enforcement: true` because
// it's a containd implementation detail. Inject it here as a lab invariant.
// ImportConfig returns any warnings surfaced by containd's commit handler
// in the X-Containd-Warnings response header (one warning per line). These
// cover infrastructure failures that don't abort the commit — e.g. an nft
// apply that hit "operation not permitted" because the container lacks
// NET_ADMIN, or a routing reconfigure that partially failed. Body is
// always {"status":"committed"}; without parsing the header these failures
// are invisible to the lab UI. Callers should propagate warnings up so a
// student sees "config committed but X didn't apply" instead of a green
// 200 hiding broken enforcement.
func (c *Client) ImportConfig(configJSON []byte) ([]string, error) {
	patched, err := ensureEnforcementOn(configJSON)
	if err != nil {
		return nil, fmt.Errorf("ensure enforcement on: %w", err)
	}

	// Try the preferred candidate/commit flow first
	if warnings, err := c.importViaCandidate(patched); err == nil {
		return warnings, nil
	} else {
		// Fall back to legacy import endpoint
		log.Printf("containd: candidate/commit flow failed (%v), falling back to /config/import", err)
		return c.importLegacy(patched)
	}
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
// header. Empty slice means "fully clean apply"; non-empty means commit
// returned 200 but at least one step in applyRunningConfig surfaced a
// warning (typical: nft apply failed, interface reconcile partial). The
// caller decides whether to treat warnings as soft failures.
func (c *Client) importViaCandidate(configJSON []byte) ([]string, error) {
	// Stage the candidate config
	resp, err := c.doRequestWithBody("POST", c.BaseURL+"/api/v1/config/candidate", configJSON)
	if err != nil {
		return nil, fmt.Errorf("post candidate config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("candidate endpoint not available (404)")
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("post candidate returned %d: %s", resp.StatusCode, string(body))
	}

	// Commit the staged config
	resp2, err := c.doRequestWithBody("POST", c.BaseURL+"/api/v1/config/commit", nil)
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

// importLegacy uses the older /api/v1/config/import endpoint.
// The legacy endpoint doesn't run applyRunningConfig and therefore can't
// produce warnings — always returns nil for the warnings slice.
func (c *Client) importLegacy(configJSON []byte) ([]string, error) {
	resp, err := c.doRequestWithBody("POST", c.BaseURL+"/api/v1/config/import", configJSON)
	if err != nil {
		return nil, fmt.Errorf("import config request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("import config returned %d: %s", resp.StatusCode, string(body))
	}

	return nil, nil
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
