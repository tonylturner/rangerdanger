package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
)

func invokePcapDownloadFile(s *Server, name string) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/pcap/download/"+name, nil)
	c.Params = gin.Params{{Key: "name", Value: name}}
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
