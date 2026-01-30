package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"gorm.io/gorm"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/models"
)

// Orchestrator manages Docker containers for lab instances.
type Orchestrator struct {
	logger       *log.Logger
	dockerClient *client.Client
}

// New creates a new orchestrator with Docker SDK client.
func New() *Orchestrator {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Printf("WARNING: Docker client init failed: %v (orchestrator will use stub mode)", err)
		return &Orchestrator{logger: log.Default(), dockerClient: nil}
	}
	return &Orchestrator{logger: log.Default(), dockerClient: cli}
}

// ProvisionLabInstance creates containers for lab nodes.
func (o *Orchestrator) ProvisionLabInstance(ctx context.Context, db *gorm.DB, instance *models.LabInstance) error {
	o.logger.Printf("[lab %s] provisioning", instance.ID)

	var template models.LabTemplate
	if err := db.First(&template, "id = ?", instance.TemplateID).Error; err != nil {
		return err
	}

	var topo struct {
		Nodes []labs.NodeYAML `json:"nodes"`
	}
	_ = json.Unmarshal([]byte(template.Topology), &topo)

	// UI proxy configuration for known node types
	uiMap := map[string]map[string]any{
		"containd_ngfw":   {"host": "192.168.240.2", "port": 8080},
		"ews":             {"host": "ews", "port": 3000},
		"plc_trainer":     {"host": "192.168.242.20", "port": 8080},
		"sis_plc":         {"host": "sis_plc", "port": 8080},
		"hmi_scada":       {"host": "192.168.241.10", "port": 1881},
		"grafana":         {"host": "grafana", "port": 3000},
		"ot_ids":          {"host": "ids", "port": 80},
		"jump_host":       {"host": "jump_host", "port": 6080},
		"historian":       {"host": "historian", "port": 80},
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

		node := models.NodeDefinition{
			ID:            n.ID,
			LabInstanceID: instance.ID,
			Type:          n.Type,
			Name:          n.Name,
			Status:        "pending",
			Metadata:      string(metaJSON),
		}

		// Skip container creation for containd_ngfw (always running as infrastructure)
		if n.Type == "containd_ngfw" {
			node.Status = "running"
			node.ContainerName = "rangerdanger-firewall"
			nodes = append(nodes, node)
			continue
		}

		// For other node types, use Docker SDK if available
		if o.dockerClient != nil {
			containerID, containerName, err := o.createContainer(ctx, n, instance.ID)
			if err != nil {
				o.logger.Printf("[lab %s] container creation failed for %s: %v", instance.ID, n.ID, err)
				node.Status = "error"
			} else {
				node.ContainerID = containerID
				node.ContainerName = containerName
				node.Status = "created"
			}
		} else {
			// Stub mode - just mark as running
			node.Status = "running"
		}

		nodes = append(nodes, node)
	}

	if len(nodes) > 0 {
		if err := db.WithContext(ctx).Create(&nodes).Error; err != nil {
			return err
		}
	}

	instance.Status = "running"
	return db.WithContext(ctx).Save(instance).Error
}

// createContainer creates a Docker container for a node.
func (o *Orchestrator) createContainer(ctx context.Context, node labs.NodeYAML, labID string) (string, string, error) {
	// Find the node template in catalog
	var nodeTemplate *labs.NodeTemplate
	for _, t := range labs.NodeCatalog {
		if t.Type == node.Type {
			nodeTemplate = &t
			break
		}
	}
	if nodeTemplate == nil {
		return "", "", fmt.Errorf("unknown node type: %s", node.Type)
	}

	containerName := fmt.Sprintf("rangerdanger-%s-%s", labID[:8], node.ID)

	// Create container config
	config := &container.Config{
		Image: nodeTemplate.Image,
		Labels: map[string]string{
			"rangerdanger.lab_id": labID,
			"rangerdanger.node_id": node.ID,
			"rangerdanger.node_type": node.Type,
		},
	}

	hostConfig := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
	}

	// Create container
	resp, err := o.dockerClient.ContainerCreate(ctx, config, hostConfig, nil, nil, containerName)
	if err != nil {
		return "", "", fmt.Errorf("create container: %w", err)
	}

	return resp.ID, containerName, nil
}

