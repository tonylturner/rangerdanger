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
// status "ok" while the API is up, even when forwarding or binding
// diagnostics in the same body report degraded operation.
type HealthStatus struct {
	Status            string                   `json:"status"`    // "ok"
	Component         string                   `json:"component"` // "mgmt"
	Build             string                   `json:"build"`     // containd build version
	Time              time.Time                `json:"time"`      // containd's clock, UTC
	IPv4Forwarding    string                   `json:"ipv4Forwarding"`
	ManagementBinding *ManagementBindingStatus `json:"managementBinding"`
	// Diagnostic pointers distinguish an omitted observation from a healthy
	// zero value. The server embeds these in /api/firewall/health unchanged.
	ManagementRecovery *HealthDiagnostic     `json:"managementRecovery,omitempty"`
	Enforcement        *HealthDiagnostic     `json:"enforcement,omitempty"`
	ICSEnforcement     *ICSEnforcementStatus `json:"icsEnforcement,omitempty"`
}

// HealthDiagnostic preserves an appliance-owned operational state and reason.
// Neither recovery nor enforcement is a management liveness/readiness gate.
type HealthDiagnostic struct {
	State  string `json:"state"`
	Reason string `json:"reason"`
}

// ICSEnforcementStatus summarizes applied ICS coverage, not management liveness.
type ICSEnforcementStatus struct {
	State        string `json:"state"`
	Enforcing    int    `json:"enforcing"`
	Partial      int    `json:"partial"`
	NotEnforcing int    `json:"notEnforcing"`
	Monitoring   int    `json:"monitoring"`
}

// ManagementBindingStatus describes the appliance's management NIC binding.
type ManagementBindingStatus struct {
	State         string `json:"state"`
	RecoveryPorts []int  `json:"recoveryPorts,omitempty"`
}

// GetManagementReservation reads authenticated operational diagnostics. The
// reservation authority is appliance-owned; preserve its fault/history objects
// rather than inventing a backend schema for them.
func (c *Client) GetManagementReservation(ctx context.Context) (json.RawMessage, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, c.BaseURL+"/api/v1/system/management-reservation")
	if err != nil {
		return nil, fmt.Errorf("management reservation: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("management reservation returned %d", resp.StatusCode)
	}
	var status map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decode management reservation: %w", err)
	}
	if _, ok := status["active"]; !ok {
		return nil, fmt.Errorf("management reservation missing active status")
	}
	return json.Marshal(status)
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

// WaitReady polls containd health, first at once and then every two
// seconds, until a supported build responds or the context is cancelled.
func (c *Client) WaitReady(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		status, err := c.GetHealth(ctx)
		if err == nil && status.Status != "" {
			// Version compatibility gates seeding, but recoverable operational
			// faults must not prevent the safe seed commit that clears them.
			return checkBuild(status.Build)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("containd not ready after %s: %w", timeout, ctx.Err())
		case <-ticker.C:
		}
	}
}
