package orchestrator

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/docker/errdefs"
	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/db"
	"github.com/tturner/rangerdanger/backend/internal/models"
	"gorm.io/gorm"
)

func labDB(t *testing.T) *gorm.DB {
	t.Helper()
	database, err := db.Connect(filepath.Join(t.TempDir(), "x.db"))
	if err != nil {
		t.Fatal(err)
	}
	return database
}
func seedLab(t *testing.T, database *gorm.DB) *models.LabInstance {
	t.Helper()
	template := models.LabTemplate{ID: "template", Topology: `{"nodes":[{"id":"alpha","name":"Alpha","type":"relay_sim","networks":["field_net","ot_ops_net"]},{"id":"beta","name":"Beta","type":"relay_sim","networks":["field_net"]},{"id":"firewall","name":"Firewall","type":"containd_ngfw","networks":["field_net"]}]}`}
	if err := database.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	instance := &models.LabInstance{ID: "short", TemplateID: template.ID, Status: "creating"}
	if err := database.Create(instance).Error; err != nil {
		t.Fatal(err)
	}
	return instance
}
func node(t *testing.T, database *gorm.DB, id string) models.NodeDefinition {
	t.Helper()
	var n models.NodeDefinition
	if err := database.First(&n, "id = ?", id).Error; err != nil {
		t.Fatal(err)
	}
	return n
}
func TestProvisionLabInstance(t *testing.T) {
	for _, tc := range []struct {
		name   string
		failed string
		want   string
		stub   bool
	}{
		{"happy", "", "running", false}, {"create failure", "create:rangerdanger-short-alpha", "error", false},
		{"network failure", "connect:rangerdanger-short-alpha@rangerdanger_ot_ops_net", "error", false},
		{"stub", "", "running", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := labDB(t)
			instance := seedLab(t, database)
			fake := &scriptedDocker{fails: map[string]error{tc.failed: errors.New("injected")}}
			o := &Orchestrator{logger: log.New(io.Discard, "", 0), dockerClient: fake}
			if tc.stub {
				o.dockerClient = nil
			}
			err := o.ProvisionLabInstance(context.Background(), database, instance)
			if tc.want == "error" && (err == nil || !strings.Contains(err.Error(), "alpha")) {
				t.Fatalf("error = %v", err)
			}
			if tc.want == "running" && err != nil {
				t.Fatal(err)
			}
			if instance.Status != tc.want {
				t.Fatalf("instance status = %q", instance.Status)
			}
			alpha, beta, firewall := node(t, database, "alpha"), node(t, database, "beta"), node(t, database, "firewall")
			if beta.Status != "running" || firewall.Status != "running" || firewall.ContainerName != "rangerdanger-firewall" {
				t.Fatalf("nodes = %+v %+v", beta, firewall)
			}
			if tc.stub {
				if len(fake.calls) != 0 || alpha.ContainerID != "" || alpha.Status != "running" {
					t.Fatalf("stub = %+v calls=%v", alpha, fake.calls)
				}
				return
			}
			if alpha.Status != tc.want || beta.IP != "10.40.40.20" {
				t.Fatalf("nodes = %+v %+v", alpha, beta)
			}
			if tc.failed == "" && (alpha.ContainerID != "rangerdanger-short-alpha" || !strings.Contains(strings.Join(fake.calls, ","), "connect:rangerdanger-short-alpha@rangerdanger_ot_ops_net")) {
				t.Fatalf("calls = %v, node=%+v", fake.calls, alpha)
			}
		})
	}
}
func TestProvisionBadTopology(t *testing.T) {
	database := labDB(t)
	inst := seedLab(t, database)
	if err := database.Model(&models.LabTemplate{}).Where("id = ?", inst.TemplateID).Update("topology", "{").Error; err != nil {
		t.Fatal(err)
	}
	o := &Orchestrator{logger: log.New(io.Discard, "", 0)}
	if err := o.ProvisionLabInstance(context.Background(), database, inst); err == nil || inst.Status != "error" {
		t.Fatalf("err=%v status=%s", err, inst.Status)
	}
}
func TestLabLifecycle(t *testing.T) {
	for _, op := range []string{"start", "stop", "remove"} {
		for _, fail := range []bool{false, true} {
			t.Run(op+map[bool]string{true: " fail", false: " success"}[fail], func(t *testing.T) {
				database := labDB(t)
				inst := seedLab(t, database)
				for _, id := range []string{"alpha", "beta"} {
					if err := database.Create(&models.NodeDefinition{ID: id, LabInstanceID: inst.ID, ContainerID: id, ContainerName: id, IP: "old", Status: "stopped"}).Error; err != nil {
						t.Fatal(err)
					}
				}
				fake := &scriptedDocker{fails: map[string]error{}}
				if fail {
					fake.fails[op+":alpha"] = errors.New("injected")
				}
				o := &Orchestrator{logger: log.New(io.Discard, "", 0), dockerClient: fake}
				var err error
				switch op {
				case "start":
					err = o.StartLabContainers(context.Background(), database, inst.ID)
				case "stop":
					err = o.StopLabContainers(context.Background(), database, inst.ID)
				case "remove":
					err = o.RemoveLabContainers(context.Background(), database, inst.ID)
				}
				if fail && (err == nil || !strings.Contains(err.Error(), "alpha")) {
					t.Fatalf("err=%v", err)
				}
				if !fail && err != nil {
					t.Fatal(err)
				}
				b := node(t, database, "beta")
				switch op {
				case "start":
					if b.Status != "running" || b.IP != "10.40.40.20" {
						t.Fatal(b)
					}
				case "stop":
					if b.Status != "stopped" {
						t.Fatal(b)
					}
				case "remove":
					if b.Status != "removed" || b.ContainerID != "" || b.ContainerName != "" || b.IP != "" {
						t.Fatal(b)
					}
				}
				if fail {
					a := node(t, database, "alpha")
					if a.Status != "stopped" || a.ContainerID == "" {
						t.Fatal(a)
					}
				}
				if len(fake.calls) < 2 {
					t.Fatalf("calls=%v", fake.calls)
				}
				if op == "remove" && fail {
					delete(fake.fails, "remove:alpha")
					if err := o.RemoveLabContainers(context.Background(), database, inst.ID); err != nil {
						t.Fatal(err)
					}
					if strings.Count(strings.Join(fake.calls, ","), "remove:beta") != 1 {
						t.Fatalf("retry re-removed beta: %v", fake.calls)
					}
				}
			})
		}
	}
}
func TestRemoveMissingContainer(t *testing.T) {
	database := labDB(t)
	inst := seedLab(t, database)
	if err := database.Create(&models.NodeDefinition{ID: "alpha", LabInstanceID: inst.ID, ContainerID: "alpha", ContainerName: "alpha", IP: "old", Status: "stopped"}).Error; err != nil {
		t.Fatal(err)
	}
	fake := &scriptedDocker{fails: map[string]error{"remove:alpha": errdefs.NotFound(errors.New("No such container: alpha"))}}
	o := &Orchestrator{logger: log.New(io.Discard, "", 0), dockerClient: fake}
	if err := o.RemoveLabContainers(context.Background(), database, inst.ID); err != nil {
		t.Fatalf("missing container must count as removed: %v", err)
	}
	if a := node(t, database, "alpha"); a.Status != "removed" || a.ContainerID != "" || a.ContainerName != "" || a.IP != "" {
		t.Fatal(a)
	}
}

