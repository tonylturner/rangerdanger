package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"
)

func writeJSON(w http.ResponseWriter, code int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	// Client disconnects are not ingest failures and cannot change stored data.
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, code int, message string) {
	writeJSON(w, code, map[string]string{"error": message})
}

func method(w http.ResponseWriter, r *http.Request, allowed string) bool {
	if r.Method == allowed {
		return true
	}
	w.Header().Set("Allow", allowed)
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	return false
}

func (s *store) ingestHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ingest" {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if !method(w, r, http.MethodPost) {
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
		if err != nil {
			s.reject()
			writeError(w, http.StatusBadRequest, "invalid ingest body")
			return
		}
		state, err := decodeState(body)
		if err != nil {
			s.reject()
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.append(state)
		writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
	})
}

func (s *store) readHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/points", "/api/history", "/api/health":
		default:
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if !method(w, r, http.MethodGet) {
			return
		}
		switch r.URL.Path {
		case "/api/points":
			writeJSON(w, http.StatusOK, s.points())
		case "/api/health":
			writeJSON(w, http.StatusOK, s.health())
		case "/api/history":
			s.serveHistory(w, r)
		}
	})
}

func (s *store) serveHistory(w http.ResponseWriter, r *http.Request) {
	q, err := url.ParseQuery(r.URL.RawQuery)
	id := q.Get("point")
	valid := len(q) == 3 && len(q["point"]) == 1 && len(q["since"]) == 1 && len(q["until"]) == 1
	if err != nil || !valid || !text(63)(id) || !stamp(q.Get("since")) || !stamp(q.Get("until")) {
		writeError(w, http.StatusBadRequest, "point, since and until are required exactly once (UTC RFC 3339)")
		return
	}
	since, _ := time.Parse(time.RFC3339Nano, q.Get("since"))
	until, _ := time.Parse(time.RFC3339Nano, q.Get("until"))
	if until.Before(since) {
		writeError(w, http.StatusBadRequest, "until precedes since")
		return
	}
	result, found := s.history(id, since, until)
	if !found {
		writeError(w, http.StatusNotFound, "point not found")
		return
	}
	writeJSON(w, http.StatusOK, result)
}
