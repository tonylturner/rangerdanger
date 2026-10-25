package containd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
)

// conntrackEntry is containd's wire shape; ports are decimal strings.
type conntrackEntry struct {
	Proto string `json:"proto"`
	Src   string `json:"src"`
	Dst   string `json:"dst"`
	Sport string `json:"sport"`
	Dport string `json:"dport"`
}

// GetSessions adapts containd's bare conntrack array to the portal's session
// contract. Conntrack does not report start time or byte/packet counters, so
// those fields remain zero. IDs identify the original-direction tuple, not a
// connection's lifetime. A missing port (for example ICMP) is represented as zero.
func (c *Client) GetSessions() ([]Session, error) {
	resp, err := c.doRequest(http.MethodGet, c.BaseURL+"/api/v1/conntrack")
	if err != nil {
		return nil, fmt.Errorf("get sessions failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get sessions returned %d", resp.StatusCode)
	}
	var entries []conntrackEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode conntrack: %w", err)
	}
	sessions := make([]Session, 0, len(entries))
	for _, entry := range entries {
		sport, err := conntrackPort(entry.Sport)
		if err != nil {
			return nil, fmt.Errorf("conntrack source: %w", err)
		}
		dport, err := conntrackPort(entry.Dport)
		if err != nil {
			return nil, fmt.Errorf("conntrack destination: %w", err)
		}
		sessions = append(sessions, Session{
			ID:       fmt.Sprintf("%s:%s:%d:%s:%d", entry.Proto, entry.Src, sport, entry.Dst, dport),
			Source:   entry.Src,
			Dest:     entry.Dst,
			Protocol: entry.Proto,
			SrcPort:  sport,
			DstPort:  dport,
		})
	}
	return sessions, nil
}

func conntrackPort(raw string) (int, error) {
	if raw == "" {
		return 0, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 0 || port > 65535 {
		return 0, fmt.Errorf("invalid port %q", raw)
	}
	return port, nil
}
