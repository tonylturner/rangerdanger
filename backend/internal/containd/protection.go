package containd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ApplyReceipt identifies the committed application and its last observed state.
type ApplyReceipt struct {
	ApplyID    string `json:"applyId"`
	Protection string `json:"protection"`
}

// ApplyResult retains both runtime readiness and auxiliary commit warnings.
type ApplyResult struct {
	Receipt  *ApplyReceipt
	Warnings []string
}

// ProtectionStatus observes readiness at an opaque committed revision.
type ProtectionStatus struct {
	State              string                `json:"state"`
	Ready              bool                  `json:"ready"`
	ProtectingRevision *string               `json:"protectingRevision"`
	DesiredRevision    *string               `json:"desiredRevision"`
	AppliedRevision    *string               `json:"appliedRevision"`
	Subsystems         []ProtectionSubsystem `json:"subsystems"`
}

// ProtectionSubsystem reports whether a required consumer has a terminal fault.
type ProtectionSubsystem struct {
	Required bool             `json:"required"`
	State    string           `json:"state"`
	Fault    *ProtectionFault `json:"fault"`
}

// ProtectionFault distinguishes configuration failures from delivery delays.
type ProtectionFault struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

// RuntimeApplyResponse is a synchronous commit acknowledgement, not readiness.
type RuntimeApplyResponse struct {
	Status          string           `json:"status"`
	RunningRevision string           `json:"runningRevision"`
	ApplyID         string           `json:"applyId"`
	Protection      ProtectionStatus `json:"protection"`
}

// ProtectionError preserves the last observation on failure or timeout.
type ProtectionError struct {
	Reason      string
	Observation ProtectionStatus
}

func (e *ProtectionError) Error() string {
	data, _ := json.Marshal(e.Observation)
	return fmt.Sprintf("containd protection %s: %s", e.Reason, data)
}

func (c *Client) rememberApply(receipt *ApplyReceipt) {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()
	copy := *receipt
	c.lastApply = &copy
}

// LastApply returns a copy of this client's last completed apply observation.
func (c *Client) LastApply() *ApplyReceipt {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()
	if c.lastApply == nil {
		return nil
	}
	receipt := *c.lastApply
	return &receipt
}

func (c *Client) waitProtection(parent context.Context, applied RuntimeApplyResponse) (*ApplyReceipt, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	observation := applied.Protection
	receipt := &ApplyReceipt{ApplyID: applied.ApplyID}
	fail := func(reason string) (*ApplyReceipt, error) {
		return receipt, &ProtectionError{Reason: reason, Observation: observation}
	}
	for {
		receipt.Protection = observation.State
		if receipt.Protection == "" {
			receipt.Protection = "unknown"
		}
		if observation.DesiredRevision != nil && *observation.DesiredRevision != applied.RunningRevision {
			return fail("superseded")
		}
		if observation.State == "disabled" {
			return fail("disabled")
		}
		for _, subsystem := range observation.Subsystems {
			if subsystem.Required && subsystem.Fault != nil &&
				(subsystem.Fault.Kind == "config" || subsystem.Fault.Kind == "render") {
				return fail("required subsystem " + subsystem.Fault.Kind + " fault")
			}
		}
		if observation.Ready && observation.ProtectingRevision != nil &&
			*observation.ProtectingRevision == applied.RunningRevision {
			receipt.Protection = "protecting"
			return receipt, nil
		}
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fail("readiness deadline: " + ctx.Err().Error())
		case <-timer.C:
		}
		resp, err := c.doRequest(ctx, http.MethodGet, c.BaseURL+"/api/v1/dataplane/protection")
		if err != nil {
			return fail("read protection: " + err.Error())
		}
		if resp.StatusCode == http.StatusServiceUnavailable {
			resp.Body.Close()
			continue
		}
		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadGateway {
			resp.Body.Close()
			return fail(fmt.Sprintf("read protection returned %d", resp.StatusCode))
		}
		var next ProtectionStatus
		err = json.NewDecoder(resp.Body).Decode(&next)
		resp.Body.Close()
		if err != nil {
			return fail("decode protection: " + err.Error())
		}
		observation = next
	}
}
