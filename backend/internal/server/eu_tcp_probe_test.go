package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

func TestEUStrictTCPClassification(t *testing.T) {
	for _, tc := range []struct {
		name, stderr, want string
		rc                 int
		err                error
	}{
		{"connected", "", "allow", 0, nil},
		{"bash refusal", "bash: connect: Connection refused", "allow", 1, nil},
		{"busybox refusal", "nc: can't connect to remote host (10.0.0.1): Connection refused", "allow", 1, nil},
		{"busybox timeout", "nc: timed out", "deny", 1, nil},
		{"connect timeout", "bash: connect: Connection timed out", "deny", 1, nil},
		{"timeout budget", "", "deny", 124, nil},
		{"container down", "", "skipped", -1, errors.New("container is stopped")},
		{"exec error overrides success", "", "skipped", 0, errors.New("inspect failed")},
		{"unknown exit one", "unexpected failure", "skipped", 1, nil},
		{"bad arguments", "nc: invalid option", "skipped", 2, nil},
		{"not executable", "permission denied", "skipped", 126, nil},
		{"missing tool", "timeout: not found", "skipped", 127, nil},
		{"killed", "", "skipped", 137, nil},
		{"terminated", "", "skipped", 143, nil},
		{"routing error", "No route to host", "skipped", 1, nil},
		{"bind failure", "Cannot assign requested address", "skipped", 1, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, detail := classifyStrictTCP("", tc.stderr, tc.rc, tc.err)
			if got != tc.want || (detail != "") != (got == "skipped") {
				t.Fatalf("got %q %q want %q", got, detail, tc.want)
			}
		})
	}
}

// An HTTP fixture, never a Docker daemon. Exercise the SDK's real hijacked
// framing and cancellation, not just the result classifier.
func euProbeDockerFixture(t *testing.T, rc int, stderr string, wedge bool, created func(string, container.ExecOptions)) (*client.Client, string) {
	t.Helper()
	var mu sync.Mutex
	var streams []net.Conn
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/exec"):
			var options container.ExecOptions
			if err := json.NewDecoder(r.Body).Decode(&options); err != nil {
				t.Error(err)
			}
			if created != nil {
				parts := strings.Split(r.URL.Path, "/")
				created(parts[len(parts)-2], options)
			}
			fmt.Fprint(w, `{"Id":"probe-exec"}`)
		case strings.HasSuffix(r.URL.Path, "/start"):
			io.Copy(io.Discard, r.Body)
			r.Body.Close()
			conn, rw, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			streams = append(streams, conn)
			mu.Unlock()
			fmt.Fprint(rw, "HTTP/1.1 101 UPGRADED\r\nContent-Type: application/vnd.docker.raw-stream\r\nConnection: Upgrade\r\nUpgrade: tcp\r\n\r\n")
			rw.Flush()
			if wedge {
				return // held open until the client cancels or test cleanup
			}
			defer conn.Close()
			_, _ = stdcopy.NewStdWriter(conn, stdcopy.Stderr).Write([]byte(stderr))
		case strings.HasSuffix(r.URL.Path, "/json"):
			json.NewEncoder(w).Encode(container.ExecInspect{ExitCode: rc})
		default:
			http.Error(w, "container not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(func() {
		ts.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, stream := range streams {
			stream.Close()
		}
	})
	cli, err := client.NewClientWithOpts(client.WithHost(strings.Replace(ts.URL, "http://", "tcp://", 1)), client.WithVersion("1.47"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cli.Close() })
	return cli, strings.Replace(ts.URL, "http://", "tcp://", 1)
}

func TestEUStrictTCPExecWireAndErrors(t *testing.T) {
	for _, tc := range []struct {
		name, stderr, want string
		rc                 int
	}{
		{"connect", "", "allow", 0},
		{"refused vendor VNC is reached", "Connection refused", "allow", 1},
		{"timeout", "", "deny", 124},
		{"missing tool", "busybox: not found", "skipped", 127},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			cli, _ := euProbeDockerFixture(t, tc.rc, tc.stderr, false, func(source string, options container.ExecOptions) {
				calls++
				want := []string{"sh", "-c", euTCPCommand, "probe", "10.20.20.10", "5900"}
				if source != "real-kali-container" || !reflect.DeepEqual(options.Cmd, want) {
					t.Errorf("wrong probe source/command: %s %+v", source, options)
				}
			})
			row := (&Server{}).strictTCPProbe(t.Context(), cli, validationProbe{Src: "real-kali-container", Dst: "10.20.20.10", Port: "5900", Expected: "deny", Category: "unauthorized"})
			if calls != 1 || row.Actual != tc.want || row.Pass != (tc.want == "deny") || (row.ProbeError != "") != (tc.want == "skipped") || tallyValidation([]validationRow{row}).pass != row.Pass {
				t.Fatalf("calls=%d row=%+v", calls, row)
			}
		})
	}
	row := (&Server{}).strictTCPProbe(t.Context(), nil, validationProbe{Src: "missing", Dst: "10.0.0.1", Port: "502", Expected: "deny"})
	if row.Pass || !strings.Contains(row.ProbeError, "docker client not available") {
		t.Fatal(row)
	}
	row = (&Server{}).strictTCPProbe(t.Context(), nil, validationProbe{Src: "source", Dst: "bad;input", Port: "502", Expected: "deny"})
	if row.Pass || !strings.Contains(row.ProbeError, "invalid TCP") {
		t.Fatal(row)
	}
}

