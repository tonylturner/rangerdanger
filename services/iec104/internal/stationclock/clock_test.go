// SPDX-License-Identifier: Apache-2.0

package stationclock

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
)

func TestClockQualityAndExtrapolation(t *testing.T) {
	host := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	now := host
	body := `{"sync_status":"locked","reported_time":"2026-10-23T10:00:00.123987-02:00"}`
	var fetchErr error
	c := New(pointmap.Clock{URL: "http://gps/time", PollMS: 500, StaleAfterMS: 3000, SyncedStatus: []string{"locked", "holdover"}}, Options{
		Now: func() time.Time { return now },
		Fetch: func(ctx context.Context, _ string) ([]byte, error) {
			if d, ok := ctx.Deadline(); !ok || time.Until(d) > 500*time.Millisecond {
				t.Error("missing poll budget")
			}
			return []byte(body), fetchErr
		},
	})
	if utc, synced := c.Now(); synced || !utc.Equal(host) {
		t.Fatal(utc, synced)
	}
	if err := c.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	base := host.Add(123 * time.Millisecond)
	if utc, synced := c.Now(); !synced || !utc.Equal(base) {
		t.Fatal(utc, synced)
	}
	now = now.Add(3 * time.Second)
	if utc, synced := c.Now(); !synced || !utc.Equal(base.Add(3*time.Second)) {
		t.Fatal(utc, synced)
	}
	now = now.Add(time.Millisecond)
	if _, synced := c.Now(); synced {
		t.Fatal("stale boundary")
	}
	fetchErr = fmt.Errorf("offline")
	if err := c.Poll(context.Background()); err == nil {
		t.Fatal("accepted failed fetch")
	}
	now = now.Add(time.Second)
	if utc, synced := c.Now(); synced || !utc.Equal(base.Add(4001*time.Millisecond)) {
		t.Fatal(utc, synced)
	}
	fetchErr = nil
	body = `{"sync_status":"unsynced","reported_time":"2026-10-23T12:00:10Z"}`
	if err := c.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if utc, synced := c.Now(); synced || !utc.Equal(host.Add(10*time.Second)) {
		t.Fatal(utc, synced)
	}
	body = `{"sync_status":"holdover","reported_time":"2026-10-23T12:00:11Z"}`
	if err := c.Poll(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, synced := c.Now(); !synced {
		t.Fatal("configured synced status")
	}
}

func TestInvalidSamplesDoNotRefreshClock(t *testing.T) {
	for _, body := range []string{`{`, `null`, `{}`, `{"sync_status":null,"reported_time":"2026-10-23T12:00:00Z"}`,
		`{"sync_status":"locked","reported_time":null}`, `{"sync_status":1,"reported_time":"bad"}`,
		`{"sync_status":"locked","reported_time":"bad"}`, `{"sync_status":"locked","reported_time":"2026-02-30T12:00:00Z"}`,
		`{"sync_status":"locked","reported_time":"2026-10-23T12:00:00Zjunk"}`} {
		t.Run(body, func(t *testing.T) {
			now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
			sample := `{"sync_status":"locked","reported_time":"2026-10-23T12:00:00Z"}`
			c := New(pointmap.Clock{URL: "http://gps/", PollMS: 500, StaleAfterMS: 3000, SyncedStatus: []string{"locked"}}, Options{
				Now:   func() time.Time { return now },
				Fetch: func(context.Context, string) ([]byte, error) { return []byte(sample), nil },
			})
			if err := c.Poll(context.Background()); err != nil {
				t.Fatal(err)
			}
			now = now.Add(3001 * time.Millisecond)
			sample = body
			if err := c.Poll(context.Background()); err == nil {
				t.Fatal("accepted invalid sample")
			}
			if utc, synced := c.Now(); synced || !utc.Equal(now) {
				t.Fatal("invalid sample synchronized clock")
			}
		})
	}
}

func TestHTTPBoundary(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		wantOK bool
	}{
		{"ok", 200, `{"sync_status":"locked","reported_time":"2026-10-23T12:00:00Z"}`, true},
		{"error", 503, `{}`, false}, {"redirect", 302, `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Accept") != "application/json" || r.Method != "GET" || r.URL.Path != "/time" {
					t.Error("GET contract")
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer s.Close()
			c := New(pointmap.Clock{URL: s.URL + "/time", PollMS: 500, StaleAfterMS: 3000}, Options{})
			if err := c.Poll(context.Background()); (err == nil) != tc.wantOK {
				t.Fatal(err)
			}
		})
	}
}

func TestConcurrentNowAndCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started, done := make(chan struct{}), make(chan struct{})
	c := New(pointmap.Clock{URL: "http://gps", PollMS: 500}, Options{Fetch: func(ctx context.Context, _ string) ([]byte, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}})
	go func() { c.Run(ctx); close(done) }()
	<-started
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				c.Now()
			}
		}()
	}
	wg.Wait()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("clock worker did not join")
	}
}
