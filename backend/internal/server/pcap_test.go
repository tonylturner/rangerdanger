package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/containd"
)

func invokePcapDownloadFile(s *Server, name string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/pcap/download/"+name, nil)
	c.Params = gin.Params{{Key: "name", Value: name}}
	c.Set(generationKey, s.rng.(*fakeRange).gen)
	s.handlePcapDownloadFile(c)
	return rec
}

func TestHandlePcapDownloadFileValidatesNameBeforeContaind(t *testing.T) {
	var requests int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		if r.URL.Path != "/api/v1/pcap/download/capture_ens33_20260928_1.pcap" {
			t.Errorf("unexpected containd path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Disposition", `attachment; filename="capture_ens33_20260928_1.pcap"`)
		_, _ = io.WriteString(w, "pcap bytes")
	}))
	defer fake.Close()
	s := newTestServer(t, fake.URL)

	for _, name := range []string{
		"../etc/passwd",
		"..",
		"a/b.pcap",
		"%2e%2e",
		"",
		"capture.txt",
		`a\b.pcap`,
		strings.Repeat("a", 251) + ".pcap",
	} {
		t.Run("reject_"+name, func(t *testing.T) {
			rec := invokePcapDownloadFile(s, name)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"error":"invalid pcap file name"`) {
				t.Fatalf("unexpected error body: %s", rec.Body.String())
			}
		})
	}
	if got := atomic.LoadInt32(&requests); got != 0 {
		t.Fatalf("containd received %d requests for invalid names, want 0", got)
	}

	name := "capture_ens33_20260928_1.pcap"
	rec := invokePcapDownloadFile(s, name)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	if got := rec.Body.String(); got != "pcap bytes" {
		t.Fatalf("body = %q, want %q", got, "pcap bytes")
	}
	if got := rec.Header().Get("Content-Disposition"); got != "attachment; filename=capture_ens33_20260928_1.pcap" {
		t.Fatalf("Content-Disposition = %q", got)
	}
	if got := atomic.LoadInt32(&requests); got != 1 {
		t.Fatalf("containd received %d requests for the valid name, want 1", got)
	}
}

func TestHandlePcapDownloadSkipsInvalidListedName(t *testing.T) {
	var requests int32
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer fake.Close()
	s := newTestServer(t, fake.URL)
	s.pcap.Files = []string{"../capture.pcap"}

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/pcap/download", nil)
	s.handlePcapDownload(c)

	if got := rec.Code; got != http.StatusNotFound {
		t.Fatalf("status = %d, want fallback status %d; body: %s", got, http.StatusNotFound, rec.Body.String())
	}
	if got := atomic.LoadInt32(&requests); got != 0 {
		t.Fatalf("containd received %d requests for invalid listed name, want 0", got)
	}
}

func TestHandlePcapStartValidatesAndForwardsPrefix(t *testing.T) {
	var requests int32
	var startRequests int32
	var stopRequests int32
	prefixes := make(chan string, 1)
	pollDone := make(chan struct{}, 1)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/pcap/start":
			atomic.AddInt32(&startRequests, 1)
			var cfg containd.PcapConfig
			if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
				t.Errorf("decode start request: %v", err)
			}
			if !reflect.DeepEqual(cfg.Zones, []string{"dmz", "lan1", "lan2", "wan"}) || len(cfg.Interfaces) != 0 {
				t.Errorf("wrong selection: %+v", cfg)
				http.Error(w, "invalid capture selection", 400)
				return
			}
			prefixes <- cfg.FilePrefix
			_, _ = io.WriteString(w, `{"running":false,"interfaces":["eth2","eth7"]}`)
		case "/api/v1/pcap/stop":
			atomic.AddInt32(&stopRequests, 1)
			_, _ = io.WriteString(w, `{"running":false}`)
		case "/api/v1/pcap/status":
			_, _ = io.WriteString(w, `{"running":false}`)
		case "/api/v1/pcap/list":
			_, _ = io.WriteString(w, `[{"name":"other_eth0.pcap"},{"name":"scenario-baseline_2.v1_eth2.pcap"}]`)
			pollDone <- struct{}{}
		default:
			t.Errorf("unexpected containd path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()
	s := newTestServer(t, fake.URL)

	for i, name := range []string{
		"../capture",
		"..",
		"a/b",
		`a\b`,
		"bad name",
		strings.Repeat("a", 65),
	} {
		t.Run("reject_prefix_"+strconv.Itoa(i), func(t *testing.T) {
			rec, _ := invoke(s, s.handlePcapStart, http.MethodPost, "/api/v1/pcap/start", map[string]any{
				"name":         name,
				"duration_sec": 1,
			})
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), `"error":"invalid capture name"`) {
				t.Fatalf("unexpected error body: %s", rec.Body.String())
			}
		})
	}
	if got := atomic.LoadInt32(&requests); got != 0 {
		t.Fatalf("containd received %d requests for invalid prefixes, want 0", got)
	}

	const validPrefix = "scenario-baseline_2.v1"
	started := time.Now()
	rec, _ := invoke(s, s.handlePcapStart, http.MethodPost, "/api/v1/pcap/start", map[string]any{
		"name":         validPrefix,
		"duration_sec": 1,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var response struct {
		Interfaces []string `json:"interfaces"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || !reflect.DeepEqual(response.Interfaces, []string{"eth2", "eth7"}) {
		t.Fatalf("resolved devices lost: %s", rec.Body)
	}
	select {
	case got := <-prefixes:
		if got != validPrefix {
			t.Fatalf("containd FilePrefix = %q, want %q", got, validPrefix)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("containd did not receive the valid capture prefix")
	}
	if got := atomic.LoadInt32(&startRequests); got != 1 {
		t.Fatalf("containd received %d start requests, want 1", got)
	}
	_, state := invoke(s, s.handlePcapStatus, "GET", "/pcap/status", nil)
	if state["capturing"] != true {
		t.Fatalf("stale engine status overrode accepted start: %v", state)
	}
	select {
	case <-pollDone:
	case <-time.After(5 * time.Second):
		t.Fatal("capture poll did not complete")
	}
	waitPcapReady(t, s, 2*time.Second)
	if elapsed := time.Since(started); elapsed < time.Second {
		t.Fatalf("capture stopped before requested duration: %s", elapsed)
	}
	if got := atomic.LoadInt32(&stopRequests); got != 1 {
		t.Fatalf("stop requests = %d, want 1", got)
	}
	s.pcapMu.Lock()
	defer s.pcapMu.Unlock()
	if s.pcap.Capturing || !reflect.DeepEqual(s.pcap.Files, []string{validPrefix + "_eth2.pcap"}) {
		t.Fatalf("unexpected completed capture: %+v", s.pcap)
	}
}

func TestPcapStartDoesNotHideSelectorOrEngineFailure(t *testing.T) {
	for _, tc := range []struct {
		name, body         string
		upstream, expected int
	}{
		{"unbound member", `{"error":"zone lan2 has no bound devices"}`, 400, 502},
		{"missing interface", `{"error":"interface lan1 resolves to missing device"}`, 400, 502},
		{"no engine", `{"status":"pcap start queued"}`, 200, 503},
		{"engine failure", `{"running":false,"lastError":"capture unavailable"}`, 200, 503},
		{"running without interfaces", `{"running":true}`, 200, 503},
		{"empty interfaces", `{"running":false,"interfaces":[]}`, 201, 503},
		{"malformed acknowledgement", `{`, 200, 502},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/pcap/start" {
					t.Errorf("unexpected config mutation: %s", r.URL)
				}
				w.WriteHeader(tc.upstream)
				io.WriteString(w, tc.body)
			}))
			defer upstream.Close()
			s := newTestServer(t, upstream.URL)
			rec, _ := invoke(s, s.handlePcapStart, "POST", "/pcap/start", `{"duration_sec":1}`)
			if rec.Code != tc.expected || s.pcap.Capturing {
				t.Fatalf("false capture success: %d %s", rec.Code, rec.Body)
			}
		})
	}
}

