package server

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
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
	ConnectionSeq *uint64 `json:"connection_seq"`
	Point         string  `json:"point"`
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

// outageWindow ties a down/stale pair to a recovery link identity and GI.
type outageWindow struct {
	down, stale, up, gi journalEntry
}

func (w outageWindow) String() string {
	return fmt.Sprintf("link_down seq %d at %s to gi_complete seq %d at %s",
		w.down.Seq, w.down.At.Format(time.RFC3339Nano), w.gi.Seq, w.gi.At.Format(time.RFC3339Nano))
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

// outageAfter completes the window that starts at down from the entries
// after it, or names what is missing: the position point going stale, then
// the first GI completed on a later connection, whose link_up lies between
// down and that GI.
func outageAfter(down journalEntry, later []journalEntry, point string) (outageWindow, string) {
	window := outageWindow{down: down}
	staleAt := -1
	for i, entry := range later {
		if entry.Kind == "point_stale" && entry.Detail.Point == point {
			window.stale, staleAt = entry, i
			break
		}
	}
	if staleAt < 0 {
		return window, "no point_stale for " + point + " after it"
	}
	giAt := -1
	for i, entry := range later[staleAt+1:] {
		if entry.Kind == "gi_complete" && laterConnection(entry, down) {
			window.gi, giAt = entry, staleAt+1+i
			break
		}
	}
	if giAt < 0 {
		return window, "no later gi_complete on a connection with a greater connection_seq"
	}
	for _, entry := range later[:giAt] {
		if entry.Kind == "link_up" && entry.Detail.ConnectionSeq != nil && *entry.Detail.ConnectionSeq == *window.gi.Detail.ConnectionSeq {
			window.up = entry
			return window, ""
		}
	}
	return window, fmt.Sprintf("no link_up for connection %d before its gi_complete", *window.gi.Detail.ConnectionSeq)
}

func laterConnection(entry, down journalEntry) bool {
	return entry.Detail.ConnectionSeq != nil && *entry.Detail.ConnectionSeq > *down.Detail.ConnectionSeq
}

// euRetainedOutages yields the streaming recovery candidates used by the
// historian validator. Unlike lab 1's first-recovery proof, it tracks the
// latest stale/up observations before each GI in collection order. Keeping
// that choice explicit preserves the validators' existing evidence contracts.
func euRetainedOutages(entries []euLab3JournalEntry, point string, start, end time.Time) iter.Seq[outageWindow] {
	return func(yield func(outageWindow) bool) {
		for _, rawDown := range entries {
			if rawDown.Kind != "link_down" || !insideEvidenceWindow(rawDown.At, start, end) {
				continue
			}
			var downDetail struct {
				ConnectionSeq *uint64 `json:"connection_seq"`
			}
			if json.Unmarshal(rawDown.Detail, &downDetail) != nil || downDetail.ConnectionSeq == nil {
				continue
			}
			down := journalEntry{Seq: rawDown.Seq, At: rawDown.At, Kind: rawDown.Kind}
			down.Detail.ConnectionSeq = downDetail.ConnectionSeq
			window := outageWindow{down: down}
			for _, raw := range entries {
				if raw.Seq <= down.Seq || !insideEvidenceWindow(raw.At, down.At, end) {
					continue
				}
				entry := journalEntry{Seq: raw.Seq, At: raw.At, Kind: raw.Kind}
				if json.Unmarshal(raw.Detail, &entry.Detail) != nil {
					continue
				}
				switch entry.Kind {
				case "point_stale":
					if entry.Detail.Point == point {
						window.stale = entry
					}
				case "link_up":
					if laterConnection(entry, down) {
						window.up = entry
					}
				case "gi_complete":
					if window.stale.At.IsZero() || window.up.At.IsZero() || !window.up.At.After(window.stale.At) ||
						entry.Detail.ConnectionSeq == nil || *entry.Detail.ConnectionSeq != *window.up.Detail.ConnectionSeq {
						continue
					}
					window.gi = entry
					if !yield(window) {
						return
					}
				}
			}
		}
	}
}
