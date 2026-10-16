package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
)

func TestConduitsRegistration(t *testing.T) {
	want := []string{labs.CapabilityPolicyContaind, labs.CapabilityProtocolIEC104, labs.CapabilityProcessTruth}
	if !slices.Equal(ValidatorRequirements()["eu-firewall-implementation"], want) || scenarioValidators["eu-firewall-implementation"].run == nil {
		t.Fatal("missing conduit registration or wrong capability contract")
	}
}

// Canonical content now mirrors the workshop. Evidence primitives below keep
// their original strict collection and rejection tests independently of slots.
func TestConduitsShippedCurriculum(t *testing.T) {
	catalog, err := labs.NewLoader(filepath.Join(repoRoot(), "lab-definitions"), ValidatorRequirements()).Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range catalog.Packages {
		if pkg.ID != euPackageID {
			continue
		}
		if pkg.Curriculum == nil || len(pkg.Curriculum.Roles) != 7 || len(pkg.Scenarios) != 7 {
			t.Fatal("incomplete EU curriculum")
		}
		for _, scenario := range pkg.Scenarios {
			if strings.Contains(scenario.Description, "Evidence prerequisite") {
				t.Errorf("%s kept an obsolete prerequisite", scenario.ID)
			}
			for _, step := range scenario.Steps {
				if !strings.Contains(step.Description, ":::hint ") {
					t.Errorf("%s/%s lacks teaching guidance", scenario.ID, step.ID)
				}
			}
			if strings.ContainsAny(scenario.Description, "*_`[]<>#") || strings.Contains(scenario.Description, ":::") {
				t.Errorf("%s has Markdown in plain mission", scenario.ID)
			}
		}
		for _, file := range []string{"eu-conduit-deep-dive.md", "eu-telecontrol-deep-dive.md"} {
			data, err := os.ReadFile(filepath.Join(repoRoot(), "lab-definitions", "packages", euPackageID, "knowledge", file))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Count(string(data), "#### ") != 8 {
				t.Errorf("%s lost its eight investigation questions", file)
			}
			if file == "eu-conduit-deep-dive.md" {
				for _, text := range []string{"**Full vendor-isolation check:**", "mgmt_net", "10.98.2.2", "10.98.3.2", "(#eu-zone-conduit-design)", "(#eu-iec62443-risk-design)", "(#eu-change-resource-plan)"} {
					if !strings.Contains(string(data), text) {
						t.Errorf("salvaged knowledge lost %q", text)
					}
				}
			}
		}
		return
	}
	t.Fatal("missing EU package")
}

// Refresh fixed fixtures to now, without relying on the real range or Docker.
func currentConduits(t *testing.T) conduitEvidence {
	t.Helper()
	ev := passingConduits(t)
	delta := time.Now().UTC().Sub(ev.collectedAt)
	shift := func(stamp *time.Time) { *stamp = stamp.Add(delta) }
	shift(&ev.collectedAt)
	shift(&ev.readStart)
	shift(&ev.readEnd)
	for i := range ev.state.Points {
		p := &ev.state.Points[i]
		shift(&p.ReceivedAt)
	}
	for i := range ev.journal.Entries {
		shift(&ev.journal.Entries[i].At)
	}
	for i := range ev.audit.Entries {
		shift(&ev.audit.Entries[i].At)
	}
	for i := range ev.history.Segments {
		for j := range ev.history.Segments[i].Samples {
			p := &ev.history.Segments[i].Samples[j]
			shift(&p.IngestedAt)
			shift(p.CCReceivedAt)
		}
	}
	for i := range ev.events {
		shift(&ev.events[i].Timestamp)
	}
	return ev
}

