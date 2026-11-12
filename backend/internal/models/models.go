package models

import (
	"time"
)

// Scenario defines a training scenario. IDs are plain slugs, unique within
// the consuming PackageID; LabTemplateID names its topology.
type Scenario struct {
	PackageID        string    `gorm:"primaryKey;not null" json:"package_id"`
	ID               string    `gorm:"primaryKey;not null" json:"id"`
	Name             string    `json:"name"`
	Summary          string    `json:"summary"`
	Description      string    `json:"description"`
	Order            string    `json:"order"`
	LabTemplateID    string    `json:"lab_template_id"`
	Tags             string    `json:"tags"`
	Steps            string    `json:"steps"`
	Nodes            string    `json:"nodes"`
	EstimatedMinutes int       `json:"estimated_minutes"`
	Validator        string    `json:"validator"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
