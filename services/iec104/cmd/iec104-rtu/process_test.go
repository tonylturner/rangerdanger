// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestRTUProcess(t *testing.T) {
	if os.Getenv("RD_RTU_PROCESS_TEST") == "1" {
		for i, a := range os.Args {
			if a == "--" {
				os.Args = append([]string{"iec104-rtu"}, os.Args[i+1:]...)
				main()
			}
		}
		os.Exit(99)
	}
	// Allocate and retain a local Modbus sink so the child cannot contact any
	// service belonging to the user. It does not answer, exercising cancellation
	// while the child owns an outstanding bounded southbound read.
	sink, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer sink.Close()
	held := make(chan net.Conn, 1)
	go func() {
		defer close(held)
		c, e := sink.Accept()
		if e == nil {
			held <- c
		}
	}()
	defer func() {
		sink.Close()
		for c := range held {
			c.Close()
		}
	}()
	data, err := os.ReadFile("../../check/points.test.json")
	if err != nil {
		t.Fatal(err)
	}
	var pm map[string]any
	if err = json.Unmarshal(data, &pm); err != nil {
		t.Fatal(err)
	}
	// The fixture has six devices; only keep the first and its dependent points
	// and command so this test owns exactly one southbound connection.
	devices := pm["devices"].([]any)
	dev := devices[0].(map[string]any)
	dev["host"] = "127.0.0.1"
	dev["port"] = sink.Addr().(*net.TCPAddr).Port
	dev["timeout_ms"] = 100
	pm["devices"] = devices[:1]
	id := dev["id"].(string)
	for _, key := range []string{"points", "commands"} {
		var kept []any
		for _, v := range pm[key].([]any) {
			m := v.(map[string]any)
			sub := "source"
			if key == "commands" {
				sub = "target"
			}
			if m[sub].(map[string]any)["device"] == id {
				kept = append(kept, v)
			}
		}
		pm[key] = kept
	}
	// Disable outbound HTTP with an invalid URL, an accepted point-map value.
	pm["clock"].(map[string]any)["url"] = "not-http"
	data, err = json.Marshal(pm)
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(t.TempDir(), "points.json")
	if err = os.WriteFile(config, data, 0600); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_, port, _ := net.SplitHostPort(address)
	listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRTUProcess$", "--", "--config", config, "--bind", "127.0.0.1", "--port", port)
	child.Env = append(os.Environ(), "RD_RTU_PROCESS_TEST=1")
	stdout, err := child.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stderr, err := child.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if child.ProcessState == nil {
			child.Process.Kill()
			child.Wait()
		}
	}()
	lines := make(chan string, 32)
	go func() {
		s := bufio.NewScanner(stdout)
		for s.Scan() {
			lines <- s.Text()
		}
		close(lines)
	}()
	for {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatal("child exited before listening")
			}
			if strings.Contains(line, " rtu listening ") {
				goto ready
			}
		case <-ctx.Done():
			t.Fatal("child never listened")
		}
	}
ready:
	select {
	case conn := <-held:
		if conn == nil {
			t.Fatal("Modbus sink closed before the child connected")
		}
		defer conn.Close()
		if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
			t.Fatal(err)
		}
		var request [12]byte
		if _, err := io.ReadFull(conn, request[:]); err != nil {
			t.Fatal(err)
		}
		if request[7] != 4 {
			t.Fatalf("expected FC4 poll, got %x", request)
		}
	case <-ctx.Done():
		t.Fatal("child never started the Modbus worker")
	}
	if err = child.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	stopping := false
	for line := range lines {
		if strings.HasSuffix(line, " rtu stopping") {
			stopping = true
		}
	}
	errorText, readErr := io.ReadAll(stderr)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err = child.Wait(); err != nil {
		t.Fatalf("SIGTERM exit: %v stderr=%s", err, errorText)
	}
	if !stopping || len(errorText) != 0 {
		t.Fatalf("stopping=%t stderr=%q", stopping, errorText)
	}
}