func TestExecCommand(t *testing.T) {
	for _, code := range []int{0, 7} {
		t.Run(string(rune('0'+code)), func(t *testing.T) {
			fake := &scriptedDocker{fails: map[string]error{}, exitCode: code}
			o := &Orchestrator{logger: log.New(io.Discard, "", 0), dockerClient: fake}
			out, stderr, got, err := o.ExecCommand(context.Background(), "alpha", []string{"echo", "hello"}, 1)
			if err != nil || out != "hello" || stderr != "warning" || got != code {
				t.Fatalf("got %q %q %d %v", out, stderr, got, err)
			}
		})
	}
}
func TestProvisionFirewallImportFailure(t *testing.T) {
	database := labDB(t)
	inst := seedLab(t, database)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "policy.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := database.Model(&models.LabTemplate{}).Where("id = ?", inst.TemplateID).Update("firewall_config_path", "policy.json").Error; err != nil {
		t.Fatal(err)
	}
	o := &Orchestrator{logger: log.New(io.Discard, "", 0), labDefsDir: dir, containdClient: containd.NewClient("http://127.0.0.1:1")}
	err := o.ProvisionLabInstance(context.Background(), database, inst)
	if err == nil || !strings.Contains(err.Error(), "import firewall config") || inst.Status != "error" {
		t.Fatalf("err=%v status=%s", err, inst.Status)
	}
	if node(t, database, "beta").Status != "running" {
		t.Fatal("nodes not saved")
	}
}

func TestProvisionNodeSaveFailure(t *testing.T) {
	database := labDB(t)
	inst := seedLab(t, database)
	if err := database.Callback().Create().Before("gorm:create").Register("inject node save failure", func(tx *gorm.DB) {
		if tx.Statement.Schema != nil && tx.Statement.Schema.Table == "node_definitions" {
			tx.AddError(errors.New("injected save"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	o := &Orchestrator{logger: log.New(io.Discard, "", 0)}
	err := o.ProvisionLabInstance(context.Background(), database, inst)
	if err == nil || !strings.Contains(err.Error(), "node alpha save") || inst.Status != "error" {
		t.Fatalf("err=%v status=%s", err, inst.Status)
	}
}

func TestLifecycleDBUpdateFailure(t *testing.T) {
	for _, op := range []string{"start", "stop", "remove"} {
		t.Run(op, func(t *testing.T) {
			database := labDB(t)
			inst := seedLab(t, database)
			for _, id := range []string{"alpha", "beta"} {
				if err := database.Create(&models.NodeDefinition{ID: id, LabInstanceID: inst.ID, ContainerID: id, Status: "stopped"}).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := database.Callback().Update().Before("gorm:update").Register("inject node update failure", func(tx *gorm.DB) {
				tx.AddError(errors.New("injected update"))
			}); err != nil {
				t.Fatal(err)
			}
			fake := &scriptedDocker{fails: map[string]error{}}
			o := &Orchestrator{logger: log.New(io.Discard, "", 0), dockerClient: fake}
			var err error
			switch op {
			case "start":
				err = o.StartLabContainers(context.Background(), database, inst.ID)
			case "stop":
				err = o.StopLabContainers(context.Background(), database, inst.ID)
			case "remove":
				err = o.RemoveLabContainers(context.Background(), database, inst.ID)
			}
			if err == nil || !strings.Contains(err.Error(), "node alpha update") || !strings.Contains(err.Error(), "node beta update") {
				t.Fatalf("expected per-node DB errors, got %v", err)
			}
		})
	}
}
