package containd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/tturner/rangerdanger/backend/internal/labs"
)

// ImportConfig stages against the running revision, commits against the candidate
// revision, then waits for the committed application's protection readiness.
// It never retries a conflict or recommits a failed application.
func (c *Client) ImportConfig(ctx context.Context, configJSON []byte) (*ApplyResult, error) {
	if err := labs.CheckManagementSafety(configJSON); err != nil {
		return nil, err
	}
	running, revision, err := c.getConfigDocument(ctx)
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

	return c.importViaCandidate(ctx, patched, revision)
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

	dpRaw := doc["dataplane"]
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

	return json.Marshal(doc)
}

func (c *Client) importViaCandidate(ctx context.Context, configJSON []byte, revision string) (*ApplyResult, error) {
	resp, err := c.writeRevision(ctx, "/api/v1/config/candidate", configJSON, revision)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	revision = resp.Header.Get("X-Containd-Revision")
	resp2, err := c.writeRevision(ctx, "/api/v1/config/commit", nil, revision)
	if err != nil {
		return nil, err
	}
	defer resp2.Body.Close()
	var applied RuntimeApplyResponse
	if err := json.NewDecoder(resp2.Body).Decode(&applied); err != nil {
		return nil, fmt.Errorf("decode commit: %w", err)
	}
	if applied.ApplyID == "" || applied.Status == "" {
		return nil, fmt.Errorf("commit missing status or applyId")
	}
	result := &ApplyResult{Warnings: collectWarnings(resp2.Header.Values("X-Containd-Warnings"))}
	receipt, err := c.waitProtection(ctx, applied)
	result.Receipt = receipt
	c.rememberApply(receipt)
	if receipt.Warning != "" {
		result.Warnings = append(result.Warnings, receipt.Warning)
	}
	return result, err
}

// ConfigWriteError preserves upstream precondition failures without retrying.
type ConfigWriteError struct {
	Status    int
	Body      string
	Operation string
}

func (e *ConfigWriteError) Error() string {
	return fmt.Sprintf("%s returned %d: %s", e.Operation, e.Status, e.Body)
}

// ConfigConflictCode and ConfigConflictMessage are the backend's conflict contract.
const ConfigConflictCode = "containd_config_conflict"
const ConfigConflictMessage = "containd configuration changed since it was read; review it and apply again"

func (c *Client) writeRevision(ctx context.Context, path string, body []byte, revision string) (*http.Response, error) {
	if revision == "" {
		return nil, fmt.Errorf("%s: missing X-Containd-Revision in preceding response", path)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Containd-If-Revision", revision)
	resp, err := c.send(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		operation := "post candidate"
		if strings.HasSuffix(path, "/commit") {
			operation = "commit"
		}
		return nil, &ConfigWriteError{Status: resp.StatusCode, Body: string(data), Operation: operation}
	}
	return resp, nil
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
