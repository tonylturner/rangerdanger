// SPDX-License-Identifier: Apache-2.0
package fielddevice

import (
	"sync"
	"time"
)

type AuditEntry struct {
	Seq       uint64       `json:"seq"`
	At        time.Time    `json:"at"`
	Operation string       `json:"operation"`
	Result    string       `json:"result"`
	Before    Snapshot     `json:"before"`
	After     Snapshot     `json:"after"`
	Context   AuditContext `json:"context"`
	Detail    any          `json:"detail,omitempty"`
}

type AuditLog struct {
	mu       sync.Mutex
	entries  []AuditEntry
	nextSeq  uint64
	capacity int
}

func NewAuditLog(capacity int) *AuditLog {
	if capacity < 1 {
		capacity = 1
	}
	return &AuditLog{capacity: capacity}
}

func (a *AuditLog) Add(entry AuditEntry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.nextSeq++
	entry.Seq = a.nextSeq
	if entry.At.IsZero() {
		entry.At = time.Now().UTC()
	}
	a.entries = append(a.entries, entry)
	if len(a.entries) > a.capacity {
		a.entries = append([]AuditEntry(nil), a.entries[len(a.entries)-a.capacity:]...)
	}
}

func (a *AuditLog) After(after uint64) ([]AuditEntry, *uint64, *uint64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.entries) == 0 {
		return []AuditEntry{}, nil, nil
	}
	oldest := a.entries[0].Seq
	latest := a.entries[len(a.entries)-1].Seq
	entries := make([]AuditEntry, 0, len(a.entries))
	for _, entry := range a.entries {
		if entry.Seq > after {
			entries = append(entries, entry)
		}
	}
	return entries, &latest, &oldest
}
