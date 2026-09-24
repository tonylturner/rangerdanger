package shared

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClassifyZone(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   string
	}{
		{name: "enterprise prefix", source: "10.10.10.12", want: "enterprise"},
		{name: "vendor prefix", source: "10.20.20.8", want: "vendor"},
		{name: "OT operations prefix", source: "10.30.30.24", want: "ot_ops"},
		{name: "field prefix", source: "10.40.40.17", want: "field"},
		{name: "physics prefix", source: "10.50.50.4", want: "physics"},
		{name: "Kali label", source: "kali-workstation", want: "enterprise"},
		{name: "mixed case Kali label", source: "KaLi-workstation", want: "enterprise"},
		{name: "vendor jump label", source: "vendor-jump", want: "vendor"},
		{name: "OT HMI label", source: "HMI", want: "ot_ops"},
		{name: "empty", source: "", want: "unknown"},
		{name: "unknown", source: "10.99.99.2", want: "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClassifyZone(tt.source); got != tt.want {
				t.Errorf("ClassifyZone(%q) = %q, want %q", tt.source, got, tt.want)
			}
		})
	}
}

func TestAuditLogBoundAndEntriesCopy(t *testing.T) {
	log := NewAuditLog(2)
	for _, command := range []string{"first", "second", "third"} {
		log.Add(AuditEntry{Command: command})
	}

	entries := log.Entries()
	if len(entries) != 2 {
		t.Fatalf("Entries() length = %d, want 2", len(entries))
	}
	if entries[0].Command != "second" || entries[1].Command != "third" {
		t.Fatalf("Entries() commands = [%q, %q], want [second, third]", entries[0].Command, entries[1].Command)
	}

	entries[0].Command = "modified copy"
	if got := log.Entries()[0].Command; got != "second" {
		t.Errorf("mutating returned entries changed the log: first command = %q, want second", got)
	}
}

func TestAuditLogAddFillsTimestampAndZone(t *testing.T) {
	log := NewAuditLog(2)
	before := time.Now()
	log.Add(AuditEntry{Source: "10.20.20.9", Command: "switch_in"})
	after := time.Now()

	entries := log.Entries()
	if len(entries) != 1 {
		t.Fatalf("Entries() length = %d, want 1", len(entries))
	}
	entry := entries[0]
	if entry.Timestamp.Before(before) || entry.Timestamp.After(after) {
		t.Errorf("filled timestamp %v is outside [%v, %v]", entry.Timestamp, before, after)
	}
	if entry.SourceZone != "vendor" {
		t.Errorf("SourceZone = %q, want vendor", entry.SourceZone)
	}
}

func TestReadJSON(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"command":"trip","value":2.5}`))
		var got CommandRequest
		if err := ReadJSON(req, &got); err != nil {
			t.Fatalf("ReadJSON() error = %v", err)
		}
		if got.Command != "trip" || got.Value != 2.5 {
			t.Errorf("ReadJSON() = %+v, want command trip and value 2.5", got)
		}
	})

	t.Run("malformed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"command":`))
		var got CommandRequest
		if err := ReadJSON(req, &got); err == nil {
			t.Fatal("ReadJSON() error = nil, want malformed JSON error")
		}
	})
}

func TestWriteJSON(t *testing.T) {
	response := httptest.NewRecorder()
	WriteJSON(response, map[string]any{"result": "executed", "count": 3})

	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var got map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &got); err != nil {
		t.Fatalf("response body is not JSON: %v", err)
	}
	if got["result"] != "executed" || got["count"] != float64(3) {
		t.Errorf("JSON body = %#v, want result executed and count 3", got)
	}
}

func TestCORSMiddleware(t *testing.T) {
	calls := 0
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("downstream"))
	})
	handler := CORSMiddleware(next)

	preflight := httptest.NewRecorder()
	handler.ServeHTTP(preflight, httptest.NewRequest(http.MethodOptions, "/resource", nil))
	if preflight.Code != http.StatusOK {
		t.Errorf("OPTIONS status = %d, want %d", preflight.Code, http.StatusOK)
	}
	if calls != 0 {
		t.Errorf("downstream calls after OPTIONS = %d, want 0", calls)
	}
	for header, want := range map[string]string{
		"Access-Control-Allow-Origin":  "*",
		"Access-Control-Allow-Methods": "GET, POST, OPTIONS",
		"Access-Control-Allow-Headers": "Content-Type",
	} {
		if got := preflight.Header().Get(header); got != want {
			t.Errorf("OPTIONS %s = %q, want %q", header, got, want)
		}
	}

	ordinary := httptest.NewRecorder()
	handler.ServeHTTP(ordinary, httptest.NewRequest(http.MethodPost, "/resource", nil))
	if ordinary.Code != http.StatusCreated || ordinary.Body.String() != "downstream" {
		t.Errorf("ordinary response = (%d, %q), want (%d, downstream)", ordinary.Code, ordinary.Body.String(), http.StatusCreated)
	}
	if calls != 1 {
		t.Errorf("downstream calls after ordinary request = %d, want 1", calls)
	}
	if got := ordinary.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Errorf("ordinary Access-Control-Allow-Origin = %q, want *", got)
	}
}