func waitPcapReady(t *testing.T, s *Server, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.pcapMu.Lock()
		ready := s.pcap.FileReady && !s.pcap.Capturing
		s.pcapMu.Unlock()
		if ready {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("capture did not finish with files")
}

func TestPcapDurationBoundsShutdownWait(t *testing.T) {
	var stops, lists int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/pcap/start":
			w.WriteHeader(http.StatusCreated)
			io.WriteString(w, `{"running":false,"interfaces":["eth0"]}`)
		case "/api/v1/pcap/stop":
			atomic.AddInt32(&stops, 1)
			io.WriteString(w, `{"running":true}`)
		case "/api/v1/pcap/status":
			io.WriteString(w, `{"running":true}`)
		case "/api/v1/pcap/list":
			atomic.AddInt32(&lists, 1)
			io.WriteString(w, `[{"name":"bounded_eth0.pcap"},{"name":"unrelated_eth0.pcap"}]`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	s := newTestServer(t, upstream.URL)
	started := time.Now()
	rec, _ := invoke(s, s.handlePcapStart, "POST", "/pcap/start", `{"duration_sec":1,"name":"bounded"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	waitPcapReady(t, s, 20*time.Second)
	if elapsed := time.Since(started); elapsed < 16*time.Second {
		t.Fatalf("shutdown wait ended early: %s", elapsed)
	}
	if atomic.LoadInt32(&stops) != 1 || atomic.LoadInt32(&lists) != 1 {
		t.Fatalf("stop/list calls = %d/%d, want 1/1", stops, lists)
	}
	_, state := invoke(s, s.handlePcapStatus, "GET", "/pcap/status", nil)
	if state["capturing"] != false || state["file_ready"] != true {
		t.Fatalf("stale engine status overrode completion: %v", state)
	}
	s.pcapMu.Lock()
	defer s.pcapMu.Unlock()
	if !reflect.DeepEqual(s.pcap.Files, []string{"bounded_eth0.pcap"}) {
		t.Fatalf("files = %v", s.pcap.Files)
	}
}

func TestPcapUserStopDoesNotStopReplacement(t *testing.T) {
	var stops int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/pcap/start":
			io.WriteString(w, `{"running":false,"interfaces":["eth0"]}`)
		case "/api/v1/pcap/stop":
			atomic.AddInt32(&stops, 1)
			io.WriteString(w, `{"running":false}`)
		case "/api/v1/pcap/list":
			io.WriteString(w, `[{"name":"reused_eth0.pcap"}]`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	s := newTestServer(t, upstream.URL)
	t.Cleanup(func() {
		if err := s.rng.(*fakeRange).gen.Stop(time.Second); err != nil {
			t.Error(err)
		}
	})
	for i, duration := range []int{1, 60} {
		rec, _ := invoke(s, s.handlePcapStart, "POST", "/pcap/start", map[string]any{"duration_sec": duration, "name": "reused"})
		if rec.Code != 200 {
			t.Fatalf("start: %d %s", rec.Code, rec.Body)
		}
		if i == 0 {
			rec, _ = invoke(s, s.handlePcapStop, "POST", "/pcap/stop", nil)
			if rec.Code != 200 {
				t.Fatalf("stop: %d %s", rec.Code, rec.Body)
			}
			waitPcapReady(t, s, time.Second)
		}
	}
	// Let the old duration worker wake while the replacement is active.
	time.Sleep(1200 * time.Millisecond)
	s.pcapMu.Lock()
	capturing, ready := s.pcap.Capturing, s.pcap.FileReady
	s.pcapMu.Unlock()
	if !capturing || ready || atomic.LoadInt32(&stops) != 1 {
		t.Fatalf("old worker affected replacement: capturing=%v ready=%v stops=%d", capturing, ready, stops)
	}
	rec, _ := invoke(s, s.handlePcapStop, "POST", "/pcap/stop", nil)
	if rec.Code != 200 || atomic.LoadInt32(&stops) != 2 {
		t.Fatalf("replacement stop: %d %s, stops=%d", rec.Code, rec.Body, stops)
	}
}

func TestPcapStopWaitsForShutdown(t *testing.T) {
	var stops, statuses int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/pcap/start":
			io.WriteString(w, `{"running":false,"interfaces":["eth0"]}`)
		case "/api/v1/pcap/stop":
			atomic.AddInt32(&stops, 1)
			io.WriteString(w, `{"running":true}`)
		case "/api/v1/pcap/status":
			running := atomic.AddInt32(&statuses, 1) == 1
			json.NewEncoder(w).Encode(containd.PcapStatus{Running: running})
		case "/api/v1/pcap/list":
			if atomic.LoadInt32(&statuses) != 2 {
				t.Error("listed files before shutdown confirmation")
			}
			io.WriteString(w, `[{"name":"delayed_eth0.pcap"}]`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	s := newTestServer(t, upstream.URL)
	t.Cleanup(func() {
		if err := s.rng.(*fakeRange).gen.Stop(time.Second); err != nil {
			t.Error(err)
		}
	})
	rec, _ := invoke(s, s.handlePcapStart, "POST", "/pcap/start", `{"duration_sec":60,"name":"delayed"}`)
	if rec.Code != 200 {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	rec, _ = invoke(s, s.handlePcapStop, "POST", "/pcap/stop", nil)
	if rec.Code != 200 || atomic.LoadInt32(&stops) != 1 {
		t.Fatalf("stop: %d %s", rec.Code, rec.Body)
	}
	waitPcapReady(t, s, time.Second)
}

func TestPcapDurationWorkerCancelsWithGeneration(t *testing.T) {
	var stops int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/pcap/start" {
			io.WriteString(w, `{"running":false,"interfaces":["eth0"]}`)
			return
		}
		atomic.AddInt32(&stops, 1)
		http.NotFound(w, r)
	}))
	defer upstream.Close()
	s := newTestServer(t, upstream.URL)
	rec, _ := invoke(s, s.handlePcapStart, "POST", "/pcap/start", `{"duration_sec":60}`)
	if rec.Code != 200 {
		t.Fatalf("start: %d %s", rec.Code, rec.Body)
	}
	if err := s.rng.(*fakeRange).gen.Stop(time.Second); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&stops) != 0 {
		t.Fatal("cancelled duration worker contacted containd")
	}
}

func TestPcapStartRejectsPackageWithoutZones(t *testing.T) {
	s := newTestServer(t, "http://127.0.0.1:1")
	s.rng.(*fakeRange).gen.Package.Template.Networks = nil
	rec, _ := invoke(s, s.handlePcapStart, "POST", "/pcap/start", `{}`)
	if rec.Code != 422 || s.pcap.Capturing {
		t.Fatalf("empty selection accepted: %d %s", rec.Code, rec.Body)
	}
}
