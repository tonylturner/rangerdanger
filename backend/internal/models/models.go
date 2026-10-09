package models

import (
	"time"
)

// LabTemplate describes a reusable lab topology definition. ID is the
// topology's own id; PackageID names the curriculum package that owns it.
type LabTemplate struct {
	ID                 string    `gorm:"primaryKey" json:"id"`
	PackageID          string    `gorm:"index" json:"package_id"`
	Name               string    `json:"name"`
	Description        string    `json:"description"`
	Topology           string    `json:"topology"`
	ComposeFile        string    `json:"compose_file"`
	FirewallConfigPath string    `json:"firewall_config_path"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// LabInstance represents a running or stopped lab derived from a template.
type LabInstance struct {
	ID              string    `gorm:"primaryKey" json:"id"`
	TemplateID      string    `json:"template_id"`
	Name            string    `json:"name"`
	Status          string    `json:"status"`
	DockerStackName string    `json:"docker_stack_name"`
	RuntimeConfig   string    `json:"runtime_config"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`

	Template LabTemplate      `gorm:"foreignKey:TemplateID" json:"template,omitempty"`
	Nodes    []NodeDefinition `json:"nodes,omitempty"`
}

// NodeDefinition captures metadata for a node within a lab instance.
type NodeDefinition struct {
	ID            string    `gorm:"primaryKey" json:"id"`
	LabInstanceID string    `json:"lab_instance_id"`
	Type          string    `json:"type"`
	Name          string    `json:"name"`
	IP            string    `json:"ip"`
	Status        string    `json:"status"`
	Metadata      string    `json:"metadata"`
	ContainerID   string    `json:"container_id"`
	ContainerName string    `json:"container_name"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
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

// ScenarioRun tracks execution state for a scenario against a lab instance.
type ScenarioRun struct {
	ID            string    `gorm:"primaryKey" json:"id"`
	ScenarioID    string    `json:"scenario_id"`
	LabInstanceID string    `json:"lab_instance_id"`
	Status        string    `json:"status"`
	Events        string    `json:"events"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// TelemetryPoint stores metrics for visualization.
type TelemetryPoint struct {
	ID            string    `gorm:"primaryKey" json:"id"`
	LabInstanceID string    `json:"lab_instance_id"`
	SourceNodeID  string    `json:"source_node_id"`
	Metric        string    `json:"metric"`
	Value         string    `json:"value"`
	Timestamp     time.Time `json:"timestamp"`
}
