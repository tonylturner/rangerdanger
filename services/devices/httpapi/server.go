// SPDX-License-Identifier: Apache-2.0
package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/tturner/rangerdanger/services/devices/feederbreaker"
)

const maxRequestBody = 1 << 20

type device interface {
	Snapshot() feederbreaker.Snapshot
	Truth() feederbreaker.Truth
	ApplyMeasurements(context.Context, feederbreaker.MeasurementInput, feederbreaker.AuditContext) (feederbreaker.Snapshot, error)
	InjectFault(context.Context, bool, feederbreaker.AuditContext) (feederbreaker.CommandResult, error)
	Reset(context.Context, feederbreaker.AuditContext) error
	MeasurementStatus() (bool, *int64)
	AuditAfter(uint64) ([]feederbreaker.AuditEntry, *uint64, *uint64)
	Generation() uint32
}

type Server struct {
	model device
	zones *feederbreaker.ZoneMap
}

func New(model device, zones *feederbreaker.ZoneMap) *Server {
	return &Server{model: model, zones: zones}
}

func (s *Server) ProcessHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/truth", s.handleTruth)
	mux.HandleFunc("POST /api/measurements", s.handleMeasurements)
	return mux
}

func (s *Server) InstructorHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("GET /api/audit", s.handleAudit)
	mux.HandleFunc("POST /api/instructor/fault", s.handleFault)
	mux.HandleFunc("POST /api/instructor/reset", s.handleReset)
	return mux
}

func (s *Server) handleTruth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.model.Truth())
}

func (s *Server) handleMeasurements(w http.ResponseWriter, r *http.Request) {
	var input feederbreaker.MeasurementInput
	complete, err := decodeRequired(r, &input,
		"schema", "device_id", "generation", "revision", "solver_epoch",
		"solve_seq", "solved_at", "current_a", "voltage_kv_ll")
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !complete {
		http.Error(w, "all measurement fields are required and non-null", http.StatusUnprocessableEntity)
		return
	}
	snapshot, err := s.model.ApplyMeasurements(r.Context(), input, s.context(r, "process"))
	if err != nil {
		var measurementErr feederbreaker.MeasurementError
		if errors.As(err, &measurementErr) {
			switch measurementErr {
			case feederbreaker.MeasurementStaleTruth, feederbreaker.MeasurementReplay:
				writeJSONStatus(w, http.StatusConflict, map[string]any{
					"accepted": false,
					"reason":   string(measurementErr),
				})
			default:
				writeJSONStatus(w, http.StatusUnprocessableEntity, map[string]any{"error": "invalid measurements"})
			}
			return
		}
		http.Error(w, "measurement request failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"accepted": true, "revision": snapshot.Revision})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	valid, age := s.model.MeasurementStatus()
	writeJSON(w, map[string]any{
		"status":             "ok",
		"measurements_valid": valid,
		"measurement_age_ms": age,
	})
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.model.Snapshot())
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	var after uint64
	if raw := r.URL.Query().Get("after"); raw != "" {
		parsed, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			http.Error(w, "invalid after sequence", http.StatusBadRequest)
			return
		}
		after = parsed
	}
	entries, latest, oldest := s.model.AuditAfter(after)
	writeJSON(w, map[string]any{
		"generation": s.model.Generation(),
		"entries":    entries,
		"latest_seq": latest,
		"oldest_seq": oldest,
	})
}

func (s *Server) handleFault(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Active *bool `json:"active"`
	}
	if err := decodeStrict(r, &request); err != nil || request.Active == nil {
		http.Error(w, "active is required", http.StatusBadRequest)
		return
	}
	result, err := s.model.InjectFault(r.Context(), *request.Active, s.context(r, "instructor"))
	if err != nil {
		http.Error(w, "fault request failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"accepted": result.Accepted})
}

func (s *Server) handleReset(w http.ResponseWriter, r *http.Request) {
	var request struct{}
	if err := decodeStrict(r, &request); err != nil {
		http.Error(w, "reset body must be an empty object", http.StatusBadRequest)
		return
	}
	if err := s.model.Reset(r.Context(), s.context(r, "instructor")); err != nil {
		http.Error(w, "reset request failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"accepted": true})
}

func (s *Server) context(r *http.Request, protocol string) feederbreaker.AuditContext {
	peer := r.RemoteAddr
	return s.zones.Context(peer, protocol)
}

func decodeStrict(r *http.Request, value any) error {
	body, err := readObjectBody(r)
	if err != nil {
		return err
	}
	return decodeBodyStrict(body, value)
}

func decodeRequired(r *http.Request, value any, required ...string) (bool, error) {
	body, err := readObjectBody(r)
	if err != nil {
		return false, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return false, err
	}
	allowed := make(map[string]struct{}, len(required))
	for _, key := range required {
		allowed[key] = struct{}{}
	}
	for key := range fields {
		if _, ok := allowed[key]; !ok {
			return false, fmt.Errorf("unknown field %q", key)
		}
	}
	for _, key := range required {
		raw, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return false, nil
		}
	}
	if err := decodeBodyStrict(body, value); err != nil {
		return false, err
	}
	return true, nil
}

func readObjectBody(r *http.Request) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxRequestBody {
		return nil, errors.New("request body too large")
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, errors.New("request body must be a JSON object")
	}
	return body, nil
}

func decodeBodyStrict(body []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("request contains more than one JSON value")
		}
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, value any) {
	writeJSONStatus(w, http.StatusOK, value)
}

func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func ListenAddress(raw string) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("invalid listen address %q: %w", raw, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsUnspecified() || ip.To4() == nil {
		return "", fmt.Errorf("listen address %q must use an explicit IPv4 address", raw)
	}
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil || number == 0 {
		return "", fmt.Errorf("listen address %q must use a valid nonzero port", raw)
	}
	return net.JoinHostPort(ip.String(), strconv.FormatUint(number, 10)), nil
}

func NewHTTPServer(address string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
}
