// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"errors"
	"testing"

	iec "github.com/tonylturner/iec104go"
)

type failingLink struct {
	handler iec.Handler
	start   error
	stop    error
	send    error
}

func (l *failingLink) Send(iec.Message) error { return l.send }
func (l *failingLink) StartDT(ctx context.Context) error {
	if l.start == nil {
		l.handler(ctx, iec.Event{Kind: iec.Started})
	}
	return l.start
}
func (l *failingLink) StopDT(ctx context.Context) error {
	if l.stop == nil {
		l.handler(ctx, iec.Event{Kind: iec.Stopped})
	}
	return l.stop
}
func (l *failingLink) Close(ctx context.Context) error {
	l.handler(ctx, iec.Event{Kind: iec.Closed})
	return nil
}

func TestTransportFailures(t *testing.T) {
	for _, tt := range []struct {
		name string
		argv []string
		link failingLink
		code int
		tail []string
		op   string
	}{
		{"start", []string{"gi"}, failingLink{start: errors.New("start failed")}, 2, []string{"STARTDT not confirmed"}, ""},
		{"stop", []string{"stopstart"}, failingLink{stop: errors.New("stop failed")}, 1, []string{"link startdt-con", "link started", "stopstart failed"}, "stopstart"},
		{"gi-send", []string{"gi"}, failingLink{send: iec.ErrQueueFull}, 1, []string{"link startdt-con", "link started", "send station GI", "send failed: iec104go: queue full", "GI not confirmed"}, "gi"},
		{"execute-send", []string{"dc", "3001", "open"}, failingLink{send: iec.ErrConnectionGone}, 1, []string{"link startdt-con", "link started", "EXECUTE ioa=3001 val=1", "send failed: iec104go: connection gone"}, "dc"},
		{"select-send", []string{"sc", "3302", "on", "-sbo"}, failingLink{send: iec.ErrNotStarted}, 1, []string{"link startdt-con", "link started", "SELECT ioa=3302 val=1", "send failed: iec104go: transfer not started"}, "sc"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rt := testRuntime()
			rt.dial = func(ctx context.Context, _ string, opts iec.Options, h iec.Handler) (link, error) {
				if opts.EventQueue < 160 || opts.APCI.T0.Seconds() != 10 || opts.APCI.T1.Seconds() != 10 || opts.APCI.T2.Seconds() != 5 || opts.APCI.T3.Seconds() != 5 {
					t.Errorf("CLI transport options: %+v", opts)
				}
				tt.link.handler = h
				h(ctx, iec.Event{Kind: iec.Opened})
				return &tt.link, nil
			}
			var out, errout bytes.Buffer
			argv := append([]string{"host", "--json"}, tt.argv...)
			code := run(context.Background(), argv, &out, &errout, rt)
			wantOut := ""
			if tt.op != "" {
				wantOut = result(tt.op, "failed")
			}
			wantErr := logs("connecting to host:2404 oa=1 ca=1", "link opened") + logs(tt.tail...) + logs("link closed")
			if code != tt.code || out.String() != wantOut || errout.String() != wantErr {
				t.Fatalf("exit=%d stdout=%q want=%q stderr=%q want=%q", code, out.String(), wantOut, errout.String(), wantErr)
			}
		})
	}
}

func TestIntegerAndEndpointBoundaries(t *testing.T) {
	for _, tt := range []struct {
		value    string
		min, max int
		valid    bool
		want     int
	}{
		{" +255", 0, 255, true, 255},
		{"-0", 0, 255, true, 0},
		{"1 ", 0, 255, false, 0},
		{"0x10", 0, 255, false, 0},
		{"1_0", 0, 255, false, 0},
		{"9223372036854775808", 0, 255, false, 0},
		{"16777215", 0, 0xffffff, true, 0xffffff},
	} {
		t.Run(tt.value, func(t *testing.T) {
			got, valid := integer(tt.value, tt.min, tt.max)
			if valid != tt.valid || (valid && got != tt.want) {
				t.Fatalf("got=%d valid=%t", got, valid)
			}
		})
	}
	a, ok := parse([]string{"host:65535", "dc", "16777215", "open", "-oa", "255", "-ca", "65535", "-delay-ms", "600000", "--hold", "86400", "--gi-every", "86400"})
	if !ok || !a.sbo || a.port != 65535 || a.oa != 255 || a.ca != 65535 || a.delayMS != 600000 || a.hold != 86400 || a.giEvery != 86400 {
		t.Fatalf("boundary flags: %+v valid=%t", a, ok)
	}
	a, ok = parse([]string{"host", "gi", "-oa", "255", "-oa", "0"})
	if !ok || a.oa != 0 {
		t.Fatalf("last flag wins: %+v valid=%t", a, ok)
	}
}
