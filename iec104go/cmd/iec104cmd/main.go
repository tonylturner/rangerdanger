// SPDX-License-Identifier: Apache-2.0

// iec104cmd is the lab's controlling-station CLI. Its text/JSON interface is
// intentionally compatible with the original C lab program.
package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	iec "github.com/tonylturner/iec104go"
)

type link interface {
	Send(iec.Message) error
	StartDT(context.Context) error
	StopDT(context.Context) error
	Close(context.Context) error
}

type runtime struct {
	now     func() time.Time
	clock   iec.Clock
	dial    func(context.Context, string, iec.Options, iec.Handler) (link, error)
	timeout time.Duration
}

func defaults() runtime {
	return runtime{
		now:   time.Now,
		clock: iec.DefaultOptions().Clock,
		dial: func(ctx context.Context, addr string, opts iec.Options, handler iec.Handler) (link, error) {
			return iec.Dial(ctx, addr, opts, handler)
		},
		timeout: 10 * time.Second,
	}
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr, defaults())
	cancel()
	os.Exit(code)
}

func run(ctx context.Context, argv []string, stdout, stderr io.Writer, rt runtime) int {
	a, ok := parse(argv)
	if !ok {
		fmt.Fprint(stderr, usage)
		return 2
	}
	o := &output{out: stdout, log: stdout, now: rt.now}
	if a.json {
		o.log = stderr
	}
	op := a.words[0]
	r := newResponses()
	opts := iec.DefaultOptions()
	opts.APCI.T0, opts.APCI.T1 = 10*time.Second, 10*time.Second
	opts.APCI.T2, opts.APCI.T3 = 5*time.Second, 5*time.Second
	// A coalesced 80-pair timed burst must not outrun the formatting callback.
	opts.EventQueue = 1024
	opts.Clock = rt.clock
	opts.Logf = o.logf
	o.logf("connecting to %s:%d oa=%d ca=%d", a.host, a.port, a.oa, a.ca)
	c, err := rt.dial(ctx, net.JoinHostPort(a.host, strconv.Itoa(a.port)), opts, r.handler(o, a.json && op == "monitor"))
	if err != nil {
		o.logf("connect failed: %v", err)
		return 2
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := c.Close(closeCtx); err != nil {
			o.logf("close failed: %v", err)
		}
	}()
	p := procedure{c: c, r: r, o: o, timeout: rt.timeout, a: a, clock: rt.clock}
	if !p.transfer(ctx, true) {
		o.logf("STARTDT not confirmed")
		return 2
	}
	o.logf("link started")
	if !a.validOperation() {
		// C's usage() exits immediately, without a destroy/closed callback.
		// Still join our workers, but do not append a new human log to usage.
		r.suppressCloseLog()
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch op {
	case "monitor":
		return p.monitor(ctx)
	case "gi":
		o.logf("send station GI")
		ok = p.gi(ctx, false)
		if ok {
			o.logf("GI terminated")
		} else {
			o.logf("GI not confirmed")
		}
	case "stopstart":
		ok = p.transfer(ctx, false) && p.transfer(ctx, true)
		if ok {
			o.logf("STOPDT then STARTDT confirmed")
		} else {
			o.logf("stopstart failed")
		}
	case "dc", "rc", "sc":
		ioa, value, _ := a.command()
		ok = p.command(ctx, uint32(ioa), value)
	}
	if a.json {
		o.result(op, ok)
	}
	if ok {
		return 0
	}
	return 1
}
