// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	iec "github.com/tonylturner/iec104go"
)

var binary string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "rd3c-cc-tests-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binary = filepath.Join(dir, "iec104-cc")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	cmd := exec.CommandContext(ctx, "go", "build", "-race", "-o", binary, ".")
	out, err := cmd.CombinedOutput()
	cancel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n%v\n", out, err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
func fixture(t *testing.T) string {
	t.Helper()
	s, err := filepath.Abs("../../check/points.test.json")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func args(t *testing.T) []string {
	return []string{"--config", fixture(t), "--bind", "127.0.0.1", "--target", "127.0.0.1:1", "--http", "127.0.0.1:18080"}
}
func TestLoaderExecutable42Scenarios(t *testing.T) {
	source, err := os.ReadFile("../../check/loader_checks.py")
	if err != nil {
		t.Fatal(err)
	}
	// Execute the authoritative 42 scenarios unchanged apart from their container
	// paths, through the built binary rather than through a loader mock.
	text := strings.ReplaceAll(string(source), `/check/points.test.json`, fixture(t))
	text = strings.ReplaceAll(text, `"iec104-cc"`, fmt.Sprintf("%q", binary))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", "-c", text)
	cmd.Env = append(os.Environ(), "GORACE=atexit_sleep_ms=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if string(out) != "PASS 42 loader cases\n" {
		t.Fatal(string(out))
	}
}
func TestArgvAndFatalBinds(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		code int
		want string
	}{{"usage", nil, 2, usage}, {"odd", []string{"--config"}, 2, usage}, {"unknown", []string{"--wat", "x"}, 2, usage}, {"target-overflow", []string{"--config", fixture(t), "--bind", "127.0.0.1", "--target", strings.Repeat("x", 128), "--http", "127.0.0.1:18080"}, 2, usage}, {"http-overflow", []string{"--config", fixture(t), "--bind", "127.0.0.1", "--target", "localhost", "--http", strings.Repeat("x", 64) + ":8080"}, 2, usage}} {
		t.Run(tc.name, func(t *testing.T) {
			var out, err bytes.Buffer
			if code := run(context.Background(), tc.args, &out, &err); code != tc.code || out.Len() != 0 || err.String() != tc.want {
				t.Fatalf("code %d out %s err %s", code, out.String(), err.String())
			}
		})
	}
	for _, tc := range []struct{ flag, value, want string }{{"--bind", "192.0.2.1", "fatal: --bind 192.0.2.1"}, {"--bind", "localhost", "fatal: --bind localhost is not an IPv4 address"}, {"--http", "192.0.2.1:8080", "cannot listen on 192.0.2.1:8080"}, {"--http", "localhost:8080", "--http localhost is not an IPv4 address"}} {
		a := args(t)
		for i := range a {
			if a[i] == tc.flag {
				a[i+1] = tc.value
			}
		}
		var out, err bytes.Buffer
		if code := run(context.Background(), a, &out, &err); code != 1 || !strings.Contains(out.String(), tc.want) || err.Len() != 0 {
			t.Fatalf("code %d out %s err %s", code, out.String(), err.String())
		}
	}
	// Targets of 64..127 bytes must survive argument parsing and config load.
	a := args(t)
	a[5] = strings.Repeat("x", 127)
	a[3] = "192.0.2.1"
	var out, err bytes.Buffer
	if code := run(context.Background(), a, &out, &err); code != 1 || !strings.Contains(out.String(), "fatal: --bind") {
		t.Fatal(code, out.String(), err.String())
	}
}
func freeAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := l.Addr().String()
	l.Close()
	return s
}
func TestProcessSIGTERM(t *testing.T) {
	for _, phase := range []string{"unreachable", "startdt-wait", "started"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
			defer cancel()
			target := "127.0.0.1:1"
			var startReady chan struct{}
			if phase != "unreachable" {
				l, err := net.Listen("tcp4", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				defer l.Close()
				target = l.Addr().String()
				if phase == "started" {
					server, err := iec.NewServer(iec.DefaultOptions(), nil)
					if err != nil {
						t.Fatal(err)
					}
					done := make(chan error, 1)
					go func() { done <- server.Serve(ctx, l) }()
					defer func() {
						cancel()
						shutdown, stop := context.WithTimeout(context.Background(), 2*time.Second)
						defer stop()
						server.Shutdown(shutdown)
						<-done
					}()
				} else {
					startReady = make(chan struct{})
					acceptedDone := make(chan struct{})
					go func() {
						defer close(acceptedDone)
						conn, err := l.Accept()
						if err != nil {
							return
						}
						defer conn.Close()
						conn.SetReadDeadline(time.Now().Add(6 * time.Second))
						if _, err := io.ReadFull(conn, make([]byte, 6)); err == nil {
							close(startReady)
						}
						_, _ = io.Copy(io.Discard, conn)
					}()
					defer func() {
						l.Close()
						select {
						case <-acceptedDone:
						case <-time.After(2 * time.Second):
							t.Error("peer connection leaked")
						}
					}()
				}
			}
			address := freeAddress(t)
			a := []string{"--config", fixture(t), "--bind", "127.0.0.1", "--target", target, "--http", address}
			cmd := exec.CommandContext(ctx, binary, a...)
			cmd.Env = append(os.Environ(), "GORACE=atexit_sleep_ms=0")
			var output bytes.Buffer
			cmd.Stdout = &output
			cmd.Stderr = &output
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- cmd.Wait() }()
			stopped := false
			defer func() {
				if !stopped {
					cmd.Process.Kill()
					<-done
				}
			}()
			client := http.Client{Timeout: 300 * time.Millisecond}
			ready := false
			deadline := time.Now().Add(6 * time.Second)
			for time.Now().Before(deadline) {
				r, err := client.Get("http://" + address + "/api/health")
				if err == nil {
					body, _ := io.ReadAll(r.Body)
					r.Body.Close()
					if r.StatusCode == 200 && (phase != "started" || strings.Contains(string(body), `"link":"started"`)) {
						ready = true
						break
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !ready {
				t.Fatal("process never became ready")
			}
			if startReady != nil {
				select {
				case <-startReady:
				case <-ctx.Done():
					t.Fatal("STARTDT was not sent")
				}
			}
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				stopped = true
				if err != nil {
					t.Fatalf("SIGTERM: %v\n%s", err, output.String())
				}
			case <-time.After(3 * time.Second):
				t.Fatal("SIGTERM did not exit")
			}
			if !strings.Contains(output.String(), "cc stopping") {
				t.Fatal(output.String())
			}
		})
	}
}
