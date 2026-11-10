package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

// Model the real HTTP boundary: separate journal/state reads, asynchronous
// 202/409 GI acceptance, and protocol evidence that arrives on later polls.
func suiteCCFixture(t *testing.T, mode string) (*Server, *lifecycle.Generation, *atomic.Int32, *atomic.Bool) {
	t.Helper()
	ev := passingEUWorkshop(t, euWorkshopSlots[0])
	var posts atomic.Int32
	var completed atomic.Bool
	poll := 0 // accessed only by the HTTP handler
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encode := func(value any) {
			t.Helper()
			if err := json.NewEncoder(w).Encode(value); err != nil {
				t.Error(err)
			}
		}
		switch r.URL.Path {
		case "/cc/api/gi":
			if r.Method != http.MethodPost || r.ContentLength > 0 {
				t.Errorf("GI request: %s, length %d", r.Method, r.ContentLength)
			}
			posts.Add(1)
			if poll == 1 && mode == "recover" {
				t.Error("requested GI before started")
			}
			if mode == "pending" || mode == "pending-timeout" {
				w.WriteHeader(http.StatusConflict)
				fmt.Fprint(w, `{"error":"interrogation already running"}`)
			} else if mode == "unavailable" {
				w.WriteHeader(http.StatusServiceUnavailable)
				fmt.Fprint(w, `{"error":"link not started"}`)
			} else {
				w.WriteHeader(http.StatusAccepted)
				fmt.Fprint(w, `{"trigger":"operator","requested_at":"2026-11-10T12:00:00Z"}`)
			}
		case "/cc/api/journal":
			poll++
			if mode == "read-error" && poll >= 3 {
				http.Error(w, "journal unavailable", http.StatusServiceUnavailable)
				return
			}
			// Retained old GI can be absent, expired, or still valid. None
			// should stand in for completing the newly requested/pending GI.
			journal := *ev.journal
			journal.Entries = append([]journalEntry(nil), ev.journal.Entries...)
			for i := range journal.Entries {
				journal.Entries[i].At = time.Now().Add(-time.Second)
				if mode == "expired" {
					journal.Entries[i].At = time.Now().Add(-11 * time.Minute)
				}
			}
			if mode == "evicted" || mode == "recover" || mode == "timeout" || mode == "down" || mode == "read-error" {
				journal.Entries = nil
			}
			journal.LatestSeq = 50
			journal.OldestSeq = 1
			if posts.Load() > 0 && poll >= 4 && mode != "timeout" && mode != "pending-timeout" && mode != "unavailable" {
				completed.Store(true)
				start := time.Now().Add(-100 * time.Millisecond)
				seq := ev.cc.Link.ConnectionSeq
				if mode == "epoch-race" && poll >= 5 {
					journal.CacheEpoch = "new-epoch"
					seq++
					journal.LatestSeq = 2 // sequence numbers restart on a new epoch
				} else {
					journal.LatestSeq = 52
				}
				journal.Entries = []journalEntry{
					{Seq: journal.LatestSeq - 1, At: start, Kind: "gi_started", Detail: journalDetail{ConnectionSeq: &seq}},
					{Seq: journal.LatestSeq, At: start.Add(time.Millisecond), Kind: "gi_complete", Detail: journalDetail{ConnectionSeq: &seq}},
				}
			}
			encode(journal)
		case "/cc/api/state":
			state := *ev.cc
			state.Points = append([]conduitPoint(nil), ev.cc.Points...)
			for i := range state.Points {
				state.Points[i].ReceivedAt = time.Now().UTC()
			}
			if mode == "down" || mode == "recover" && poll == 1 {
				state.Link.State = "connecting"
			}
			if mode == "epoch-race" && poll >= 4 {
				state.CacheEpoch = "new-epoch"
				state.Link.ConnectionSeq++
			}
			encode(state)
		case "/breaker-1/api/state":
			encode(ev.devices["breaker-1"])
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(upstream.Close)
	gen := multiDeviceGeneration(t, upstream.URL)
	for i := range gen.Manifest.Services {
		if gen.Manifest.Services[i].Key == "control_centre" {
			gen.Manifest.Services[i].Endpoints["api"] = upstream.URL + "/cc"
		}
	}
	return rangeServer(gen, "weak"), gen, &posts, &completed
}

func TestSuiteCCPreparation(t *testing.T) {
	for _, mode := range []string{"recover", "evicted", "expired", "retained", "pending", "epoch-race"} {
		t.Run(mode, func(t *testing.T) {
			_, gen, posts, completed := suiteCCFixture(t, mode)
			result := prepareSuiteCC(t.Context(), gen, 3*time.Second)
			if !result.Success || posts.Load() != 1 || !completed.Load() {
				t.Fatalf("result=%+v GI posts=%d completed=%t", result, posts.Load(), completed.Load())
			}
		})
	}
}

func TestSuiteCCPreparationTimeout(t *testing.T) {
	for _, mode := range []string{"timeout", "pending-timeout", "down", "read-error", "unavailable"} {
		t.Run(mode, func(t *testing.T) {
			_, gen, posts, _ := suiteCCFixture(t, mode)
			result := prepareSuiteCC(t.Context(), gen, 550*time.Millisecond)
			want := "no retained matched GI start/completion"
			if mode == "pending-timeout" {
				// Old valid GI must not permit passing a still-pending request.
				want = "waiting for requested/pending GI completion"
			} else if mode == "unavailable" {
				want = "GI not accepted on this connection"
			}
			wantPosts := int32(1)
			if mode == "down" {
				want = "schema-3 state and journal must share a live CC cache epoch"
				wantPosts = 0
			}
			if result.Success || !strings.Contains(result.Detail, want) ||
				!strings.Contains(result.Detail, "context deadline exceeded") || posts.Load() != wantPosts {
				t.Fatalf("result=%+v GI posts=%d", result, posts.Load())
			}
		})
	}
}

func TestSuiteCCPreparationCancellation(t *testing.T) {
	_, gen, posts, _ := suiteCCFixture(t, "timeout")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	result := prepareSuiteCC(ctx, gen, time.Minute)
	if result.Success || !strings.Contains(result.Detail, "context canceled") || posts.Load() != 0 {
		t.Fatalf("result=%+v GI posts=%d", result, posts.Load())
	}
}

func TestSuiteCCChecksKeepOriginalExpectations(t *testing.T) {
	s, gen, posts, _ := suiteCCFixture(t, "evicted")
	results := s.executeSuiteCheck(t.Context(), gen, map[string]any{"cc_link_fresh": true, "breaker_closed": false})
	if len(results) != 2 || results[0].Action != "Breaker closed" || results[0].Success ||
		results[1].Action != "CC link, GI and station delivery" || !results[1].Success || posts.Load() != 1 {
		t.Fatalf("original expectations lost: %+v posts=%d", results, posts.Load())
	}
}

func TestSuiteCCFailedPreparationIsFailedStep(t *testing.T) {
	s, gen, posts, _ := suiteCCFixture(t, "timeout")
	ctx, cancel := context.WithTimeout(t.Context(), 550*time.Millisecond)
	defer cancel()
	results := s.executeSuiteCheck(ctx, gen, map[string]any{"cc_link_fresh": true})
	if len(results) != 1 || results[0].Success || results[0].Action != "CC link, GI and station delivery" ||
		!strings.Contains(results[0].Detail, "no retained matched GI start/completion") || posts.Load() != 1 {
		t.Fatalf("result=%+v GI posts=%d", results, posts.Load())
	}
}

func TestSuiteCCPreparationOnlyForPositiveEUExpectation(t *testing.T) {
	for _, expect := range []map[string]any{
		{"breaker_closed": true},
		{"cc_link_fresh": false},
		{"cc_link_fresh": "true"},
	} {
		t.Run(fmt.Sprint(expect), func(t *testing.T) {
			s, gen, posts, _ := suiteCCFixture(t, "retained")
			want := s.executeCheck(t.Context(), gen, expect)
			got := s.executeSuiteCheck(t.Context(), gen, expect)
			if !reflect.DeepEqual(got, want) || posts.Load() != 0 {
				t.Fatalf("got=%+v want=%+v posts=%d", got, want, posts.Load())
			}
		})
	}
	s, gen, posts, _ := suiteCCFixture(t, "retained")
	gen.Package.ID = "non-eu"
	expect := map[string]any{"cc_link_fresh": true}
	if got, want := s.executeSuiteCheck(t.Context(), gen, expect), s.executeCheck(t.Context(), gen, expect); !reflect.DeepEqual(got, want) || posts.Load() != 0 {
		t.Fatalf("non-EU dispatch changed: got=%+v want=%+v posts=%d", got, want, posts.Load())
	}
}

func TestSuiteCCBudget(t *testing.T) {
	s, gen, _, _ := suiteCCFixture(t, "retained")
	path := filepath.Join(s.cfg.LabDefinitionsPath, "packages", gen.Package.ID, "protocols", "points.json")
	got, err := suiteCCBudget(path)
	if err != nil || got != 58400*time.Millisecond {
		t.Fatalf("package budget=%s err=%v", got, err)
	}
	path = filepath.Join(t.TempDir(), "points.json")
	for _, tc := range []struct {
		body string
		want time.Duration
	}{
		{`{"timers":{"t0_s":1,"t1_s":2},"reconnect_interval_ms":300}`, 8700 * time.Millisecond},
		{`{}`, 0},
		{`{"timers":{"t0_s":1,"t1_s":-1},"reconnect_interval_ms":300}`, 0},
		{`broken`, 0},
	} {
		if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := suiteCCBudget(path)
		if got != tc.want || (err != nil) != (tc.want == 0) {
			t.Fatalf("budget=%s err=%v body=%s", got, err, tc.body)
		}
	}
}

func TestSuiteGIHTTPBoundary(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
	}))
	defer target.Close()
	for _, mode := range []string{"length", "chunked", "close", "redirect", "bad-json", "too-large", "status"} {
		t.Run(mode, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "redirect" {
					http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
					return
				}
				if mode == "status" {
					http.Error(w, "unavailable", http.StatusInternalServerError)
					return
				}
				body := `{"trigger":"operator"}`
				if mode == "bad-json" {
					body = "not json"
				} else if mode == "too-large" {
					body = `{"padding":"` + strings.Repeat("é", maxUpstreamBody/2) + `"}`
				}
				if mode == "close" {
					conn, rw, err := w.(http.Hijacker).Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					defer conn.Close()
					fmt.Fprintf(rw, "HTTP/1.0 202 Accepted\r\n\r\n%s", body)
					if err := rw.Flush(); err != nil {
						t.Error(err)
					}
					return
				}
				if mode == "length" {
					w.Header().Set("Content-Length", fmt.Sprint(len(body)))
				}
				w.WriteHeader(http.StatusAccepted)
				if mode == "chunked" {
					w.(http.Flusher).Flush()
				}
				fmt.Fprint(w, body)
			}))
			defer upstream.Close()
			status, err := requestSuiteGI(t.Context(), upstream.URL)
			wantStatus := http.StatusAccepted
			if mode == "redirect" {
				wantStatus = http.StatusTemporaryRedirect
			} else if mode == "status" {
				wantStatus = http.StatusInternalServerError
			}
			if status != wantStatus || (err != nil) != (mode == "bad-json" || mode == "too-large") {
				t.Fatalf("status=%d err=%v", status, err)
			}
		})
	}
	if redirected.Load() != 0 {
		t.Fatal("GI POST followed a redirect")
	}
}
