// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArguments(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"--config"}, {"--config", "x", "--port", "0"}, {"--config", "x", "--port", "65536"}, {"--config", "x", "--port", "1 "}, {"--config", "x", "unknown", "y"}} {
		var out, err bytes.Buffer
		if code := run(context.Background(), args, &out, &err); code != 2 || out.Len() != 0 || err.String() != usage {
			t.Fatalf("%v: code=%d out=%q err=%q", args, code, out.String(), err.String())
		}
	}
	a, ok := parse([]string{"--port", " +123", "--bind", "127.0.0.2", "--config", "first", "--port", "2405", "--config", "last"})
	if !ok || a.port != 2405 || a.config != "last" || a.bind != "127.0.0.2" {
		t.Fatal(a, ok)
	}
	a, ok = parse([]string{"--config", ""})
	if !ok || a.config != "" {
		t.Fatal("empty path is config failure, not usage")
	}
}
func TestConfigError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	var out, err bytes.Buffer
	if code := run(context.Background(), []string{"--config", path}, &out, &err); code != 1 || err.Len() != 0 || !strings.HasSuffix(out.String(), " rtu config error: cannot open "+path+"\n") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), err.String())
	}
}
func TestCannotListen(t *testing.T) {
	// Binding a nonlocal numeric IPv4 address deterministically fails without
	// creating a competing process or probing the live lab's port 2404.
	var out, err bytes.Buffer
	code := run(context.Background(), []string{"--config", "../../check/points.test.json", "--bind", "192.0.2.254", "--port", "12345"}, &out, &err)
	if code != 1 || err.Len() != 0 || !strings.HasSuffix(out.String(), " rtu cannot listen on 192.0.2.254:12345\n") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), err.String())
	}
}
func TestCancelledShutdown(t *testing.T) {
	// Disable external clock and device destinations in a temporary fixture;
	// even an already-cancelled process must join all workers and close listen.
	data, e := os.ReadFile("../../check/points.test.json")
	if e != nil {
		t.Fatal(e)
	}
	data = bytes.ReplaceAll(data, []byte("172.28.104."), []byte("127.0.0."))
	config := filepath.Join(t.TempDir(), "points.json")
	if e = os.WriteFile(config, data, 0600); e != nil {
		t.Fatal(e)
	}
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, err bytes.Buffer
	code := run(ctx, []string{"--config", config, "--bind", "127.0.0.1", "--port", fmt.Sprint(port)}, &out, &err)
	if code != 0 || err.Len() != 0 || !strings.Contains(out.String(), fmt.Sprintf(" rtu listening 127.0.0.1:%d ca=1 points=42 devices=6 select-timeout=5000ms\n", port)) || !strings.HasSuffix(out.String(), " rtu stopping\n") {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), err.String())
	}
}
