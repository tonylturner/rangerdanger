package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/gin-gonic/gin"
	"github.com/tturner/rangerdanger/backend/internal/db"
	"github.com/tturner/rangerdanger/backend/internal/models"
	"github.com/tturner/rangerdanger/backend/internal/orchestrator"
	"gorm.io/gorm"
)

type lifecycleDocker struct {
	*client.Client
	failed string
	calls  []string
}

func (f *lifecycleDocker) ContainerStart(_ context.Context, id string, _ container.StartOptions) error {
	return f.call("start", id)
}
func (f *lifecycleDocker) ContainerStop(_ context.Context, id string, _ container.StopOptions) error {
	return f.call("stop", id)
}
func (f *lifecycleDocker) ContainerRemove(_ context.Context, id string, _ container.RemoveOptions) error {
	return f.call("remove", id)
}
func (f *lifecycleDocker) ContainerInspect(_ context.Context, id string) (types.ContainerJSON, error) {
	f.calls = append(f.calls, "inspect:"+id)
	return types.ContainerJSON{NetworkSettings: &types.NetworkSettings{Networks: map[string]*network.EndpointSettings{"rangerdanger_field_net": {IPAddress: "10.40.40.20"}}}}, nil
}
func (f *lifecycleDocker) call(op, id string) error {
	f.calls = append(f.calls, op+":"+id)
	if op+":"+id == f.failed {
		return errors.New("injected")
	}
	return nil
}
func labServer(t *testing.T, fake *lifecycleDocker) (*Server, *gorm.DB) {
	t.Helper()
	database, err := db.Connect(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	var o *orchestrator.Orchestrator
	if fake == nil {
		o = orchestrator.NewWithDocker(nil, "")
	} else {
		o = orchestrator.NewWithDocker(fake, "")
	}
	return &Server{db: database, orchestrator: o, rng: servingRange(nil)}, database
}
func seedHandlerLab(t *testing.T, database *gorm.DB) {
	t.Helper()
	if err := database.Create(&models.LabTemplate{ID: "template", Topology: `{"nodes":[]}`}).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&models.LabInstance{ID: "lab", TemplateID: "template", Status: "stopped"}).Error; err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"alpha", "beta"} {
		if err := database.Create(&models.NodeDefinition{ID: id, LabInstanceID: "lab", ContainerID: id, ContainerName: id, Status: "stopped"}).Error; err != nil {
			t.Fatal(err)
		}
	}
}
func TestCreateLabInstanceAsync(t *testing.T) {
	for _, tc := range []struct{ topology, want string }{{`{"nodes":[]}`, "running"}, {"{", "error"}} {
		t.Run(tc.want, func(t *testing.T) {
			s, database := labServer(t, nil)
			if err := database.Create(&models.LabTemplate{ID: "template", Topology: tc.topology}).Error; err != nil {
				t.Fatal(err)
			}
			rec, body := invoke(s, s.handleCreateLabInstance, "POST", "/api/labs/instances", map[string]string{"template_id": "template", "name": "Demo"})
			if rec.Code != http.StatusAccepted || body["status"] != "creating" {
				t.Fatalf("response=%d %v", rec.Code, body)
			}
			id, ok := body["id"].(string)
			if !ok {
				t.Fatal(body)
			}
			deadline := time.Now().Add(2 * time.Second)
			for {
				var inst models.LabInstance
				if err := database.First(&inst, "id = ?", id).Error; err != nil {
					t.Fatal(err)
				}
				if inst.Status == tc.want {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("status=%q want %q", inst.Status, tc.want)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}
func TestLabLifecycleHandlers(t *testing.T) {
	for _, op := range []string{"start", "stop", "delete"} {
		for _, fail := range []bool{false, true} {
			t.Run(op+map[bool]string{false: " success", true: " failure"}[fail], func(t *testing.T) {
				fake := &lifecycleDocker{}
				if fail {
					key := op
					if op == "delete" {
						key = "remove"
					}
					fake.failed = key + ":alpha"
				}
				s, database := labServer(t, fake)
				seedHandlerLab(t, database)
				method, path := "POST", "/api/labs/instances/lab/"+op
				var handler func(*gin.Context)
				switch op {
				case "start":
					handler = s.handleStartLabInstance
				case "stop":
					handler = s.handleStopLabInstance
				case "delete":
					method = "DELETE"
					path = "/api/labs/instances/lab"
					handler = s.handleDeleteLabInstance
				}
				rec, body := invokeLab(handler, method, path)
				if fail {
					if rec.Code != 500 || !strings.Contains(body["error"].(string), "alpha") {
						t.Fatalf("response=%d %v", rec.Code, body)
					}
				} else if op == "delete" {
					if rec.Code != 204 {
						t.Fatalf("response=%d", rec.Code)
					}
				} else if rec.Code != 200 {
					t.Fatalf("response=%d %v", rec.Code, body)
				}
				var inst models.LabInstance
				err := database.First(&inst, "id = ?", "lab").Error
				if op == "delete" && !fail {
					if !errors.Is(err, gorm.ErrRecordNotFound) {
						t.Fatalf("row remains: %v", err)
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					if fail && inst.Status != "stopped" {
						t.Fatalf("status=%s", inst.Status)
					}
					if !fail && op == "start" && inst.Status != "running" {
						t.Fatalf("status=%s", inst.Status)
					}
				}
				if len(fake.calls) < 2 {
					t.Fatalf("calls=%v", fake.calls)
				}
			})
		}
	}
}

func invokeLab(handler gin.HandlerFunc, method, path string) (*httptest.ResponseRecorder, gin.H) {
	router := gin.New()
	route := "/api/labs/instances/:id"
	if method == "POST" {
		route += "/" + strings.TrimPrefix(path, "/api/labs/instances/lab/")
	}
	router.Handle(method, route, handler)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	var body gin.H
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
	}
	return rec, body
}
