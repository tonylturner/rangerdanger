package shared

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// AuditEntry records a command attempt with its process-level consequence.
type AuditEntry struct {
	Timestamp     time.Time `json:"timestamp"`
	Source        string    `json:"source"`
	SourceZone    string    `json:"source_zone,omitempty"`    // enterprise, vendor, ot_ops, field, unknown
	Target        string    `json:"target"`
	Command       string    `json:"command"`
	Result        string    `json:"result"`                   // "executed", "rejected", "blocked"
	Detail        string    `json:"detail,omitempty"`
	ProcessImpact string    `json:"process_impact,omitempty"` // human-readable consequence
}

// AuditLog is a thread-safe bounded audit log.
type AuditLog struct {
	mu      sync.Mutex
	entries []AuditEntry
	max     int
}

func NewAuditLog(max int) *AuditLog {
	return &AuditLog{max: max}
}

func (a *AuditLog) Add(e AuditEntry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	if e.SourceZone == "" {
		e.SourceZone = ClassifyZone(e.Source)
	}
	a.entries = append(a.entries, e)
	if len(a.entries) > a.max {
		a.entries = a.entries[len(a.entries)-a.max:]
	}
}

func (a *AuditLog) Entries() []AuditEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]AuditEntry, len(a.entries))
	copy(out, a.entries)
	return out
}

// CommandRequest is the standard command payload for device simulators.
type CommandRequest struct {
	Command string  `json:"command"`
	Source  string  `json:"source,omitempty"`
	Value   float64 `json:"value,omitempty"`
}

// ClassifyZone maps a source IP or label to a network zone name.
func ClassifyZone(source string) string {
	if source == "" {
		return "unknown"
	}
	s := strings.ToLower(source)
	switch {
	case strings.HasPrefix(source, "10.10.10."):
		return "enterprise"
	case strings.HasPrefix(source, "10.20.20."):
		return "vendor"
	case strings.HasPrefix(source, "10.30.30."):
		return "ot_ops"
	case strings.HasPrefix(source, "10.40.40."):
		return "field"
	case strings.HasPrefix(source, "10.50.50."):
		return "physics"
	case strings.Contains(s, "kali") || strings.Contains(s, "enterprise"):
		return "enterprise"
	case strings.Contains(s, "vendor") || strings.Contains(s, "jump"):
		return "vendor"
	case strings.Contains(s, "compromised") || strings.Contains(s, "openplc") || strings.Contains(s, "hmi"):
		return "ot_ops"
	case strings.Contains(s, "rtac") || strings.Contains(s, "reset-script") || strings.Contains(s, "scenario-script"):
		return "ot_ops"
	case strings.Contains(s, "web-ui"):
		return "operator"
	}
	return "unknown"
}

// JSON helpers

func WriteJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func ReadJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}

func SourceFromRequest(r *http.Request, cmd CommandRequest) string {
	if cmd.Source != "" {
		return cmd.Source
	}
	return r.RemoteAddr
}

// CORS middleware for development.
func CORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func StartServer(name string, mux *http.ServeMux) {
	addr := ":8080"
	log.Printf("%s starting on %s", name, addr)
	if err := http.ListenAndServe(addr, CORSMiddleware(mux)); err != nil {
		log.Fatalf("%s failed: %v", name, err)
	}
}
