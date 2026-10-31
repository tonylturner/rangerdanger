package containd

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestQueryEventsFiltersBeforeLimitAndPages(t *testing.T) {
	since := time.Date(2026, 10, 9, 12, 0, 1, 123, time.UTC)
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeader(t, r)
		calls++
		q := r.URL.Query()
		if q.Get("query") != "1" || q.Get("since") != since.Format(time.RFC3339Nano) || q.Get("kind") != "response" || q.Get("protocol") != "modbus" || q.Get("verdict") != "ALLOW" || q.Get("limit") != "1" {
			// The old limit-only request would return only retained old DENYs.
			io.WriteString(w, `[{"id":1,"kind":"firewall.rule.hit"}]`)
			return
		}
		switch q.Get("cursor") {
		case "":
			io.WriteString(w, `{"events":[{"id":3,"kind":"response","proto":"modbus"}],"nextCursor":"older +/=","historyTruncated":true}`)
		case "older +/=":
			// Inclusive lower bound: an event exactly at since is retained.
			io.WriteString(w, `{"events":[{"id":2,"timestamp":"2026-10-09T12:00:01.000000123Z"}],"historyTruncated":false}`)
		default:
			t.Errorf("unexpected cursor %q", q.Get("cursor"))
		}
	}))
	defer srv.Close()
	page, err := newTestClient(srv.URL).QueryEvents(t.Context(), EventQuery{Limit: 1, Since: since, Kind: "response", Protocol: "modbus", Verdict: "ALLOW"})
	if err != nil || calls != 2 || len(page.Events) != 2 || !page.HistoryTruncated || page.Events[0].Protocol != "modbus" || !page.Events[1].Timestamp.Equal(since) {
		t.Fatalf("page=%+v calls=%d err=%v", page, calls, err)
	}
}

func TestQueryEventsPreservesEmptyEngineError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"events":[],"historyTruncated":true,"engineError":"engine unreachable"}`)
	}))
	defer srv.Close()
	page, err := newTestClient(srv.URL).QueryEvents(t.Context(), EventQuery{Limit: 10})
	if err != nil || page.EngineError != "engine unreachable" || !page.HistoryTruncated || len(page.Events) != 0 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
}

func TestQueryEventsRejectsInvalidPages(t *testing.T) {
	for _, body := range []string{`[]`, `{}`, `{"events":[],"historyTruncated":false,"nextCursor":"same"}`} {
		t.Run(body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, body) }))
			defer srv.Close()
			if _, err := newTestClient(srv.URL).QueryEvents(t.Context(), EventQuery{Limit: 10}); err == nil {
				t.Fatal("accepted invalid page or repeated cursor")
			}
		})
	}
}

func TestQueryEventsHTTPFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Error(w, "unsupported query engine", 502) }))
	defer srv.Close()
	_, err := newTestClient(srv.URL).QueryEvents(t.Context(), EventQuery{Limit: 10})
	if err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("err=%v", err)
	}
}
