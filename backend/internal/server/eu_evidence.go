package server

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// EU evidence envelopes share the same wire shape while each validator
// decodes only its own snapshot/detail projection. No observation is filled
// from another surface, and collection order/error handling stays with the caller.
type euJournal[D any] struct {
	CacheEpoch string              `json:"cache_epoch"`
	Entries    []euJournalEntry[D] `json:"entries"`
}

type euJournalEntry[D any] struct {
	Seq    uint64    `json:"seq"`
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Detail D         `json:"detail"`
}

type journalDetail struct {
	ConnectionSeq *uint64         `json:"connection_seq"`
	Point         string          `json:"point"`
	Value         json.RawMessage `json:"value"`
	StaleReason   *string         `json:"stale_reason"`
}

type euAudit[E any] struct {
	Generation uint32 `json:"generation"`
	Entries    []E    `json:"entries"`
}

type euAuditEntry[B, A, C any] struct {
	Seq       uint64          `json:"seq"`
	At        time.Time       `json:"at"`
	Operation string          `json:"operation"`
	Result    string          `json:"result"`
	Detail    json.RawMessage `json:"detail"`
	Before    B               `json:"before"`
	After     A               `json:"after"`
	Context   C               `json:"context"`
}

type auditContext struct {
	Protocol string `json:"protocol"`
}

type peerAuditContext struct {
	Protocol string `json:"protocol"`
	Peer     string `json:"peer"`
}

// fetchJSON GETs url, which must answer a JSON object of shape T.
func fetchJSON[T any](ctx context.Context, service, url string) (*T, error) {
	body, err := fetchUpstreamObject(ctx, service, url)
	if err != nil {
		return nil, err
	}
	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("%s: decode %s: %w", service, url, err)
	}
	return &out, nil
}

func insideEvidenceWindow(at, start, end time.Time) bool {
	return !at.IsZero() && !at.Before(start) && !at.After(end)
}
