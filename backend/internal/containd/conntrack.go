package containd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// ErrSessionNotFound reports a kill whose tuple matches no conntrack entry.
var ErrSessionNotFound = errors.New("conntrack entry not found")

// Session is one containd conntrack entry with its ports as integers.
// containd omits empty fields, so a port or state it did not report is nil.
type Session struct {
	Proto   string  `json:"proto"`
	Src     string  `json:"src"`
	Dst     string  `json:"dst"`
	Sport   *int    `json:"sport"`
	Dport   *int    `json:"dport"`
	State   *string `json:"state"`
	Timeout int     `json:"timeout"`
}

// SessionTuple is the original-direction 5-tuple containd deletes. Linux
// encodes both ports even when zero, so zero is a port, not a wildcard.
type SessionTuple struct {
	Proto string `json:"proto"`
	Src   string `json:"src"`
	Dst   string `json:"dst"`
	Sport int    `json:"sport"`
	Dport int    `json:"dport"`
}

// conntrackEntry is containd's wire shape (pkg/dp/conntrack.Entry): ports
// are decimal strings.
type conntrackEntry struct {
	Proto       string `json:"proto"`
	State       string `json:"state"`
	Src         string `json:"src"`
	Dst         string `json:"dst"`
	Sport       string `json:"sport"`
	Dport       string `json:"dport"`
	TimeoutSecs int    `json:"timeoutSecs"`
}

// ListSessions returns up to limit entries of the firewall's conntrack
// table (GET /api/v1/conntrack, a bare JSON array).
func (c *Client) ListSessions(ctx context.Context, limit int) ([]Session, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("%s/api/v1/conntrack?limit=%d", c.BaseURL, limit))
	if err != nil {
		return nil, fmt.Errorf("list conntrack: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("list conntrack returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var entries []conntrackEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode conntrack: %w", err)
	}
	sessions := make([]Session, 0, len(entries))
	for _, entry := range entries {
		sport, err := parsePort(entry.Sport)
		if err != nil {
			return nil, fmt.Errorf("conntrack entry %s %s->%s: sport: %w", entry.Proto, entry.Src, entry.Dst, err)
		}
		dport, err := parsePort(entry.Dport)
		if err != nil {
			return nil, fmt.Errorf("conntrack entry %s %s->%s: dport: %w", entry.Proto, entry.Src, entry.Dst, err)
		}
		session := Session{Proto: entry.Proto, Src: entry.Src, Dst: entry.Dst, Sport: sport, Dport: dport, Timeout: entry.TimeoutSecs}
		if entry.State != "" {
			state := entry.State
			session.State = &state
		}
		sessions = append(sessions, session)
	}
	return sessions, nil
}

// parsePort reads containd's string port; empty means not reported.
func parsePort(value string) (*int, error) {
	if value == "" {
		return nil, nil
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 0 || port > 65535 {
		return nil, fmt.Errorf("invalid port %q", value)
	}
	return &port, nil
}

// SessionKillResult records containd's per-step revocation/deletion outcome.
// Closure describes packet handling, not confirmed endpoint closure.
type SessionKillResult struct {
	Scope               string `json:"scope"`
	Matched             int    `json:"matched"`
	Deleted             int    `json:"deleted"`
	ConntrackDeleted    bool   `json:"conntrackDeleted"`
	DeleteError         string `json:"deleteError,omitempty"`
	RevocationInstalled bool   `json:"revocationInstalled"`
	RevocationError     string `json:"revocationError,omitempty"`
	BlockedUntil        string `json:"blockedUntil,omitempty"`
	Closure             string `json:"closure"`
}

// KillSession revokes the exact tuple in both directions for the default
// 60 seconds, then deletes its conntrack entry (POST /api/v1/conntrack/kill).
// TCP resets are signalled on subsequent matching packets, not injected;
// a fresh source port is allowed by the revocation but still meets policy.
// Partial failures return both the result and an error. A 502 containing
// "not found" is returned as ErrSessionNotFound.
func (c *Client) KillSession(ctx context.Context, tuple SessionTuple) (*SessionKillResult, error) {
	body, err := json.Marshal(tuple)
	if err != nil {
		return nil, err
	}
	resp, err := c.doRequestWithBody(ctx, http.MethodPost, c.BaseURL+"/api/v1/conntrack/kill", body)
	if err != nil {
		return nil, fmt.Errorf("kill conntrack entry: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		var result SessionKillResult
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			return nil, fmt.Errorf("decode conntrack kill result: %w", err)
		}
		var failures []string
		if !result.ConntrackDeleted || result.DeleteError != "" {
			failures = append(failures, fmt.Sprintf("conntrack deletion failed (conntrackDeleted=%t, deleteError=%q)", result.ConntrackDeleted, result.DeleteError))
		}
		if !result.RevocationInstalled || result.RevocationError != "" {
			failures = append(failures, fmt.Sprintf("session revocation failed (revocationInstalled=%t, revocationError=%q)", result.RevocationInstalled, result.RevocationError))
		}
		if len(failures) > 0 {
			return &result, fmt.Errorf("kill conntrack entry: %s", strings.Join(failures, "; "))
		}
		return &result, nil
	}
	detail, _ := io.ReadAll(resp.Body)
	text := strings.TrimSpace(string(detail))
	if resp.StatusCode == http.StatusBadGateway && strings.Contains(text, "not found") {
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, text)
	}
	return nil, fmt.Errorf("kill conntrack entry returned %d: %s", resp.StatusCode, text)
}