// StartContainer starts a container by ID.
func (o *Orchestrator) StartContainer(ctx context.Context, containerID string) error {
	if o.dockerClient == nil {
		return nil
	}
	return o.dockerClient.ContainerStart(ctx, containerID, container.StartOptions{})
}

// StopContainer stops a container by ID.
func (o *Orchestrator) StopContainer(ctx context.Context, containerID string) error {
	if o.dockerClient == nil {
		return nil
	}
	timeout := 10
	return o.dockerClient.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout})
}

// RemoveContainer removes a container by ID.
func (o *Orchestrator) RemoveContainer(ctx context.Context, containerID string) error {
	if o.dockerClient == nil {
		return nil
	}
	return o.dockerClient.ContainerRemove(ctx, containerID, container.RemoveOptions{Force: true})
}

// ExecShell executes an interactive shell in a container.
func (o *Orchestrator) ExecShell(ctx context.Context, containerID string) (types.HijackedResponse, error) {
	if o.dockerClient == nil {
		return types.HijackedResponse{}, fmt.Errorf("docker client not available")
	}

	execConfig := container.ExecOptions{
		Cmd:          []string{"/bin/sh"},
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
	}

	execID, err := o.dockerClient.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return types.HijackedResponse{}, fmt.Errorf("exec create: %w", err)
	}

	return o.dockerClient.ContainerExecAttach(ctx, execID.ID, container.ExecStartOptions{Tty: true})
}

// GetContainerLogs returns logs from a container.
func (o *Orchestrator) GetContainerLogs(ctx context.Context, containerID string, tail string) (string, error) {
	if o.dockerClient == nil {
		return "", fmt.Errorf("docker client not available")
	}

	options := container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tail,
	}

	reader, err := o.dockerClient.ContainerLogs(ctx, containerID, options)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	logs, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	return string(logs), nil
}

// StartLabContainers starts all containers for a lab instance.
func (o *Orchestrator) StartLabContainers(ctx context.Context, db *gorm.DB, instanceID string) error {
	var nodes []models.NodeDefinition
	if err := db.Where("lab_instance_id = ?", instanceID).Find(&nodes).Error; err != nil {
		return err
	}

	for _, node := range nodes {
		if node.ContainerID == "" || node.Type == "containd_ngfw" {
			continue
		}
		if err := o.StartContainer(ctx, node.ContainerID); err != nil {
			o.logger.Printf("[lab %s] failed to start container %s: %v", instanceID, node.ID, err)
		} else {
			db.Model(&node).Update("status", "running")
		}
	}

	return nil
}

// StopLabContainers stops all containers for a lab instance.
func (o *Orchestrator) StopLabContainers(ctx context.Context, db *gorm.DB, instanceID string) error {
	var nodes []models.NodeDefinition
	if err := db.Where("lab_instance_id = ?", instanceID).Find(&nodes).Error; err != nil {
		return err
	}

	for _, node := range nodes {
		if node.ContainerID == "" || node.Type == "containd_ngfw" {
			continue
		}
		if err := o.StopContainer(ctx, node.ContainerID); err != nil {
			o.logger.Printf("[lab %s] failed to stop container %s: %v", instanceID, node.ID, err)
		} else {
			db.Model(&node).Update("status", "stopped")
		}
	}

	return nil
}

// RemoveLabContainers removes all containers for a lab instance.
func (o *Orchestrator) RemoveLabContainers(ctx context.Context, db *gorm.DB, instanceID string) error {
	var nodes []models.NodeDefinition
	if err := db.Where("lab_instance_id = ?", instanceID).Find(&nodes).Error; err != nil {
		return err
	}

	for _, node := range nodes {
		if node.ContainerID == "" || node.Type == "containd_ngfw" {
			continue
		}
		if err := o.RemoveContainer(ctx, node.ContainerID); err != nil {
			o.logger.Printf("[lab %s] failed to remove container %s: %v", instanceID, node.ID, err)
		}
	}

	return nil
}
