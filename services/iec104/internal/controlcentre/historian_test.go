// SPDX-License-Identifier: Apache-2.0

package controlcentre

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	iec "github.com/tonylturner/iec104go"
)

func TestTimedSnapshotsAndOverflow(t *testing.T) {
	c, now := model(t)
	c.historian = true
	for _, invalid := range []bool{false, true} {
		for _, r := range []struct {
			typ, untimed iec.TypeID
			ioa, index   int
			e            iec.Element
		}{{31, 3, 1001, 0, iec.DoublePoint{DIQ: 2}}, {30, 1, 1303, 20, iec.SinglePoint{SIQ: 1}}, {32, 5, 1301, 18, iec.StepPosition{Value: 3}}} {
			receive(c, r.typ, 3, r.ioa, iec.Timed{Value: r.e, Time: iec.CP56FromTime(*now, invalid)})
			receive(c, r.untimed, 20, r.ioa, r.e)
		}
	}
	if len(c.queue) != 6 {
		t.Fatal("six immediate snapshots required")
	}
	for i, b := range c.queue {
		want := `"source_time":"` + at + `","source_time_invalid":false`
		if i >= 3 {
			want = strings.Replace(want, "false", "true", 1)
		}
		if !strings.Contains(string(b), want) {
			t.Fatal(string(b))
		}
	}
	for i := 0; i < 80; i++ {
		tag := iec.CP56FromTime(now.Add(time.Duration(i)*time.Millisecond), false)
		receive(c, 30, 3, 1303, iec.Timed{Value: iec.SinglePoint{SIQ: 1}, Time: tag})
		receive(c, 1, 20, 1303, iec.SinglePoint{SIQ: 1})
	}
	if c.dropped != 70 || len(c.queue) != 16 {
		t.Fatalf("drop count %d queue %d", c.dropped, len(c.queue))
	}
	if !strings.Contains(string(c.queue[15]), `"source_time":"2026-10-15T12:34:56.868Z"`) {
		t.Fatal("newest lost")
	}
	var last uint64
	for _, b := range c.queue {
		var v struct {
			Seq uint64 `json:"snapshot_seq"`
		}
		if err := json.Unmarshal(b, &v); err != nil {
			t.Fatal(err)
		}
		if v.Seq <= last {
			t.Fatal("snapshot ordering")
		}
		last = v.Seq
	}
	if c.points[20].source != "" {
		t.Fatal("untimed report retained source tag")
	}
}
func TestPublisherHTTPAndHealth(t *testing.T) {
	var mu sync.Mutex
	var bodies [][]byte
	var sources []string
	code := 202
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		mu.Lock()
		bodies = append(bodies, b)
		sources = append(sources, r.RemoteAddr)
		status := code
		mu.Unlock()
		w.WriteHeader(status)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p, err := NewPublisher(ctx, server.URL+"/api/ingest", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if !p.push(ctx, []byte(`{"snapshot_seq":1}`)) {
		t.Fatal("2xx rejected")
	}
	mu.Lock()
	code = 503
	mu.Unlock()
	if p.push(ctx, []byte(`{}`)) {
		t.Fatal("503 accepted")
	}
	c, _ := model(t)
	c.historian = true
	done := make(chan struct{})
	go func() { defer close(done); c.publish(ctx, p) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		c.mu.Lock()
		fail := c.failures
		c.mu.Unlock()
		if fail > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("publisher never reported failure")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
	c.mu.Lock()
	equal(t, c.healthJSON(), `{"status":"ok","link":"disconnected","historian":{"last_ok_at":null,"failures":1,"dropped_snapshots":0}}`)
	c.mu.Unlock()
	mu.Lock()
	defer mu.Unlock()
	if string(bodies[0]) != `{"snapshot_seq":1}` {
		t.Fatal(string(bodies[0]))
	}
	for _, s := range sources {
		host, _, err := net.SplitHostPort(s)
		if err != nil || host != "127.0.0.1" {
			t.Fatal(s)
		}
	}
}
func TestPublisherValidationAndDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, tc := range []struct{ url, bind string }{{"http://127.0.0.1/", ""}, {"https://127.0.0.1/", "127.0.0.1"}, {"http://127.0.0.1", "127.0.0.1"}, {"http://127.0.0.1/a b", "127.0.0.1"}, {"http://127.0.0.1/", "192.0.2.1"}} {
		if _, err := NewPublisher(ctx, tc.url, tc.bind); err == nil || err.Error() != "invalid or unbindable --historian-url / --historian-bind" {
			t.Fatal(err)
		}
	}
	if p, err := NewPublisher(ctx, "", ""); p != nil || err != nil {
		t.Fatal("disabled publisher")
	}
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.Copy(io.Discard, r.Body); <-release }))
	defer server.Close()
	defer close(release)
	p, err := NewPublisher(ctx, server.URL+"/", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if p.push(ctx, []byte(`{}`)) {
		t.Fatal("silent historian succeeded")
	}
	if elapsed := time.Since(start); elapsed < 400*time.Millisecond || elapsed > time.Second {
		t.Fatal(elapsed)
	}
}
