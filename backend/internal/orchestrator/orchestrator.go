package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"gorm.io/gorm"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/models"
)

// Network name mapping from lab definition to Docker network names.
// Supports both the new substation scenario and legacy oil-plant names.
var networkNameMap = map[string]string{
	// Substation segmentation lab
	"enterprise_net": "rangerdanger_enterprise_net",
	"vendor_net":     "rangerdanger_vendor_net",
	"ot_ops_net":     "rangerdanger_ot_ops_net",
	"field_net":      "rangerdanger_field_net",
	"physics_net":    "rangerdanger_physics_net",
	// Legacy oil-plant names (backward compat)
	"it_net":         "rangerdanger_it_net",
	"dmz_net":        "rangerdanger_dmz_net",
	"ot_control_net": "rangerdanger_ot_control_net",
	"ot_safety_net":  "rangerdanger_ot_safety_net",
}

// Orchestrator manages Docker containers for lab instances.
type Orchestrator struct {
	logger         *log.Logger
	dockerClient   *client.Client
	containdClient *containd.Client
	labDefsDir     string
}

// New creates a new orchestrator with Docker SDK client.
func New(containdClient *containd.Client, labDefsDir string) *Orchestrator {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.Printf("WARNING: Docker client init failed: %v (orchestrator will use stub mode)", err)
		return &Orchestrator{logger: log.Default(), dockerClient: nil, containdClient: containdClient, labDefsDir: labDefsDir}
	}
	return &Orchestrator{logger: log.Default(), dockerClient: cli, containdClient: containdClient, labDefsDir: labDefsDir}
}

// DockerClient returns the underlying Docker client for direct API access.
// Returns nil if Docker is not available (stub mode).
func (o *Orchestrator) DockerClient() *client.Client {
	return o.dockerClient
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

	// UI port configuration for known node types (host will be set from container IP)
	uiPortMap := map[string]int{
		"containd_ngfw":    8080,
		"fuxa_hmi":         1881,
		"rtac_sim":         8080,
		"relay_sim":        8080,
		"recloser_sim":     8080,
		"regulator_sim":    8080,
		"capbank_sim":      8080,
		"historian_sim":    8080,
		"gps_sim":          8080,
		"opendss_sim":      8080,
		"openplc":          8080,
		"corp_workstation": 3000,
		"vendor_jumpbox":   3000,
		"eng_workstation":  3000,
		"kali_pentest":     3000,
		// Legacy types
		"ews":           3000,
		"ubuntu_jumpbox": 3000,
		"plc_trainer":   8080,
		"sis_plc":       8080,
		"hmi_view":      1881,
		"hmi_control":   1881,
		"hmi_scada":     1881,
		"historian":     8086,
		"ot_ids":        9999,
	}

	var nodes []models.NodeDefinition
	for _, n := range topo.Nodes {
		node := models.NodeDefinition{
			ID:            n.ID,
			LabInstanceID: instance.ID,
			Type:          n.Type,
			Name:          n.Name,
			Status:        "pending",
		}

		// Skip container creation for containd_ngfw (always running as infrastructure)
		if n.Type == "containd_ngfw" {
			node.Status = "running"
			node.ContainerName = "rangerdanger-firewall"
			node.IP = "10.10.10.2" // IT network interface IP (primary)

			// Add all interface IPs to metadata for multi-homed firewall
			firewallMeta := map[string]any{
				"networks": n.Networks,
				"interface_ips": map[string]string{
					"it_net":         "10.10.10.2",
					"dmz_net":        "10.20.20.2",
					"ot_control_net": "10.30.30.2",
					"ot_safety_net":  "10.40.40.2",
				},
				"ui_path":         "/containd/",             // Proxied access via nginx (same-origin, auth works)
				"external_ui_url": "http://localhost:9080", // Direct access for "open in new tab"
				"ui":              map[string]any{"host": "10.10.10.2", "port": 8080},
			}
			firewallMetaJSON, _ := json.Marshal(firewallMeta)
			node.Metadata = string(firewallMetaJSON)

			nodes = append(nodes, node)
			continue
		}

		// For other node types, use Docker SDK if available
		if o.dockerClient != nil {
			containerID, containerName, err := o.createContainer(ctx, n, instance.ID)
			if err != nil {
				o.logger.Printf("[lab %s] container creation failed for %s: %v", instance.ID, n.ID, err)
				node.Status = "error"
				// Set metadata without UI since container failed
				meta := map[string]any{"networks": n.Networks}
				metaJSON, _ := json.Marshal(meta)
				node.Metadata = string(metaJSON)
			} else {
				node.ContainerID = containerID
				node.ContainerName = containerName
				// Start the container immediately after creation
				if err := o.StartContainer(ctx, containerID); err != nil {
					o.logger.Printf("[lab %s] container start failed for %s: %v", instance.ID, n.ID, err)
					node.Status = "error"
				} else {
					node.Status = "running"
					node.IP = o.getContainerIP(ctx, containerID)
				}

				// Build metadata with actual container IP for UI proxy
				meta := map[string]any{"networks": n.Networks}
				if port, ok := uiPortMap[n.Type]; ok && port > 0 && node.IP != "" {
					meta["ui"] = map[string]any{"host": node.IP, "port": port}
				}
				metaJSON, _ := json.Marshal(meta)
				node.Metadata = string(metaJSON)
			}
		} else {
			// Stub mode - just mark as running
			node.Status = "running"
			meta := map[string]any{"networks": n.Networks}
			metaJSON, _ := json.Marshal(meta)
			node.Metadata = string(metaJSON)
		}

		nodes = append(nodes, node)
	}

	// Save nodes using upsert to handle re-provisioning
	for _, node := range nodes {
		if err := db.WithContext(ctx).Save(&node).Error; err != nil {
			o.logger.Printf("[lab %s] failed to save node %s: %v", instance.ID, node.ID, err)
		}
	}

	// Push lab-specific firewall config to containd
	if template.FirewallConfigPath != "" && o.containdClient != nil {
		cfgPath := filepath.Join(o.labDefsDir, template.FirewallConfigPath)
		data, err := os.ReadFile(cfgPath)
		if err != nil {
			o.logger.Printf("[lab %s] failed to read firewall config %s: %v", instance.ID, cfgPath, err)
		} else if err := o.containdClient.ImportConfig(data); err != nil {
			o.logger.Printf("[lab %s] failed to import firewall config: %v", instance.ID, err)
		} else {
			o.logger.Printf("[lab %s] firewall config imported from %s", instance.ID, cfgPath)
		}
	}

	instance.Status = "running"
	return db.WithContext(ctx).Save(instance).Error
}

