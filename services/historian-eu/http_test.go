package main

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

const historyQuery = "?point=oltc-1-tap&since=2026-10-15T09:00:00Z&until=2026-10-15T11:00:00Z"

func TestListenerRouteAndMethodIsolation(t *testing.T) {
	s := newStore(time.Now)
	ingest := httptest.NewServer(s.ingestHandler())
	defer ingest.Close()
	read := httptest.NewServer(s.readHandler())
	defer read.Close()
	accepted := uint64(0)
	for _, route := range []string{
		"/api/ingest", "/api/points", "/api/history" + historyQuery, "/api/health",
		"/api/commands", "/api/reset", "/api/state", "/api/ingest/", "/", "//api/ingest",
	} {
		for _, verb := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"} {
			for _, listener := range []struct {
				name string
				url  string
				read bool
			}{{"ingest", ingest.URL, false}, {"dmz-read", read.URL, true}} {
				t.Run(listener.name+"-"+verb+"-"+route, func(t *testing.T) {
					want := http.StatusNotFound
					known := route == "/api/ingest" && !listener.read ||
						listener.read && (route == "/api/points" || route == "/api/health" || strings.HasPrefix(route, "/api/history?"))
					allowed := http.MethodPost
					if listener.read {
						allowed = http.MethodGet
					}
					if known {
						want = http.StatusMethodNotAllowed
						if verb == allowed {
							want = http.StatusOK
							if !listener.read {
								accepted++
							}
						}
					}
					req, err := http.NewRequest(verb, listener.url+route, strings.NewReader(fixture))
					if err != nil {
						t.Fatal(err)
					}
					response, err := listenerClient.Do(req)
					if err != nil {
						t.Fatal(err)
					}
					defer response.Body.Close()
					body, err := io.ReadAll(response.Body)
					if err != nil {
						t.Fatal(err)
					}
					if response.StatusCode != want {
						t.Fatalf("got %d want %d", response.StatusCode, want)
					}
					if want == http.StatusMethodNotAllowed && response.Header.Get("Allow") != allowed {
						t.Fatalf("wrong Allow: %s", response.Header.Get("Allow"))
					}
					if want >= 400 {
						if verb == http.MethodHead {
							if response.Header.Get("Content-Type") != "application/json" || len(body) != 0 {
								t.Fatalf("HEAD error must have JSON headers but no body: %s %s", response.Header, body)
							}
						} else {
							assertJSONError(t, response.Header, body, "")
						}
					}
				})
			}
		}
	}
	if h := s.health(); h.IngestCount != accepted || accepted != 1 || h.RejectedCount != 0 {
		t.Fatalf("non-ingest routes/methods altered data: %+v", h)
	}
}

var listenerClient = &http.Client{
	Timeout: 2 * time.Second,
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

func TestReadAPI(t *testing.T) {
	now := testClock()
	s := newStore(func() time.Time { return now })
	request := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.readHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		return w
	}
	initial := request("/api/health")
	var h health
	if err := json.Unmarshal(initial.Body.Bytes(), &h); err != nil {
		t.Fatal(err)
	}
	if h.Status != "degraded" || h.LastIngestAt != nil || h.IngestCount != 0 || h.RejectedCount != 0 {
		t.Fatalf("initial health: %+v", h)
	}
	if w := request("/api/points"); w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("initial points: %d %s", w.Code, w.Body)
	}
	s.append(fixtureState(t))
	s.reject()
	w := request("/api/points")
	var points []pointLast
	if err := json.Unmarshal(w.Body.Bytes(), &points); err != nil || len(points) != 4 {
		t.Fatalf("points: %s, %v", w.Body, err)
	}
	for i := 1; i < len(points); i++ {
		if points[i-1].ID >= points[i].ID {
			t.Fatal("points not deterministically ordered")
		}
	}
	w = request("/api/history" + historyQuery)
	var result history
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || len(result.Segments) != 1 || len(result.Gaps) != 0 {
		t.Fatalf("history: %s, %v", w.Body, err)
	}
	if result.Point != "oltc-1-tap" || result.Segments[0].Samples[0].IngestedAt != now {
		t.Fatalf("history sample: %+v", result)
	}
	var raw map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	sample := raw["segments"].([]any)[0].(map[string]any)["samples"].([]any)[0].(map[string]any)
	if _, ok := sample["received_at"]; ok {
		t.Fatal("CC timestamp was not renamed")
	}
	if sample["cc_received_at"] != "2026-10-15T10:00:00Z" || sample["snapshot_seq"] != float64(1) {
		t.Fatalf("sample wire fields: %+v", sample)
	}
	for _, p := range points {
		if p.ID == "oltc-1-tap" && (p.IOA != 1301 || p.ASDU != "M_ST_TB_1" ||
			string(p.Last.Value) != `{"value":-3,"transient":true}`) {
			t.Fatalf("last sample: %+v", p)
		}
	}
	w = request("/api/health")
	if err := json.Unmarshal(w.Body.Bytes(), &h); err != nil {
		t.Fatal(err)
	}
	if h.Status != "ok" || h.IngestCount != 1 || h.RejectedCount != 1 || h.LastIngestAt == nil || !h.LastIngestAt.Equal(now) {
		t.Fatalf("health wire counters: %+v", h)
	}
	for _, route := range []string{"/api/points", "/api/history" + historyQuery, "/api/health"} {
		w := request(route)
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "application/json" {
			t.Fatalf("unsafe cache/content headers: %+v", w.Header())
		}
	}
}

