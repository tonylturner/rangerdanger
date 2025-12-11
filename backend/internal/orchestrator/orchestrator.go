package orchestrator

import (
	"context"
	"encoding/json"
	"log"

	"gorm.io/gorm"

	"github.com/tturner/rangerrocks/backend/internal/labs"
	"github.com/tturner/rangerrocks/backend/internal/models"
)

// Orchestrator is a high-level stub that would eventually drive Docker/Compose.
type Orchestrator struct {
	logger *log.Logger
}

// New creates a new orchestrator stub.
func New() *Orchestrator {
	return &Orchestrator{logger: log.Default()}
}

// ProvisionLabInstance simulates container orchestration.
func (o *Orchestrator) ProvisionLabInstance(ctx context.Context, db *gorm.DB, instance *models.LabInstance) error {
	o.logger.Printf("[lab %s] provisioning stub", instance.ID)

	var template models.LabTemplate
	if err := db.First(&template, "id = ?", instance.TemplateID).Error; err != nil {
		return err
	}

	var topo struct {
		Nodes []labs.NodeYAML `json:"nodes"`
	}
	_ = json.Unmarshal([]byte(template.Topology), &topo)

	uiMap := map[string]map[string]any{
		"ews":               {"host": "ews", "port": 3000},
		"plc_trainer":       {"host": "plc_trainer", "port": 8080},
		"sis_plc":           {"host": "sis_plc", "port": 8080},
		"hmi_scada":         {"host": "hmi_scada", "port": 1881},
		"grafana":           {"host": "grafana", "port": 3000},
		"ot_ids":            {"host": "ids", "port": 80},
		"jump_host":         {"host": "jump_host", "port": 6080},
		"historian":         {"host": "historian", "port": 80},
		"opnsense_external": {"host": "opnsense", "port": 443},
	}

	var nodes []models.NodeDefinition
	for _, n := range topo.Nodes {
		meta := map[string]any{
			"networks": n.Networks,
		}
		if cfg, ok := uiMap[n.Type]; ok {
			meta["ui"] = cfg
		}
		metaJSON, _ := json.Marshal(meta)
		nodes = append(nodes, models.NodeDefinition{
			ID:            n.ID,
			LabInstanceID: instance.ID,
			Type:          n.Type,
			Name:          n.Name,
			Status:        "running",
			Metadata:      string(metaJSON),
		})
	}

	if len(nodes) > 0 {
		if err := db.WithContext(ctx).Create(&nodes).Error; err != nil {
			return err
		}
	}

	instance.Status = "running"
	return db.WithContext(ctx).Save(instance).Error
}
