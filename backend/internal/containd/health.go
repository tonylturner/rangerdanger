package containd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// HealthStatus represents the firewall health.
type HealthStatus struct {
	Status    string `json:"status"` // "healthy", "degraded", "unhealthy"
	Version   string `json:"version"`
	Uptime    int64  `json:"uptime"`
	Zones     int    `json:"zones"`
	Sessions  int    `json:"sessions"`
	EventRate int    `json:"event_rate"` // Events per second
}

// GetHealth returns the firewall health status.
func (c *Client) GetHealth() (*HealthStatus, error) {
	resp, err := c.doRequest("GET", c.BaseURL+"/api/v1/health")
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

// IsAvailable checks if containd is reachable.
func (c *Client) IsAvailable() bool {
	status, err := c.GetHealth()
	return err == nil && status.Status != ""
}

// WaitReady polls containd health until it responds or the context is cancelled.
func (c *Client) WaitReady(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("containd not ready after %s: %w", timeout, ctx.Err())
		case <-ticker.C:
			if c.IsAvailable() {
				return nil
			}
		}
	}
}