func TestEUStrictTCPExecCancellation(t *testing.T) {
	cli, _ := euProbeDockerFixture(t, 0, "", true, nil)
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	row := (&Server{}).strictTCPProbe(ctx, cli, validationProbe{Src: "wedged-source", Dst: "10.0.0.1", Port: "502", Expected: "deny"})
	if time.Since(start) > time.Second || row.Pass || row.Actual != "skipped" || !strings.Contains(row.ProbeError, "deadline") {
		t.Fatalf("elapsed=%s row=%+v", time.Since(start), row)
	}
}

func TestEUProbeMatrixBoundsAndRetries(t *testing.T) {
	var active, peak atomic.Int32
	started := make(chan struct{}, euProbeConcurrency)
	release := make(chan struct{})
	matrix := make([]validationProbe, 16)
	for i := range matrix {
		matrix[i] = validationProbe{Src: fmt.Sprint(i), Expected: "deny", Category: "unauthorized"}
	}
	done := make(chan []validationRow, 1)
	go func() {
		done <- runEUProbeMatrix(t.Context(), matrix, func(ctx context.Context, p validationProbe) validationRow {
			n := active.Add(1)
			for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
			}
			if n <= euProbeConcurrency {
				select {
				case started <- struct{}{}:
				default:
				}
			}
			<-release
			active.Add(-1)
			return validationRow{validationProbe: p, Actual: "deny", Pass: true}
		})
	}()
	for i := 0; i < euProbeConcurrency; i++ {
		<-started
	}
	close(release)
	rows := <-done
	if peak.Load() != euProbeConcurrency || !tallyValidation(rows).pass {
		t.Fatalf("peak=%d rows=%+v", peak.Load(), rows)
	}
	// Exercise the total-matrix cancellation with a shorter caller deadline.
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	rows = runEUProbeMatrix(ctx, matrix, func(ctx context.Context, p validationProbe) validationRow {
		deadline, _ := ctx.Deadline()
		if time.Until(deadline) > euProbeMatrixLimit {
			t.Error("missing matrix deadline")
		}
		<-ctx.Done()
		return validationRow{validationProbe: p, Actual: "skipped", ProbeError: ctx.Err().Error()}
	})
	for _, row := range rows {
		if row.Actual != "skipped" || row.ProbeError == "" || row.Pass {
			t.Fatalf("deadline became denied: %+v", row)
		}
	}
	calls := map[string]int{}
	var mu sync.Mutex
	rows = runEUProbeMatrix(t.Context(), []validationProbe{
		{Src: "authorized", Expected: "allow", Category: "authorized"},
		{Src: "unauthorized", Expected: "deny", Category: "unauthorized"},
		{Src: "broken", Expected: "allow", Category: "authorized"},
	}, func(ctx context.Context, p validationProbe) validationRow {
		deadline, _ := ctx.Deadline()
		if remaining := time.Until(deadline); remaining <= 0 || remaining > euProbeMatrixLimit {
			t.Error("wrong matrix budget")
		}
		mu.Lock()
		calls[p.Src]++
		n := calls[p.Src]
		mu.Unlock()
		if p.Src == "broken" {
			return validationRow{validationProbe: p, Actual: "skipped", ProbeError: "exec error"}
		}
		if p.Src == "authorized" && n == 1 {
			return validationRow{validationProbe: p, Actual: "deny"}
		}
		return validationRow{validationProbe: p, Actual: "allow", Pass: p.Expected == "allow"}
	})
	if calls["authorized"] != 2 || calls["unauthorized"] != 1 || calls["broken"] != 1 || !rows[0].Pass || rows[1].Pass || rows[2].Pass {
		t.Fatal(rows)
	}
}
