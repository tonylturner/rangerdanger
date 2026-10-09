package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
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
	prefixes := make(chan string, 1)
	pollDone := make(chan struct{}, 1)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/pcap/start":
			atomic.AddInt32(&startRequests, 1)
			var cfg struct {
				FilePrefix string `json:"filePrefix"`
			}
			if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
				t.Errorf("decode start request: %v", err)
			}
			prefixes <- cfg.FilePrefix
			_, _ = io.WriteString(w, `{"running":true,"startedAt":"2026-09-28T12:00:00Z"}`)
		case "/api/v1/pcap/status":
			_, _ = io.WriteString(w, `{"running":false}`)
		case "/api/v1/pcap/list":
			_, _ = io.WriteString(w, `[]`)
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
	rec, _ := invoke(s, s.handlePcapStart, http.MethodPost, "/api/v1/pcap/start", map[string]any{
		"name":         validPrefix,
		"duration_sec": 1,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
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
	select {
	case <-pollDone:
	case <-time.After(5 * time.Second):
		t.Fatal("capture poll did not complete")
	}
}