// createContainer creates a Docker container for a node and connects it to proper networks.
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
			"rangerdanger.lab_id":    labID,
			"rangerdanger.node_id":   node.ID,
			"rangerdanger.node_type": node.Type,
		},
	}

	// Override command if specified in catalog
	if len(nodeTemplate.Cmd) > 0 {
		config.Cmd = nodeTemplate.Cmd
	}

	hostConfig := &container.HostConfig{
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
	}

	// Determine the primary network for initial container creation
	var networkConfig *network.NetworkingConfig
	if len(node.Networks) > 0 {
		primaryNetwork := node.Networks[0]
		if dockerNet, ok := networkNameMap[primaryNetwork]; ok {
			networkConfig = &network.NetworkingConfig{
				EndpointsConfig: map[string]*network.EndpointSettings{
					dockerNet: {},
				},
			}
		}
	}

	// Create container
	resp, err := o.dockerClient.ContainerCreate(ctx, config, hostConfig, networkConfig, nil, containerName)
	if err != nil {
		return "", "", fmt.Errorf("create container: %w", err)
	}

	// Connect to additional networks
	for i := 1; i < len(node.Networks); i++ {
		if dockerNet, ok := networkNameMap[node.Networks[i]]; ok {
			err := o.dockerClient.NetworkConnect(ctx, dockerNet, resp.ID, nil)
			if err != nil {
				o.logger.Printf("[container %s] failed to connect to network %s: %v", containerName, dockerNet, err)
			}
		}
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

// ExecCommand runs a command non-interactively in a container and returns stdout/stderr.
func (o *Orchestrator) ExecCommand(ctx context.Context, containerName string, cmd []string, timeoutSec int) (string, string, int, error) {
	if o.dockerClient == nil {
		return "", "", -1, fmt.Errorf("docker client not available")
	}

	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	if timeoutSec > 60 {
		timeoutSec = 60
	}

	execCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSec)*time.Second)
	defer cancel()

	execConfig := container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          false,
	}

	execID, err := o.dockerClient.ContainerExecCreate(execCtx, containerName, execConfig)
	if err != nil {
		return "", "", -1, fmt.Errorf("exec create: %w", err)
	}

	resp, err := o.dockerClient.ContainerExecAttach(execCtx, execID.ID, container.ExecStartOptions{Tty: false})
	if err != nil {
		return "", "", -1, fmt.Errorf("exec attach: %w", err)
	}
	defer resp.Close()

	// Read multiplexed stdout/stderr (non-TTY mode uses Docker stream multiplexing)
	output, err := io.ReadAll(resp.Reader)
	if err != nil && execCtx.Err() == nil {
		return "", "", -1, fmt.Errorf("read output: %w", err)
	}

	// Get exit code
	inspect, err := o.dockerClient.ContainerExecInspect(ctx, execID.ID)
	exitCode := -1
	if err == nil {
		exitCode = inspect.ExitCode
	}

	// For non-TTY, Docker multiplexes stdout/stderr with 8-byte headers.
	// The stdcopy package handles this, but for simplicity we return raw output.
	// Most tools write to stdout anyway.
	return string(output), "", exitCode, nil
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

// StartLabContainers starts all containers for a lab instance and updates their IPs.
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
			// Get container IP address
			ip := o.getContainerIP(ctx, node.ContainerID)
			db.Model(&node).Updates(map[string]interface{}{
				"status": "running",
				"ip":     ip,
			})
		}
	}

	return nil
}

// getContainerIP retrieves the IP address of a container from its first network.
func (o *Orchestrator) getContainerIP(ctx context.Context, containerID string) string {
	if o.dockerClient == nil {
		return ""
	}

	inspect, err := o.dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		return ""
	}

	// Return IP from first available network (prefer zone networks over bridge)
	for netName, netSettings := range inspect.NetworkSettings.Networks {
		if strings.HasPrefix(netName, "rangerdanger_") && netSettings.IPAddress != "" {
			return netSettings.IPAddress
		}
	}

	// Fallback to any available IP
	for _, netSettings := range inspect.NetworkSettings.Networks {
		if netSettings.IPAddress != "" {
			return netSettings.IPAddress
		}
	}

	return ""
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
