package main

import (
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/tturner/rangerdanger/services/shared"
)

type RecloserState struct {
	mu                sync.RWMutex
	Closed            bool   `json:"closed"`
	RecloseEnabled    bool   `json:"reclose_enabled"`
	ShotCount         int    `json:"shot_count"`
	Lockout           bool   `json:"lockout"`
	FaultSeen         bool   `json:"fault_seen"`
	AutoMode          bool   `json:"auto_mode"`
	CommsOK           bool   `json:"comms_ok"`
	LastCommandSource string `json:"last_command_source"`
}

const maxShots = 3

func (s *RecloserState) snapshot() map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]any{
		"closed":              s.Closed,
		"reclose_enabled":     s.RecloseEnabled,
		"shot_count":          s.ShotCount,
		"lockout":             s.Lockout,
		"fault_seen":          s.FaultSeen,
		"auto_mode":           s.AutoMode,
		"comms_ok":            s.CommsOK,
		"last_command_source": s.LastCommandSource,
	}
}

// autoReclose simulates the automatic reclosing sequence.
func (s *RecloserState) autoReclose() {
	time.Sleep(2 * time.Second)
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.RecloseEnabled || s.Lockout || !s.AutoMode {
		return
	}

	if s.FaultSeen && s.ShotCount < maxShots {
		s.Closed = true
		log.Printf("auto-reclose attempt %d", s.ShotCount)
		// If fault persists, trip again after brief delay
		if s.FaultSeen {
			go func() {
				time.Sleep(500 * time.Millisecond)
				s.mu.Lock()
				defer s.mu.Unlock()
				if s.FaultSeen && s.Closed {
					s.Closed = false
					s.ShotCount++
					log.Printf("tripped on persistent fault, shot %d", s.ShotCount)
					if s.ShotCount >= maxShots {
						s.Lockout = true
						log.Printf("lockout after %d shots", maxShots)
					} else {
						go s.autoReclose()
					}
				}
			}()
		}
	}
}

var (
	state = &RecloserState{
		Closed:         true,
		RecloseEnabled: true,
		AutoMode:       true,
		CommsOK:        true,
	}
	audit = shared.NewAuditLog(100)
)

func handleState(w http.ResponseWriter, r *http.Request) {
	shared.WriteJSON(w, state.snapshot())
}

func handleCommand(w http.ResponseWriter, r *http.Request) {
	var cmd shared.CommandRequest
	if err := shared.ReadJSON(r, &cmd); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	source := shared.SourceFromRequest(r, cmd)

	state.mu.Lock()

	entry := shared.AuditEntry{
		Source:  source,
		Target:  "recloser-sim",
		Command: cmd.Command,
	}

	switch cmd.Command {
	case "open":
		state.Closed = false
		state.LastCommandSource = source
		entry.Result = "executed"
		entry.Detail = "recloser opened"
		log.Printf("OPEN from %s", source)
	case "close":
		if state.Lockout {
			entry.Result = "rejected"
			entry.Detail = "lockout active"
		} else {
			state.Closed = true
			state.ShotCount = 0
			state.LastCommandSource = source
			entry.Result = "executed"
			entry.Detail = "recloser closed"
			log.Printf("CLOSE from %s", source)
		}
	case "enable_reclose":
		state.RecloseEnabled = true
		state.LastCommandSource = source
		entry.Result = "executed"
		entry.Detail = "reclose enabled"
		log.Printf("RECLOSE ENABLED from %s", source)
	case "disable_reclose":
		state.RecloseEnabled = false
		state.LastCommandSource = source
		entry.Result = "executed"
		entry.Detail = "reclose disabled"
		log.Printf("RECLOSE DISABLED from %s - auto-restoration will fail", source)
	case "reset_lockout":
		state.Lockout = false
		state.ShotCount = 0
		state.LastCommandSource = source
		entry.Result = "executed"
		entry.Detail = "lockout reset"
	case "inject_fault":
		state.FaultSeen = true
		entry.Result = "executed"
		entry.Detail = "fault injected"
		if state.Closed {
			state.Closed = false
			state.ShotCount++
			log.Printf("fault trip, shot %d", state.ShotCount)
			if state.ShotCount < maxShots && state.RecloseEnabled && state.AutoMode {
				state.mu.Unlock()
				go state.autoReclose()
				audit.Add(entry)
				shared.WriteJSON(w, map[string]any{"result": entry.Result, "detail": entry.Detail})
				return
			} else if state.ShotCount >= maxShots {
				state.Lockout = true
				entry.Detail = "fault injected, lockout after max shots"
			}
		}
	case "clear_fault":
		state.FaultSeen = false
		entry.Result = "executed"
		entry.Detail = "fault cleared"
	default:
		entry.Result = "rejected"
		entry.Detail = "unknown command"
	}

	state.mu.Unlock()
	audit.Add(entry)
	shared.WriteJSON(w, map[string]any{"result": entry.Result, "detail": entry.Detail})
}

func handleAudit(w http.ResponseWriter, r *http.Request) {
	shared.WriteJSON(w, map[string]any{"entries": audit.Entries()})
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	shared.WriteJSON(w, map[string]string{"status": "ok", "service": "recloser-sim"})
}

func main() {
	go startModbusServer()
	go startDNP3Server()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", handleState)
	mux.HandleFunc("POST /api/command", handleCommand)
	mux.HandleFunc("GET /api/audit", handleAudit)
	mux.HandleFunc("GET /api/health", handleHealth)
	shared.StartServer("recloser-sim", mux)
}
