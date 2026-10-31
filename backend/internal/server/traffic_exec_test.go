package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

func TestTrafficSupervisorKillsAndReapsCommandOnEOF(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	// Execute the exact container supervisor locally, without Docker or a
	// range. The child reports its identity before blocking in flight.
	cmd := exec.CommandContext(ctx, "python3", "-c", trafficSupervisor,
		`exec python3 -c 'import os,time; print(os.getpid(), flush=True); time.sleep(30)'`)
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	err = cmd.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 137 {
		t.Fatalf("supervisor exit = %v, want 137", err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("child %d remains after supervisor exit: %v", pid, err)
	}
}

func TestTrafficSupervisorNormalCompletion(t *testing.T) {
	for _, code := range []int{0, 7} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "python3", "-c", trafficSupervisor, fmt.Sprintf("exit %d", code))
			// Keep stdin open as Docker does for the lifetime of an exec.
			in, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			err = cmd.Run()
			if code == 0 && err != nil {
				t.Fatal(err)
			}
			if code != 0 {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != code {
					t.Fatalf("exit = %v, want %d", err, code)
				}
			}
		})
	}
}

func TestTrafficExecCountsCompletedAttemptsRegardlessOfToolExit(t *testing.T) {
	for _, code := range []int{0, 7} {
		t.Run(strconv.Itoa(code), func(t *testing.T) {
			cli, _ := euProbeDockerFixture(t, code, "", false, nil)
			if err := executeTraffic(t.Context(), cli, resolvedTraffic{container: "source", cmd: "test-command"}); err != nil {
				t.Fatalf("completed attempt with exit %d: %v", code, err)
			}
		})
	}
}

func TestTrafficExecCancellationDrainsBeforeReturning(t *testing.T) {
	attached, eof, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan error, 1)
	var reaped atomic.Bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/exec"):
			var options container.ExecOptions
			if err := json.NewDecoder(r.Body).Decode(&options); err != nil {
				t.Error(err)
			}
			if !options.AttachStdin || !options.AttachStdout || !options.AttachStderr ||
				len(options.Cmd) != 4 || options.Cmd[0] != "python3" || options.Cmd[2] != trafficSupervisor || options.Cmd[3] != "test-command" {
				t.Errorf("exec options = %+v", options)
			}
			fmt.Fprint(w, `{"Id":"traffic-exec"}`)
		case strings.HasSuffix(r.URL.Path, "/start"):
			_, _ = io.Copy(io.Discard, r.Body)
			conn, rw, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			fmt.Fprint(rw, "HTTP/1.1 101 UPGRADED\r\nContent-Type: application/vnd.docker.raw-stream\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
			if err := rw.Flush(); err != nil {
				t.Error(err)
				return
			}
			close(attached)
			_, _ = io.Copy(io.Discard, rw)
			close(eof)
			select {
			case <-release:
			case <-time.After(3 * time.Second):
				t.Error("test did not release exec drain")
			}
			reaped.Store(true)
		case strings.HasSuffix(r.URL.Path, "/json"):
			json.NewEncoder(w).Encode(container.ExecInspect{Running: !reaped.Load(), ExitCode: 137})
		default:
			http.NotFound(w, r)
		}
	}))
	defer ts.Close()
	cli, err := client.NewClientWithOpts(client.WithHost(strings.Replace(ts.URL, "http://", "tcp://", 1)), client.WithVersion("1.47"))
	if err != nil {
		t.Fatal(err)
	}
	defer cli.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		finished <- executeTraffic(ctx, cli, resolvedTraffic{container: "test-source", cmd: "test-command"})
	}()
	awaitTraffic(t, attached)
	cancel()
	awaitTraffic(t, eof)
	select {
	case err := <-finished:
		t.Fatalf("exec returned before child exit: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-finished:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("exec error = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("exec did not return after child exit")
	}
}
