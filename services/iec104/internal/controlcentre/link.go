// SPDX-License-Identifier: Apache-2.0

package controlcentre

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	iec "github.com/tonylturner/iec104go"
)

// Transport is one connection; it never reconnects itself.
type Transport interface {
	Send(iec.Message) error
	StartDT(context.Context) error
	Close(context.Context) error
	Done() <-chan struct{}
}

// DialFunc creates one fresh connection with ordered protocol callbacks.
type DialFunc func(context.Context, string, iec.Options, iec.Handler) (Transport, error)

func dial(ctx context.Context, target string, o iec.Options, h iec.Handler) (Transport, error) {
	return iec.Dial(ctx, target, o, h)
}
func (c *Centre) protocolOptions(bind string) iec.Options {
	o := iec.DefaultOptions()
	t := c.pm.Timers
	o.APCI = iec.APCI{K: t.K, W: t.W, T0: time.Duration(t.T0) * time.Second, T1: time.Duration(t.T1) * time.Second, T2: time.Duration(t.T2) * time.Second, T3: time.Duration(t.T3) * time.Second}
	o.LocalAddress = &net.TCPAddr{IP: net.ParseIP(bind)}
	o.EventQueue = 1024
	o.Logf = c.printf
	o.Tap = func(d iec.Direction, _ uint64, _ time.Time, _ []byte) {
		if d == iec.Receive {
			c.mu.Lock()
			c.link.lastAt = c.opts.Now()
			c.link.lastRx = c.stamp()
			c.mu.Unlock()
		}
	}
	return o
}
func pause(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
func (c *Centre) closeSession(s Transport, reason string) error {
	c.mu.Lock()
	if reason != "" {
		c.link.reason = reason
	}
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.Close(ctx)
}
func (c *Centre) runSession(ctx context.Context, bind string) error {
	c.mu.Lock()
	c.link.state = "connecting"
	c.link.since = c.stamp()
	c.link.reconnect = false
	c.link.reason = ""
	c.link.failed = false
	c.mu.Unlock()
	h := func(callback context.Context, e iec.Event) {
		if ctx.Err() != nil {
			c.mu.Lock()
			c.link.shutdown = true
			c.mu.Unlock()
		}
		c.handle(callback, e)
	}
	s, err := c.opts.Dial(ctx, c.target, c.protocolOptions(bind), h)
	if err != nil {
		c.mu.Lock()
		if ctx.Err() != nil {
			c.link.shutdown = true
		}
		c.printf("link failed")
		c.ended()
		c.mu.Unlock()
		return nil
	}
	c.mu.Lock()
	c.session = s
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.session = nil; c.mu.Unlock() }()
	started, stop := context.WithTimeout(ctx, time.Duration(c.pm.Timers.T1+1)*time.Second)
	err = s.StartDT(started)
	stop()
	if err != nil {
		return c.closeSession(s, "")
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			c.mu.Lock()
			c.link.shutdown = true
			c.mu.Unlock()
			return c.closeSession(s, "")
		case <-s.Done():
			return nil
		case <-ticker.C:
			c.mu.Lock()
			now := c.opts.Now()
			reconnect := c.link.reconnect
			failed := c.link.failed || (c.link.gi == "running" && now.Sub(c.link.start) >= 2*time.Duration(c.pm.Timers.T1)*time.Second)
			c.watchdog(now)
			trigger := ""
			if !reconnect && !failed {
				trigger = c.tickGI(now)
			}
			c.mu.Unlock()
			if reconnect {
				return c.closeSession(s, "operator_reconnect")
			}
			if failed {
				return c.closeSession(s, "io_error")
			}
			if trigger != "" {
				m := iec.ASDU{Header: iec.Header{Type: iec.C_IC_NA_1, Cause: 6, CommonAddress: uint16(c.pm.CA)}, Objects: []iec.Object{{Address: 0, Element: iec.Interrogation{QOI: 20}}}}
				err := s.Send(m)
				if err != nil {
					c.sendFailures.Add(1)
					c.printf("send error: %v", err)
				}
				c.printf("sent station GI ca=%d trigger=%s sent=%d", c.pm.CA, trigger, boolInt(err == nil))
				if err != nil && trigger == "connect" {
					return c.closeSession(s, "io_error")
				}
			}
		}
	}
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// Serve owns the listener and joins the link, HTTP and optional publisher workers.
func (c *Centre) Serve(ctx context.Context, listener net.Listener, bind string, publisher *Publisher) error {
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	defer listener.Close()
	var workers sync.WaitGroup
	if publisher != nil {
		c.mu.Lock()
		c.historian = true
		c.mu.Unlock()
		workers.Add(1)
		go func() { defer workers.Done(); c.publish(run, publisher) }()
	}
	workers.Add(1)
	linkDone := make(chan error, 1)
	go func() {
		defer workers.Done()
		for run.Err() == nil {
			if err := c.runSession(run, bind); err != nil {
				linkDone <- err
				return
			}
			if !pause(run, time.Duration(c.pm.ReconnectIntervalMS)*time.Millisecond) {
				break
			}
		}
		linkDone <- nil
	}()
	httpDone := make(chan error, 1)
	go func() { httpDone <- c.serveHTTP(run, listener) }()
	var result error
	httpFinished, linkFinished := false, false
	select {
	case <-ctx.Done():
	case result = <-linkDone:
		linkFinished = true
	case result = <-httpDone:
		httpFinished = true
	}
	c.printf("stopping")
	c.mu.Lock()
	c.link.shutdown = true
	c.notify()
	c.mu.Unlock()
	cancel()
	listener.Close()
	workers.Wait()
	if !httpFinished {
		err := <-httpDone
		if result == nil {
			result = err
		}
	}
	if !linkFinished {
		err := <-linkDone
		if result == nil {
			result = err
		}
	}
	if result != nil {
		return fmt.Errorf("control centre: %w", result)
	}
	return nil
}
