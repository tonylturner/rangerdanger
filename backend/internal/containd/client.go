package containd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client communicates with the containd NGFW API.
type Client struct {
	BaseURL    string
	httpClient *http.Client
}

// Event represents a containd DPI/IDS event.
type Event struct {
	ID        string    `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`      // "connection", "modbus", "dns", "alert"
	Source    string    `json:"source"`    // Source IP
	Dest      string    `json:"dest"`      // Destination IP
	Protocol  string    `json:"protocol"`  // "modbus", "tcp", "udp", etc.
	SrcPort   int       `json:"src_port"`
	DstPort   int       `json:"dst_port"`
	Details   string    `json:"details"`   // Human-readable description
	Severity  string    `json:"severity"`  // "info", "warning", "critical"
	Zone      string    `json:"zone"`      // Source zone
}

// Session represents an active connection through the firewall.
type Session struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"`
	Dest      string    `json:"dest"`
	Protocol  string    `json:"protocol"`
	SrcPort   int       `json:"src_port"`
	DstPort   int       `json:"dst_port"`
	StartTime time.Time `json:"start_time"`
	Bytes     int64     `json:"bytes"`
	Packets   int64     `json:"packets"`
}

// HealthStatus represents the firewall health.
type HealthStatus struct {
	Status    string `json:"status"` // "healthy", "degraded", "unhealthy"
	Version   string `json:"version"`
	Uptime    int64  `json:"uptime"`
	Zones     int    `json:"zones"`
	Sessions  int    `json:"sessions"`
	EventRate int    `json:"event_rate"` // Events per second
}

// NewClient creates a containd API client.
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetHealth returns the firewall health status.
func (c *Client) GetHealth() (*HealthStatus, error) {
	resp, err := c.httpClient.Get(c.BaseURL + "/api/v1/health")
	if err != nil {
		return nil, fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("health check returned %d", resp.StatusCode)
	}

	var status HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decode health: %w", err)
	}

	return &status, nil
}

// GetEvents returns recent events, optionally filtered by time.
func (c *Client) GetEvents(since string, limit int) ([]Event, error) {
	url := fmt.Sprintf("%s/api/v1/events?limit=%d", c.BaseURL, limit)
	if since != "" {
		url += "&since=" + since
	}

	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("get events failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get events returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Events []Event `json:"events"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode events: %w", err)
	}

	return result.Events, nil
}

// GetSessions returns active sessions through the firewall.
func (c *Client) GetSessions() ([]Session, error) {
	resp, err := c.httpClient.Get(c.BaseURL + "/api/v1/sessions")
	if err != nil {
		return nil, fmt.Errorf("get sessions failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get sessions returned %d", resp.StatusCode)
	}

	var result struct {
		Sessions []Session `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode sessions: %w", err)
	}

	return result.Sessions, nil
}

// IsAvailable checks if containd is reachable.
func (c *Client) IsAvailable() bool {
	status, err := c.GetHealth()
	return err == nil && status.Status != ""
}
