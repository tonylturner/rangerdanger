package containd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// GetFirewallHash returns a stable SHA-256 hash of the firewall sub-
// document of containd's running config. Used by the server's policy
// observer to detect when a student commits a policy directly in
// containd's CLI/UI (the in-memory activeConfig only tracks
// backend-initiated applies, so we have to observe out-of-band
// changes by hashing the canonicalised running config).
//
// Canonical form: parse to map, re-marshal with json.Marshal so map
// keys come out in stable sorted order (Go's encoding/json sorts
// map keys when serialising). Hash the resulting bytes. Two
// committed configs that differ only in whitespace / key order
// produce the same hash, but two that differ in any rule field
// produce different hashes.
func (c *Client) GetFirewallHash(ctx context.Context) (string, error) {
	resp, err := c.doRequest(ctx, "GET", c.BaseURL+"/api/v1/config")
	if err != nil {
		return "", fmt.Errorf("get config failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("get config returned %d: %s", resp.StatusCode, string(body))
	}

	// Pull just the firewall key out of the full config. Hashing
	// the whole document would also include things like
	// interfaces / DHCP / NAT that students don't (and shouldn't)
	// touch in this lab — keeping the hash scoped to the firewall
	// sub-document avoids spurious manual-custom flips from
	// unrelated containd activity.
	var doc map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return "", fmt.Errorf("decode config: %w", err)
	}
	fw, ok := doc["firewall"]
	if !ok {
		// No firewall block in the running config — treat as a
		// stable zero-state so the observer doesn't keep flipping.
		return canonicalHash(struct{}{}), nil
	}
	return canonicalHash(fw), nil
}

// canonicalHash returns SHA-256 of v marshalled to canonical JSON
// (sorted map keys via encoding/json). Exported helper kept private
// so other packages can't accidentally hash a non-canonical form.
func canonicalHash(v any) string {
	buf, err := json.Marshal(v)
	if err != nil {
		// json.Marshal only fails on truly weird inputs (cycles,
		// unsupported types). Hash an empty marker so the
		// observer at least doesn't crash on garbage.
		return "ERR-MARSHAL"
	}
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:])
}

// FirewallHashFromBytes returns the canonical hash of an already-
// serialised containd config (or just its firewall sub-document).
// Used by the server's apply handlers to record what was applied at
// the moment of apply, so the observer has a baseline to compare
// against. Accepts either a full-config JSON or a bare firewall-
// object JSON; auto-unwraps if the top level has a "firewall" key.
func FirewallHashFromBytes(b []byte) (string, error) {
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		return "", fmt.Errorf("decode bytes: %w", err)
	}
	if fw, ok := raw["firewall"]; ok {
		return canonicalHash(fw), nil
	}
	return canonicalHash(raw), nil
}
