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
	Warning    string `json:"warning,omitempty"`
}

// ApplyResult retains both runtime readiness and auxiliary commit warnings.
type ApplyResult struct {
	Receipt  *ApplyReceipt
	Warnings []string
}

// ProtectionStatus is the readiness observation, including component reasons.
type ProtectionStatus struct {
	BootID           string                `json:"bootId"`
	ApplyID          string                `json:"applyId"`
	InstalledApplyID string                `json:"installedApplyId"`
	State            string                `json:"state"`
	Retryable        bool                  `json:"retryable"`
	ObservedAt       string                `json:"observedAt"`
	Components       []ProtectionComponent `json:"components"`
	Effects          []ProtectionEffect    `json:"effects"`
}

// ProtectionComponent explains a consumer's readiness state.
type ProtectionComponent struct {
	Name        string `json:"name"`
	Required    bool   `json:"required"`
	State       string `json:"state"`
	Reason      string `json:"reason"`
	Attempt     int    `json:"attempt,omitempty"`
	MaxAttempts int    `json:"maxAttempts,omitempty"`
	Since       string `json:"since"`
}

// ProtectionEffect describes the traffic consequence of consumer readiness.
type ProtectionEffect struct {
	Traffic string `json:"traffic"`
	Effect  string `json:"effect"`
}

// RuntimeApplyResponse is a synchronous commit acknowledgement, not readiness.
type RuntimeApplyResponse struct {
	Status     string           `json:"status"`
	ApplyID    string           `json:"applyId"`
	Protection ProtectionStatus `json:"protection"`
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
	initialInstalled := observation.InstalledApplyID
	seen := false
	receipt := &ApplyReceipt{ApplyID: applied.ApplyID}
	fail := func(reason string) (*ApplyReceipt, error) {
		return receipt, &ProtectionError{Reason: reason, Observation: observation}
	}
	for {
		receipt.Protection = observation.State
		if receipt.Protection == "" {
			receipt.Protection = "unknown"
		}
		if observation.ApplyID == applied.ApplyID {
			seen = true
		} else if observation.ApplyID != "" && seen {
			return fail("superseded")
		}
		if observation.InstalledApplyID != "" && observation.InstalledApplyID != applied.ApplyID &&
			observation.InstalledApplyID != initialInstalled {
			return fail("superseded")
		}
		switch observation.State {
		case "failed", "disabled":
			return fail(observation.State)
		case "protecting", "degraded":
			if observation.ApplyID == applied.ApplyID && observation.InstalledApplyID == applied.ApplyID {
				if observation.State == "degraded" {
					receipt.Warning = "containd protection degraded: rule-hit logging unavailable"
				}
				return receipt, nil
			}
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
		if resp.StatusCode != http.StatusOK {
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