func TestConduitsCollector(t *testing.T) {
	for _, missing := range []string{"", "/cc/api/state", "/cc/api/journal", "/oltc/api/audit", "/oltc/api/state", "/hist/api/history", "/api/v1/events", "exec"} {
		t.Run("missing="+missing, func(t *testing.T) {
			ev := currentConduits(t)
			paths := map[string]any{"/cc/api/state": ev.state, "/cc/api/journal": ev.journal, "/oltc/api/state": ev.device, "/oltc/api/audit": ev.audit, "/hist/api/history": ev.history, "/api/v1/events": ev.events}
			seen := map[string]int{}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				seen[r.URL.Path]++
				if r.Method != "GET" {
					t.Errorf("validator mutated source: %s %s", r.Method, r.URL)
				}
				if r.URL.Path == missing {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				if r.URL.Path == "/hist/api/history" {
					q := r.URL.Query()
					if len(q) != 3 || q.Get("point") != "oltc-1-tap" || q.Get("since") == "" || q.Get("until") == "" {
						t.Errorf("wrong history window: %s", r.URL.RawQuery)
					}
				}
				data, ok := paths[r.URL.Path]
				if !ok {
					t.Errorf("unexpected endpoint: %s", r.URL)
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(data); err != nil {
					t.Error(err)
				}
			}))
			defer upstream.Close()
			gen := euGeneration(t, containd.NewClient(upstream.URL), map[string]string{"control_centre": upstream.URL + "/cc", "oltc_1": upstream.URL + "/oltc", "historian_eu": upstream.URL + "/hist"})
			s := rangeServer(gen, "custom") // Alternative reviewed policy designs are allowed.
			sources := map[string]bool{}
			s.execInContainer = func(_ context.Context, container string, cmd []string, budget int) (string, string, int, error) {
				if len(cmd) == 4 && cmd[2] == isolationHTTP {
					if missing == "exec" {
						return "", "", -1, errors.New("exec unavailable")
					}
					return passingIsolation(t, cmd, container), "", 0, nil
				}
				if container == "rangerdanger-iec104-rtu" {
					if len(cmd) != 7 || cmd[0] != "wget" || cmd[6] != "http://10.30.30.50:8080/api/state" {
						t.Fatalf("clock command=%q", cmd)
					}
					if missing == "exec" {
						return "", "", -1, errors.New("exec unavailable")
					}
					data, _ := json.Marshal(map[string]any{"sync_status": "locked", "reported_time": time.Now().UTC().Format(time.RFC3339Nano)})
					return string(data), "", 0, nil
				}
				if len(cmd) != 7 || cmd[0] != "python3" || cmd[2] != conduitHTTPGet || budget != 5 {
					t.Fatalf("exec command=%q budget=%d", cmd, budget)
				}
				sources[cmd[3]] = true
				if missing == "exec" {
					return "", "", -1, errors.New("exec unavailable")
				}
				var body string
				if cmd[4] == "10.70.70.40" {
					data, err := json.Marshal(ev.state.Points)
					if err != nil {
						t.Fatal(err)
					}
					body = string(data)
					if cmd[3] != "10.10.10.50" || container != "rangerdanger-kali" || cmd[6] != "/api/points" {
						t.Errorf("enterprise read arguments=%s %q", container, cmd)
					}
				} else {
					body = "<html>OpenPLC login</html>"
					if cmd[3] != "10.20.20.20" || cmd[4] != "10.30.30.30" || container != "rangerdanger-eng-ws" || cmd[6] != "/login" {
						t.Errorf("engineering read arguments=%s %q", container, cmd)
					}
				}
				data, _ := json.Marshal(map[string]any{"status": 200, "body": body})
				return string(data), "", 0, nil
			}
			checks := evaluateConduits(s.collectConduits(t.Context(), gen))
			failed := false
			for _, check := range checks {
				if check.Status != "pass" {
					failed = true
					if missing == "" {
						t.Errorf("%s: %s", check.Name, check.Detail)
					}
				}
			}
			if missing != "" && !failed {
				t.Fatal("missing evidence passed")
			}
			for path := range paths {
				if seen[path] != 1 {
					t.Errorf("%s read %d times, want1", path, seen[path])
				}
			}
			if len(sources) != 2 {
				t.Errorf("did not run both source-bound GETs: %v", sources)
			}
		})
	}
}

func TestConduitsReadErrors(t *testing.T) {
	for _, tt := range []struct {
		name, stdout string
		rc           int
		err          error
	}{
		{"missing executable", "", 127, nil},
		{"exec unavailable", "", -1, errors.New("unavailable")},
		{"malformed response", "not json", 0, nil},
		{"HTTP error", `{"status":403,"body":"denied"}`, 0, nil},
		{"empty body", `{"status":200,"body":""}`, 0, nil},
		{"wrong historian object", `{"status":200,"body":"{}"}`, 0, nil},
		{"empty historian inventory", `{"status":200,"body":"[]"}`, 0, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gen := euGeneration(t, nil, nil)
			s := rangeServer(gen, "improved")
			s.execInContainer = func(context.Context, string, []string, int) (string, string, int, error) {
				return tt.stdout, "stderr", tt.rc, tt.err
			}
			check := s.conduitRead(t.Context(), gen, "kali-1", "enterprise_net", "historian-1", "ops_dmz_net", "/api/points", "read")
			if check.Status != "fail" || !strings.HasPrefix(check.Detail, "missing evidence: ") {
				t.Fatalf("invalid GET passed: %+v", check)
			}
		})
	}
}

func TestConduitsClockRead(t *testing.T) {
	for _, tt := range []struct {
		name, status string
		age          time.Duration
		rc           int
		body         string
		wantPass     bool
	}{
		{"locked current", "locked", 0, 0, "", true},
		{"holdover", "holdover", 0, 0, "", false},
		{"freerun", "freerun", 0, 0, "", false},
		{"offset", "locked", time.Hour, 0, "", false},
		{"stale", "locked", -time.Minute, 0, "", false},
		{"missing tool", "locked", 0, 127, "", false},
		{"invalid JSON", "locked", 0, 0, "not json", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gen := euGeneration(t, nil, nil)
			s := rangeServer(gen, "improved")
			s.execInContainer = func(_ context.Context, container string, cmd []string, budget int) (string, string, int, error) {
				if container != "rangerdanger-iec104-rtu" || budget != 5 || len(cmd) != 7 || cmd[0] != "wget" || cmd[6] != "http://10.30.30.50:8080/api/state" {
					t.Fatalf("clock exec=%s %q budget%d", container, cmd, budget)
				}
				if tt.body != "" {
					return tt.body, "", tt.rc, nil
				}
				data, _ := json.Marshal(map[string]any{"sync_status": tt.status, "reported_time": time.Now().UTC().Add(tt.age).Format(time.RFC3339Nano)})
				return string(data), "stderr", tt.rc, nil
			}
			check := s.conduitClockRead(t.Context(), gen)
			if (check.Status == "pass") != tt.wantPass {
				t.Fatalf("clock check=%+v", check)
			}
		})
	}
}