func TestHistoryQueryValidation(t *testing.T) {
	s := newStore(testClock)
	s.append(fixtureState(t))
	for _, tc := range []struct {
		query string
		code  int
	}{
		{"", 400},
		{"?point=oltc-1-tap", 400},
		{historyQuery + "&point=oltc-1-tap", 400},
		{historyQuery + "&extra=x", 400},
		{historyQuery + "&bad=%xx", 400},
		{strings.Replace(historyQuery, "09:00", "12:00", 1), 400},
		{strings.Replace(historyQuery, "09:00:00Z", "bad", 1), 400},
		{strings.Replace(historyQuery, "09:00:00Z", url.QueryEscape("09:00:00+00:00"), 1), 400},
		{strings.Replace(historyQuery, "oltc-1-tap", "", 1), 400},
		{strings.Replace(historyQuery, "oltc-1-tap", "unknown", 1), 404},
		{historyQuery, 200},
		{strings.Replace(historyQuery, "11:00", "09:30", 1), 200},
	} {
		t.Run(tc.query, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.readHandler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/history"+tc.query, nil))
			if w.Code != tc.code {
				t.Fatalf("got %d want %d: %s", w.Code, tc.code, w.Body)
			}
			if tc.code >= 400 {
				assertJSONError(t, w.Header(), w.Body.Bytes(), "")
			}
		})
	}
}

func assertJSONError(t *testing.T, header http.Header, body []byte, want string) {
	t.Helper()
	if header.Get("Content-Type") != "application/json" || header.Get("Cache-Control") != "no-store" {
		t.Fatalf("error headers: %+v", header)
	}
	var result map[string]string
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("error is not a JSON object: %s (%v)", body, err)
	}
	if len(result) != 1 || result["error"] == "" || want != "" && result["error"] != want {
		t.Fatalf("error object: %s, want error=%q", body, want)
	}
}

func TestJSONErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ingest  bool
		method  string
		path    string
		body    io.Reader
		code    int
		message string
	}{
		{"history-query", false, "GET", "/api/history", nil, 400, "point, since and until are required exactly once (UTC RFC 3339)"},
		{"history-order", false, "GET", "/api/history" + strings.Replace(historyQuery, "09:00", "12:00", 1), nil, 400, "until precedes since"},
		{"unknown-point", false, "GET", "/api/history" + strings.Replace(historyQuery, "oltc-1-tap", "unknown", 1), nil, 404, "point not found"},
		{"read-route", false, "GET", "/api/ingest", nil, 404, "not found"},
		{"read-method", false, "POST", "/api/points", nil, 405, "method not allowed"},
		{"ingest-route", true, "POST", "/api/points", nil, 404, "not found"},
		{"ingest-method", true, "GET", "/api/ingest", nil, 405, "method not allowed"},
		{"ingest-schema", true, "POST", "/api/ingest", strings.NewReader(`{"schema":1}`), 400, "expected a complete CC state schema 2"},
		{"ingest-malformed", true, "POST", "/api/ingest", strings.NewReader(`{`), 400, "EOF"},
		{"ingest-oversize", true, "POST", "/api/ingest", strings.NewReader(strings.Repeat(" ", maxBodyBytes+1)), 400, "invalid ingest body"},
		{"ingest-read-failure", true, "POST", "/api/ingest", iotest.ErrReader(errors.New("broken body")), 400, "invalid ingest body"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newStore(testClock)
			w := httptest.NewRecorder()
			handler := s.readHandler()
			if tc.ingest {
				handler = s.ingestHandler()
			}
			handler.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, tc.body))
			if w.Code != tc.code {
				t.Fatalf("got %d want %d: %s", w.Code, tc.code, w.Body)
			}
			assertJSONError(t, w.Header(), w.Body.Bytes(), tc.message)
			wantRejected := uint64(0)
			if tc.ingest && tc.code == 400 {
				wantRejected = 1
			}
			if h := s.health(); h.IngestCount != 0 || h.RejectedCount != wantRejected {
				t.Fatalf("error changed ingest/counter semantics: %+v", h)
			}
		})
	}
}
