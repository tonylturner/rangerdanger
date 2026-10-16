package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func passingIsolation(t *testing.T, cmd []string, container string) string {
	t.Helper()
	if len(cmd) != 4 || cmd[0] != "python3" || cmd[2] != isolationHTTP {
		t.Fatalf("isolation command=%q", cmd)
	}
	if container != "rangerdanger-vendor-jump" && container != "rangerdanger-eng-ws" {
		t.Fatalf("unexpected desktop %s", container)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(cmd[3]), &rows); err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		delivery := strings.HasPrefix(row["host"].(string), "10.98.")
		row["blocked"], row["status"], row["connected"] = !delivery, 0, delivery
		if delivery {
			row["status"] = 403
		}
	}
	data, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestEULab2FullVendorIsolation(t *testing.T) {
	for _, node := range []string{"vendor-jump-1", "eng-ws-1"} {
		for _, fault := range []string{"", "exec", "missing python", "empty", "malformed", "missing row", "wrong target", "management HTTP", "connected without response", "proxy allows", "proxy down", "forwarded redirect"} {
			t.Run(node+"/"+fault, func(t *testing.T) {
				gen := euGeneration(t, nil, nil)
				s := rangeServer(gen, "improved")
				s.execInContainer = func(_ context.Context, container string, cmd []string, budget int) (string, string, int, error) {
					if budget != 8 {
						t.Fatalf("isolation budget=%d", budget)
					}
					if fault == "exec" {
						return "", "", -1, errors.New("unavailable")
					}
					if fault == "missing python" {
						return "", "python3: not found", 127, nil
					}
					if fault == "malformed" {
						return "not JSON", "", 0, nil
					}
					if fault == "empty" {
						return "[]", "", 0, nil
					}
					var rows []map[string]any
					if err := json.Unmarshal([]byte(passingIsolation(t, cmd, container)), &rows); err != nil {
						t.Fatal(err)
					}
					switch fault {
					case "missing row":
						rows = rows[1:]
					case "wrong target":
						rows[0]["host"] = "127.0.0.1"
					case "management HTTP":
						rows[0]["blocked"], rows[0]["status"], rows[0]["connected"] = false, 200, true
					case "connected without response":
						rows[0]["connected"] = true
					case "proxy allows", "proxy down", "forwarded redirect":
						host := "10.98.2.2"
						if node == "eng-ws-1" {
							host = "10.98.3.2"
						}
						for _, row := range rows {
							if row["host"] == host {
								switch fault {
								case "proxy allows":
									row["status"] = 200
								case "forwarded redirect":
									row["status"] = 301
								case "proxy down":
									row["status"], row["blocked"], row["connected"] = 0, true, false
								}
							}
						}
					}
					data, _ := json.Marshal(rows)
					return string(data), "", 0, nil
				}
				check := s.conduitIsolation(t.Context(), gen, node)
				if (check.Status == "pass") != (fault == "") {
					t.Fatalf("fault=%s: %+v", fault, check)
				}
			})
		}
	}
}

func TestEULab2IsolationTargets(t *testing.T) {
	gen := euGeneration(t, nil, nil)
	targets, err := isolationTargets(gen)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, target := range targets {
		seen[target.Host] = true
	}
	for _, host := range []string{"10.99.99.2", "10.99.99.3", "10.99.99.10", "10.99.99.11", "10.98.1.2", "10.98.2.2", "10.98.3.2"} {
		if !seen[host] {
			t.Errorf("missing control/delivery target %s", host)
		}
	}
	for _, service := range gen.Manifest.Services {
		for _, iface := range service.Interfaces {
			if iface.Network == "mgmt_net" && !seen[iface.IPv4] {
				t.Errorf("missing management target %s", iface.IPv4)
			}
		}
	}
	if strings.Contains(isolationHTTP, "source_address") {
		t.Fatal("probe must not hide a bypass by binding its source")
	}
}

func TestEULab2IsolationProbe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/deny" {
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer server.Close()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	// Reserve then release a loopback listener to exercise connection refusal.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closedPort := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	targets := []isolationTarget{
		{"127.0.0.1", port, "/"}, {"127.0.0.1", port, "/deny"}, {"127.0.0.1", closedPort, "/"},
	}
	input, err := json.Marshal(targets)
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.CommandContext(t.Context(), "python3", "-c", isolationHTTP, string(input)).CombinedOutput()
	if err != nil {
		t.Fatalf("Python probe: %v: %s", err, out)
	}
	var rows []struct {
		Status    int  `json:"status"`
		Blocked   bool `json:"blocked"`
		Connected bool `json:"connected"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 || rows[0].Status != 200 || !rows[0].Connected || rows[0].Blocked ||
		rows[1].Status != 403 || !rows[1].Connected || rows[1].Blocked ||
		rows[2].Status != 0 || rows[2].Connected || !rows[2].Blocked {
		t.Fatalf("probe results = %+v", rows)
	}
}
