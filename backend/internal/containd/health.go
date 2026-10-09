package containd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// HealthStatus mirrors containd's GET /api/v1/health body. The endpoint
// is a liveness probe of the management plane: it always answers
// status "ok" while the API is up and says nothing about the engine.
type HealthStatus struct {
	Status    string    `json:"status"`    // "ok"
	Component string    `json:"component"` // "mgmt"
	Build     string    `json:"build"`     // containd build version
	Time      time.Time `json:"time"`      // containd's clock, UTC
}

// GetHealth returns the firewall health status.
func (c *Client) GetHealth(ctx context.Context) (*HealthStatus, error) {
	resp, err := c.doRequest(ctx, "GET", c.BaseURL+"/api/v1/health")
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
func (c *Client) IsAvailable(ctx context.Context) bool {
	status, err := c.GetHealth(ctx)
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
			if c.IsAvailable(ctx) {
				return nil
			}
		}
	}
}
