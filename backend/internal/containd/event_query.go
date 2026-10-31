package containd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// EventQuery predicates are ANDed; Kind accepts comma-separated prefixes.
type EventQuery struct {
	Limit                           int
	Since                           time.Time
	Kind, Protocol, Verdict, Cursor string
}

// EventPage describes retained evidence, never a durable or snapshot history.
type EventPage struct {
	Events           []Event `json:"events"`
	NextCursor       string  `json:"nextCursor,omitempty"`
	HistoryTruncated bool    `json:"historyTruncated"`
	EngineError      string  `json:"engineError,omitempty"`
}

// GetEventPage explicitly requests the filtered-page contract, even without
// predicates. Callers must retain engine errors and incomplete-history warnings.
func (c *Client) GetEventPage(ctx context.Context, query EventQuery) (EventPage, error) {
	params := url.Values{"query": {"1"}, "limit": {strconv.Itoa(query.Limit)}}
	if !query.Since.IsZero() {
		params.Set("since", query.Since.UTC().Format(time.RFC3339Nano))
	}
	for key, value := range map[string]string{"kind": query.Kind, "protocol": query.Protocol, "verdict": query.Verdict, "cursor": query.Cursor} {
		if value != "" {
			params.Set(key, value)
		}
	}
	resp, err := c.doRequest(ctx, http.MethodGet, c.BaseURL+"/api/v1/events?"+params.Encode())
	if err != nil {
		return EventPage{}, fmt.Errorf("query events: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return EventPage{}, fmt.Errorf("query events returned %d: %s", resp.StatusCode, body)
	}
	var wire struct {
		Events           []Event `json:"events"`
		NextCursor       string  `json:"nextCursor"`
		HistoryTruncated *bool   `json:"historyTruncated"`
		EngineError      string  `json:"engineError"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&wire); err != nil {
		return EventPage{}, fmt.Errorf("decode event page: %w", err)
	}
	if wire.HistoryTruncated == nil {
		return EventPage{}, fmt.Errorf("event page missing historyTruncated")
	}
	return EventPage{wire.Events, wire.NextCursor, *wire.HistoryTruncated, wire.EngineError}, nil
}

// QueryEvents follows cursors with identical predicates and combines metadata.
// Retention can change between pages; reaching the end is not completeness proof.
func (c *Client) QueryEvents(ctx context.Context, query EventQuery) (EventPage, error) {
	result := EventPage{Events: []Event{}}
	cursors := map[string]bool{query.Cursor: true}
	for {
		page, err := c.GetEventPage(ctx, query)
		if err != nil {
			return result, err
		}
		result.Events = append(result.Events, page.Events...)
		result.HistoryTruncated = result.HistoryTruncated || page.HistoryTruncated
		if page.EngineError != "" {
			result.EngineError = page.EngineError
			return result, nil
		}
		if page.NextCursor == "" {
			return result, nil
		}
		if cursors[page.NextCursor] {
			return result, fmt.Errorf("event query repeated cursor %q", page.NextCursor)
		}
		cursors[page.NextCursor] = true
		query.Cursor = page.NextCursor
	}
}
