package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
	"github.com/tturner/rangerdanger/backend/internal/orchestrator"
)

type resetRange struct {
	*fakeRange
	restart func(context.Context, *lifecycle.Generation, []string) error
}

func (r *resetRange) RestartServices(ctx context.Context, gen *lifecycle.Generation, services []string) error {
	return r.restart(ctx, gen, services)
}

// The retained device fixture is intentionally independent of lane A's
// package. Renamed service keys prove resolution is not a container lookup.
func sraResetFixture(client *containd.Client) *lifecycle.Generation {
	base := usGeneration(client)
	base.Package.ID = sraPackageID
	base.Manifest.Services = append(base.Manifest.Services,
		manifest.Service{Key: "fixture_broker", Node: "sra-broker-1", Container: "not-the-service-key"},
		manifest.Service{Key: "fixture_gateway", Node: "sra-guacd-1", Container: "also-not-the-key"})
	return lifecycle.NewGeneration(2, base.Package, base.Manifest, client, time.Now())
}

func TestSRARecipeSupportedOperations(t *testing.T) {
	recipe, err := recipeFor(sraResetFixture(nil))
	if err != nil || recipe != sraRecipe {
		t.Fatalf("recipe = %v %v", recipe, err)
	}
	if !reflect.DeepEqual(recipe.resetCommands, resetDeviceCommands) || recipe.commandExecutor == nil {
		t.Fatal("kept US devices must have their real RTAC command transport")
	}
	if recipe.check != nil || recipe.compare != nil || recipe.staticRules != nil || recipe.traffic != nil || recipe.testSuite {
		t.Fatal("SRA inherited unsupported US workshop operations")
	}
	if recipe.canary != usRecipe.canary {
		t.Fatal("kept RTAC canary changed")
	}
}

func TestSRARestartResolutionFailsBeforeMutation(t *testing.T) {
	gen := sraResetFixture(nil)
	called := false
	runner := &resetRange{restart: func(context.Context, *lifecycle.Generation, []string) error {
		called = true
		return nil
	}}
	err := restartResetNodes(t.Context(), runner, gen, []string{"sra-broker-1", "absent"})
	if err == nil || called {
		t.Fatalf("partial restart: called=%v err=%v", called, err)
	}
	if err := restartResetNodes(t.Context(), struct{}{}, gen, sraRecipe.resetRestartNodes); err == nil {
		t.Fatal("missing lifecycle restart accepted")
	}
}

func TestSRAWorkshopReset(t *testing.T) {
	for _, failure := range []string{"", "policy", "device", "restart"} {
		t.Run(failure, func(t *testing.T) {
			status := http.StatusOK
			if failure == "policy" {
				status = http.StatusPreconditionFailed
			}
			fw, counters := fakeContaind(t, status, http.StatusOK)
			var commands atomic.Int32
			rtac := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if atomic.LoadInt32(&counters.commitCount) != 1 {
					t.Error("device reset before weak policy commit")
				}
				commands.Add(1)
				if failure == "device" {
					w.WriteHeader(http.StatusBadGateway)
				}
				fmt.Fprint(w, `{"result":"executed","detail":"device reset"}`)
			}))
			defer rtac.Close()
			gen := sraResetFixture(containd.NewClient(fw.URL))
			for i := range gen.Manifest.Services {
				if gen.Manifest.Services[i].Key == "rtac_sim" {
					gen.Manifest.Services[i].Endpoints["api"] = rtac.URL
				}
			}
			s := rangeServer(gen, "improved")
			s.orchestrator = &orchestrator.Orchestrator{}
			restarts := 0
			s.rng = &resetRange{fakeRange: s.rng.(*fakeRange), restart: func(ctx context.Context, got *lifecycle.Generation, services []string) error {
				restarts++
				if got != gen || !reflect.DeepEqual(services, []string{"fixture_broker", "fixture_gateway"}) || commands.Load() != int32(len(resetDeviceCommands)) {
					t.Errorf("restart before device reset or wrong identities: %v commands=%d", services, commands.Load())
				}
				if failure == "restart" {
					return errors.New("compose restart failed")
				}
				return nil
			}}
			rec := callRoute(s, s.handleWorkshopReset, "POST", "/api/workshop/reset", "/api/workshop/reset", "")
			var body struct {
				Success  bool                   `json:"success"`
				Actions  []resetAction          `json:"actions"`
				Containd *containd.ApplyReceipt `json:"containd"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			wantStatus := http.StatusOK
			if failure != "" {
				wantStatus = http.StatusBadGateway
			}
			if failure == "policy" {
				wantStatus = http.StatusConflict
				if commands.Load() != 0 || restarts != 0 {
					t.Fatal("mutated devices/sessions after policy conflict")
				}
			} else if restarts != 1 || len(body.Actions) != len(resetDeviceCommands)+2 || body.Containd == nil || body.Containd.Protection != "protecting" {
				t.Fatalf("incomplete reset: %s", rec.Body)
			}
			if rec.Code != wantStatus || body.Success != (failure == "") {
				t.Fatalf("reset = %d %s", rec.Code, rec.Body)
			}
		})
	}
}
