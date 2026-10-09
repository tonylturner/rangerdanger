package models

import (
	"time"
)

// LabTemplate stores a package's topology for the workshop graph. ID is
// the topology's own id; PackageID names the curriculum package that owns
// it.
type LabTemplate struct {
	ID          string    `gorm:"primaryKey" json:"id"`
	PackageID   string    `gorm:"index" json:"package_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Topology    string    `json:"topology"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Scenario defines a training scenario. IDs are plain slugs, unique across
// every package; PackageID names the owning package.
type Scenario struct {
	ID               string    `gorm:"primaryKey" json:"id"`
	PackageID        string    `gorm:"index" json:"package_id"`
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
