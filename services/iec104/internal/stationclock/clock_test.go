// SPDX-License-Identifier: Apache-2.0

package stationclock

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
)

func TestReportedTimeSyntax(t *testing.T) {
	for _, tc := range []struct {
		name, reported, wantUTC string
	}{
		{"UTC", "2026-10-23T12:00:00Z", "2026-10-23T12:00:00Z"},
		{"fraction", "2026-10-23T12:00:00.1Z", "2026-10-23T12:00:00.1Z"},
		{"nanoseconds", "2026-10-23T12:00:00.123987654Z", "2026-10-23T12:00:00.123Z"},
		// RFC 3339 does not limit fraction length; station time stays millisecond precision.
		{"long fraction", "2026-10-23T12:00:00.123987654321Z", "2026-10-23T12:00:00.123Z"},
		{"positive offset", "2026-10-23T14:30:00+02:30", "2026-10-23T12:00:00Z"},
		{"negative offset", "2026-10-23T09:30:00-02:30", "2026-10-23T12:00:00Z"},
		{"zero offset", "2026-10-23T12:00:00+00:00", "2026-10-23T12:00:00Z"},
		{"unknown offset", "2026-10-23T12:00:00-00:00", "2026-10-23T12:00:00Z"},
		{"maximum positive offset", "2026-10-23T23:59:59+23:59", "2026-10-23T00:00:59Z"},
		{"maximum negative offset", "2026-10-23T00:00:00-23:59", "2026-10-23T23:59:00Z"},
		{"leap day", "2024-02-29T23:59:59Z", "2024-02-29T23:59:59Z"},
		{"lowercase t", "2026-10-23t12:00:00Z", ""},
		{"lowercase z", "2026-10-23T12:00:00z", ""},
		{"space separator", "2026-10-23 12:00:00Z", ""},
		{"missing offset", "2026-10-23T12:00:00", ""},
		{"short hour", "2026-10-23T2:00:00Z", ""},
		{"short month", "2026-1-23T12:00:00Z", ""},
		{"short day", "2026-10-3T12:00:00Z", ""},
		{"short minute", "2026-10-23T12:0:00Z", ""},
		{"short second", "2026-10-23T12:00:0Z", ""},
		{"comma fraction", "2026-10-23T12:00:00,123Z", ""},
		{"empty fraction", "2026-10-23T12:00:00.Z", ""},
		{"month zero", "2026-00-23T12:00:00Z", ""},
		{"month overflow", "2026-13-23T12:00:00Z", ""},
		{"day zero", "2026-10-00T12:00:00Z", ""},
		{"day overflow", "2026-10-32T12:00:00Z", ""},
		{"invalid leap day", "2026-02-29T12:00:00Z", ""},
		{"hour overflow", "2026-10-23T24:00:00Z", ""},
		{"minute overflow", "2026-10-23T12:60:00Z", ""},
		{"second overflow", "2026-10-23T12:00:61Z", ""},
		// Reject even a real UTC leap second: time.Time cannot represent it.
		{"leap second unsupported", "2016-12-31T23:59:60Z", ""},
		{"positive offset hour overflow", "2026-10-23T12:00:00+24:00", ""},
		{"negative offset hour overflow", "2026-10-23T12:00:00-24:00", ""},
		{"positive offset minute overflow", "2026-10-23T12:00:00+00:60", ""},
		{"negative offset minute overflow", "2026-10-23T12:00:00-00:60", ""},
		{"short offset hour", "2026-10-23T12:00:00+1:00", ""},
		{"short offset minute", "2026-10-23T12:00:00+01:0", ""},
		{"compact offset", "2026-10-23T12:00:00+0100", ""},
		{"leading whitespace", " 2026-10-23T12:00:00Z", ""},
		{"trailing whitespace", "2026-10-23T12:00:00Z ", ""},
		{"trailing newline", "2026-10-23T12:00:00Z\n", ""},
		{"trailing junk", "2026-10-23T12:00:00Zjunk", ""},
		{"empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
			body, err := json.Marshal(map[string]string{"sync_status": "locked", "reported_time": tc.reported})
			if err != nil {
				t.Fatal(err)
			}
			sample := body
			c := New(pointmap.Clock{URL: "http://gps/", PollMS: 500, StaleAfterMS: 3000, SyncedStatus: []string{"locked"}}, Options{
				Now:   func() time.Time { return now },
				Fetch: func(context.Context, string) ([]byte, error) { return sample, nil },
			})
			err = c.Poll(context.Background())
			if tc.wantUTC != "" {
				if err != nil {
					t.Fatal(err)
				}
				want, err := time.Parse(time.RFC3339Nano, tc.wantUTC)
				if err != nil {
					t.Fatal(err)
				}
				if utc, synced := c.Now(); !synced || !utc.Equal(want) {
					t.Fatalf("got %v, synced=%v; want %v, synced=true", utc, synced, want)
				}
				return
			}
			if err == nil {
				t.Fatal("accepted malformed reported_time")
			}
			if utc, synced := c.Now(); synced || !utc.Equal(now) {
				t.Fatal("malformed first sample installed a base", utc, synced)
			}
			// A malformed locked sample must neither replace an existing shifted
			// base nor renew the age of the last successful fetch.
			sample = []byte(`{"sync_status":"locked","reported_time":"2026-10-23T13:00:00Z"}`)
			if err := c.Poll(context.Background()); err != nil {
				t.Fatal(err)
			}
			now = now.Add(time.Second)
			sample = body
			if err := c.Poll(context.Background()); err == nil {
				t.Fatal("accepted malformed replacement sample")
			}
			now = now.Add(2 * time.Second)
			if utc, synced := c.Now(); !synced || !utc.Equal(now.Add(time.Hour)) {
				t.Fatal("malformed sample changed base or quality at age limit", utc, synced)
			}
			now = now.Add(time.Nanosecond)
			if utc, synced := c.Now(); synced || !utc.Equal(now.Add(time.Hour).Truncate(time.Millisecond)) {
				t.Fatal("malformed sample refreshed age or changed base", utc, synced)
			}
		})
	}
}

func TestShiftedLockedClockFreshnessUsesFetchCompletion(t *testing.T) {
	for _, shift := range []time.Duration{-365 * 24 * time.Hour, -time.Hour, time.Hour, 365 * 24 * time.Hour} {
		t.Run(shift.String(), func(t *testing.T) {
			now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
			base := now.Add(shift)
			c := New(pointmap.Clock{URL: "http://gps/", PollMS: 500, StaleAfterMS: 3000, SyncedStatus: []string{"locked"}}, Options{
				Now: func() time.Time { return now },
				Fetch: func(context.Context, string) ([]byte, error) {
					now = now.Add(100 * time.Millisecond)
					return []byte(fmt.Sprintf(`{"sync_status":"locked","reported_time":%q}`, base.Format(time.RFC3339Nano))), nil
				},
			})
			if err := c.Poll(context.Background()); err != nil {
				t.Fatal(err)
			}
			if utc, synced := c.Now(); !synced || !utc.Equal(base) {
				t.Fatal("locked shifted clock must be accepted", utc, synced)
			}
			now = now.Add(3 * time.Second)
			if utc, synced := c.Now(); !synced || !utc.Equal(base.Add(3*time.Second)) {
				t.Fatal("must remain fresh exactly at fetch-age limit", utc, synced)
			}
			now = now.Add(time.Nanosecond)
			if utc, synced := c.Now(); synced || !utc.Equal(base.Add(3*time.Second)) {
				t.Fatal("must become stale one tick past limit without losing base", utc, synced)
			}
		})
	}
}

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
