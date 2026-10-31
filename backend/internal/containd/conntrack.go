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

// KillSession deletes the conntrack entry with the exact tuple (POST
// /api/v1/conntrack/kill). It does not reset the connection: the next
// packet of the flow is new and meets the active policy. containd reports
// a missing entry as 502 with the kernel's "not found", which is returned
// as ErrSessionNotFound.
func (c *Client) KillSession(ctx context.Context, tuple SessionTuple) error {
	body, err := json.Marshal(tuple)
	if err != nil {
		return err
	}
	resp, err := c.doRequestWithBody(ctx, http.MethodPost, c.BaseURL+"/api/v1/conntrack/kill", body)
	if err != nil {
		return fmt.Errorf("kill conntrack entry: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	detail, _ := io.ReadAll(resp.Body)
	text := strings.TrimSpace(string(detail))
	if resp.StatusCode == http.StatusBadGateway && strings.Contains(text, "not found") {
		return fmt.Errorf("%w: %s", ErrSessionNotFound, text)
	}
	return fmt.Errorf("kill conntrack entry returned %d: %s", resp.StatusCode, text)
}
