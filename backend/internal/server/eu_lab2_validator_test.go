package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
)

func TestConduitsRegistration(t *testing.T) {
	want := []string{labs.CapabilityPolicyContaind, labs.CapabilityProtocolIEC104, labs.CapabilityProcessTruth}
	if !slices.Equal(ValidatorRequirements()["eu-station-conduits"], want) || scenarioValidators["eu-station-conduits"].run == nil {
		t.Fatal("missing conduit registration or wrong capability contract")
	}
}

func TestConduitsShippedCurriculum(t *testing.T) {
	catalog, err := labs.NewLoader(filepath.Join(repoRoot(), "lab-definitions"), ValidatorRequirements()).Load()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{
		"iec104-telecontrol-boundary": {"observe-baseline", "operate-breaker", "expose-weak", "apply-improved", "cut-session", "local-trip", "clear-fault", "restore", "evidence", "restore-device"},
		"eu-station-conduits":         {"inventory-and-findings", "capture-operational-flows", "probe-three-footholds", "decide-data-and-time-conduits", "budget-the-change", "implement-and-rehearse-rollback", "start-proof-window", "prove-plc-and-historian", "prove-denies-and-maintenance", "compare-plan-and-outcome"},
	}
	found := map[string]bool{}
	for _, pkg := range catalog.Packages {
		for _, scenario := range pkg.Scenarios {
			ids, ok := want[scenario.ID]
			if !ok {
				continue
			}
			found[scenario.ID] = true
			var got []string
			decisions, hints := 0, 0
			for _, step := range scenario.Steps {
				got = append(got, step.ID)
				decisions += strings.Count(step.Description, ":::decision ")
				hints += strings.Count(step.Description, ":::hint ")
			}
			if !reflect.DeepEqual(got, ids) {
				t.Errorf("%s steps=%v want%v", scenario.ID, got, ids)
			}
			if hints < 8 {
				t.Errorf("%s hints=%d want at least8", scenario.ID, hints)
			}
			if decisions != 8 {
				t.Errorf("%s decisions=%d want8", scenario.ID, decisions)
			}
		}
	}
	if len(found) != 2 {
		t.Fatalf("shipped scenarios=%v", found)
	}
}

func TestEULab2To4DescriptionsPlainText(t *testing.T) {
	catalog, err := labs.NewLoader(filepath.Join(repoRoot(), "lab-definitions"), ValidatorRequirements()).Load()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"eu-station-conduits":   false,
		"eu-rmu-switching":      false,
		"eu-oltc-plc-historian": false,
	}
	for _, pkg := range catalog.Packages {
		for _, scenario := range pkg.Scenarios {
			if _, ok := want[scenario.ID]; !ok {
				continue
			}
			want[scenario.ID] = true
			t.Run(scenario.ID, func(t *testing.T) {
				// The runner's header is a plain paragraph; only steps render Markdown.
				if strings.ContainsAny(scenario.Description, "*_`[]<>#") || strings.Contains(scenario.Description, ":::") {
					t.Error("scenario description contains Markdown in a plain-text surface")
				}
				if scenario.ID != "eu-station-conduits" {
					return
				}
				if words := len(strings.Fields(scenario.Description)); words < 80 || words > 110 {
					t.Errorf("mission description has %d words, want 80–110", words)
				}
				if strings.Contains(scenario.Description, "Evidence prerequisite") {
					t.Error("obsolete evidence prerequisite remains in the mission")
				}
				first := scenario.Steps[0].Description
				for _, text := range []string{
					"**Acceptance limit:**", "mgmt_net", "unrestricted acceptance sign-off",
					"(#eu-zone-conduit-design)", "(#eu-iec62443-risk-design)", "(#eu-change-resource-plan)",
				} {
					if !strings.Contains(first, text) {
						t.Errorf("step 1 lost %q", text)
					}
				}
			})
		}
	}
	for id, found := range want {
		if !found {
			t.Errorf("missing scenario %s", id)
		}
	}
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
			checks := scenarioValidators["eu-station-conduits"].run(t.Context(), s, validatorInput{gen: gen, activeConfig: "custom"})
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
