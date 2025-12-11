package orchestrator

import (
    "context"
    "log"

    "github.com/google/uuid"
    "gorm.io/gorm"

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

    nodes := []models.NodeDefinition{
        {
            ID:            uuid.NewString(),
            LabInstanceID: instance.ID,
            Type:          "ews",
            Name:          "Engineering Workstation",
            IP:            "10.10.10.10",
            Status:        "running",
        },
        {
            ID:            uuid.NewString(),
            LabInstanceID: instance.ID,
            Type:          "plc_trainer",
            Name:          "Process PLC",
            IP:            "10.30.30.10",
            Status:        "running",
        },
    }

    if err := db.WithContext(ctx).Create(&nodes).Error; err != nil {
        return err
    }

    instance.Status = "running"
    return db.WithContext(ctx).Save(instance).Error
}
