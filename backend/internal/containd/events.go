package containd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// EventID handles containd returning IDs as either strings or numbers.
type EventID string

func (e *EventID) UnmarshalJSON(data []byte) error {
	// Try string first, then number
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*e = EventID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err == nil {
		*e = EventID(n.String())
		return nil
	}
	*e = EventID(string(data))
	return nil
}

// Event represents a containd DPI/IDS event.
type Event struct {
	ID         EventID        `json:"id"`
	Timestamp  time.Time      `json:"timestamp"`
	Kind       string         `json:"kind"`
	Source     string         `json:"srcIp"`
	Dest       string         `json:"dstIp"`
	Protocol   string         `json:"protocol"` // mapped from containd's `proto`
	Transport  string         `json:"transport"`
	SrcPort    int            `json:"srcPort"`
	DstPort    int            `json:"dstPort"`
	Attributes map[string]any `json:"attributes"`
}

// UnmarshalJSON decodes containd's wire key `proto` into Protocol.
// The backend re-serialises events to the portal under `protocol`,
// so the containd key and the portal key differ.
func (e *Event) UnmarshalJSON(data []byte) error {
	type eventFields Event
	var wire struct {
		eventFields
		Proto string `json:"proto"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*e = Event(wire.eventFields)
	e.Protocol = wire.Proto
	return nil
}

// Flow mirrors containd's FlowSummary, one row of the engine flow table
// served by GET /api/v1/flows. The table is a rollup of DPI events, so a
// flow appears once the engine has seen traffic on it and disappears when
// the engine drops it (for example after an IEC 104 session abort).
type Flow struct {
	FlowID      string    `json:"flowId"`
	FirstSeen   time.Time `json:"firstSeen"`
	LastSeen    time.Time `json:"lastSeen"`
	SrcIP       string    `json:"srcIp,omitempty"`
	DstIP       string    `json:"dstIp,omitempty"`
	SrcPort     uint16    `json:"srcPort,omitempty"`
	DstPort     uint16    `json:"dstPort,omitempty"`
	Transport   string    `json:"transport,omitempty"`
	Application string    `json:"application,omitempty"`
	EventCount  uint64    `json:"eventCount"`
	AvDetected  bool      `json:"avDetected,omitempty"`
	AvBlocked   bool      `json:"avBlocked,omitempty"`
}

// GetEvents returns recent events, optionally filtered by time.
func (c *Client) GetEvents(since string, limit int) ([]Event, error) {
	url := fmt.Sprintf("%s/api/v1/events?limit=%d", c.BaseURL, limit)
	if since != "" {
		url += "&since=" + since
	}

	resp, err := c.doRequest("GET", url)
	if err != nil {
		return nil, fmt.Errorf("get events failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get events returned %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read events body: %w", err)
	}

	// containd may return events as a raw array or as {"events": [...]}
	var events []Event
	if err := json.Unmarshal(body, &events); err != nil {
		// Try wrapped format
		var result struct {
			Events []Event `json:"events"`
		}
		if err2 := json.Unmarshal(body, &result); err2 != nil {
			return nil, fmt.Errorf("decode events: %w (also tried array: %w)", err2, err)
		}
		events = result.Events
	}

	return events, nil
}

// containd's GET /api/v1/flows bounds: it accepts a limit in
// 1..MaxFlowLimit and silently uses DefaultFlowLimit for anything else.
const (
	DefaultFlowLimit = 200
	MaxFlowLimit     = 5000
)

// GetFlows returns up to limit rows of the engine flow table. containd
// answers with a bare JSON array.
func (c *Client) GetFlows(limit int) ([]Flow, error) {
	resp, err := c.doRequest("GET", fmt.Sprintf("%s/api/v1/flows?limit=%d", c.BaseURL, limit))
	if err != nil {
		return nil, fmt.Errorf("get flows failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get flows returned %d: %s", resp.StatusCode, string(body))
	}

	var flows []Flow
	if err := json.NewDecoder(resp.Body).Decode(&flows); err != nil {
		return nil, fmt.Errorf("decode flows: %w", err)
	}
	return flows, nil
}
