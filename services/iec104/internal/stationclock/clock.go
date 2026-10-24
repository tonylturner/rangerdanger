// SPDX-License-Identifier: Apache-2.0

// Package stationclock extrapolates GPS UTC using a monotonic local clock.
package stationclock

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/tturner/rangerdanger/services/iec104/internal/pointmap"
)

// Fetch is a bounded, one-shot HTTP GPS request, injectable in model tests.
type Fetch func(context.Context, string) ([]byte, error)

// Options provides deterministic clock and transport seams. Now must retain
// monotonic time (time.Now does); a test may supply a controlled time source.
type Options struct {
	Now   func() time.Time
	Fetch Fetch
	Logf  func(string, ...any)
}

// Clock owns the most recent good GPS base and poll health.
type Clock struct {
	config               pointmap.Clock
	now                  func() time.Time
	fetch                Fetch
	logf                 func(string, ...any)
	mu                   sync.Mutex
	base, baseAt, lastOK time.Time
	haveBase, lastSynced bool
}

// New constructs a clock without starting a worker.
func New(config pointmap.Clock, opts Options) *Clock {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Fetch == nil {
		opts.Fetch = fetchHTTP
	}
	return &Clock{config: config, now: opts.Now, fetch: opts.Fetch, logf: opts.Logf}
}

// Now returns best-effort UTC plus synchronization quality. GPS failure never
// stops the extrapolated base; before the first base, host time is invalid.
func (c *Clock) Now() (time.Time, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	fresh := !c.lastOK.IsZero() && now.Sub(c.lastOK) <= time.Duration(c.config.StaleAfterMS)*time.Millisecond
	utc := now
	if c.haveBase {
		utc = c.base.Add(now.Sub(c.baseAt))
	}
	return utc.UTC().Truncate(time.Millisecond), fresh && c.lastSynced
}

// Poll obtains one GPS sample with a total budget of min(poll_ms, 2000 ms).
func (c *Clock) Poll(ctx context.Context) error {
	u, err := url.Parse(c.config.URL)
	if err != nil || u.Scheme != "http" || u.Hostname() == "" {
		if c.logf != nil {
			c.logf("clock: bad url %s", c.config.URL)
		}
		return fmt.Errorf("clock: bad url %s", c.config.URL)
	}
	budget := min(time.Duration(c.config.PollMS)*time.Millisecond, 2*time.Second)
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	body, err := c.fetch(ctx, c.config.URL)
	if err != nil {
		return err
	}
	var sample struct {
		Status *string `json:"sync_status"`
		Time   *string `json:"reported_time"`
	}
	if err := json.Unmarshal(body, &sample); err != nil {
		return err
	}
	if sample.Status == nil || sample.Time == nil {
		return fmt.Errorf("clock: missing sync_status/reported_time")
	}
	utc, err := time.Parse(time.RFC3339Nano, *sample.Time)
	if err != nil {
		// A valid status alone cannot vouch for a UTC base or renew its age.
		return fmt.Errorf("clock: invalid reported_time: %w", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	c.lastOK, c.baseAt, c.base = now, now, utc.Truncate(time.Millisecond)
	c.haveBase, c.lastSynced = true, c.config.StatusSynced(*sample.Status)
	return nil
}

// Run polls immediately, then waits the reference worker's 50-ms-rounded
// interval after each completed poll. Cancellation joins the worker naturally.
// Poll failures intentionally retain the last good base until it goes stale.
func (c *Clock) Run(ctx context.Context) {
	interval := time.Duration((c.config.PollMS+49)/50) * 50 * time.Millisecond
	for ctx.Err() == nil {
		_ = c.Poll(ctx) // Lost GPS is represented by Now's stale/invalid quality.
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func fetchHTTP(ctx context.Context, address string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Close = true
	// No environment proxy, redirect, connection pool, or transparent gzip:
	// GPS is a direct, one-shot lab connection.
	transport := &http.Transport{DisableKeepAlives: true, DisableCompression: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("clock: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(b) > 1<<20 {
		return nil, fmt.Errorf("clock: response too large")
	}
	return b, nil
}
