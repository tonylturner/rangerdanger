package orchestrator

import (
	"context"
	"log"
	"strings"
	"time"

	dtypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
)

// GatewayMapping defines the containd gateway IP for a Docker network.
type GatewayMapping struct {
	DockerNetwork string // Docker network name (e.g., "rangerdanger_enterprise_net")
	GatewayIP     string // containd IP on that network (e.g., "10.10.10.2")
}

// DefaultGatewayMappings returns the static gateway mappings derived from docker-compose.yml.
// Each zone network should route through containd for DPI and policy enforcement.
func DefaultGatewayMappings() []GatewayMapping {
	return []GatewayMapping{
		{DockerNetwork: "enterprise_net", GatewayIP: "10.10.10.2"},
		{DockerNetwork: "vendor_net", GatewayIP: "10.20.20.2"},
		{DockerNetwork: "ot_ops_net", GatewayIP: "10.30.30.2"},
		{DockerNetwork: "field_net", GatewayIP: "10.40.40.2"},
		// physics_net is intentionally NOT routed through containd
	}
}

// containerGateway defines what gateway a specific container should use.
type containerGateway struct {
	ContainerName string
	GatewayIP     string
}

// ProvisionGateways sets the default gateway on all lab containers to route through containd.
// This ensures cross-zone traffic flows through the firewall for DPI and policy enforcement.
// Should be called after all containers are running.
func (o *Orchestrator) ProvisionGateways(ctx context.Context) {
	if o.dockerClient == nil {
		log.Println("gateway provisioner: Docker client not available, skipping")
		return
	}

	// Wait for containers to be running
	log.Println("gateway provisioner: waiting for containers to start...")
	time.Sleep(10 * time.Second)

	mappings := DefaultGatewayMappings()

	// Build a lookup: network suffix → gateway IP
	gwByNetwork := map[string]string{}
	for _, m := range mappings {
		gwByNetwork[m.DockerNetwork] = m.GatewayIP
	}

	// List all running containers
	containers, err := o.dockerClient.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		log.Printf("gateway provisioner: failed to list containers: %v", err)
		return
	}

	// For each container, determine the correct gateway based on its network membership
	var targets []containerGateway
	for _, c := range containers {
		name := strings.TrimPrefix(c.Names[0], "/")

		// Skip infrastructure containers that shouldn't route through containd
		if isInfraContainer(name) {
			continue
		}

		// Find the primary zone network and its gateway
		gw := pickGateway(c, gwByNetwork)
		if gw == "" {
			continue
		}

		targets = append(targets, containerGateway{ContainerName: name, GatewayIP: gw})
	}

	// Apply gateway to each container
	provisioned := 0
	for _, t := range targets {
		if err := o.setContainerGateway(ctx, t.ContainerName, t.GatewayIP); err != nil {
			log.Printf("gateway provisioner: %s: %v", t.ContainerName, err)
		} else {
			provisioned++
		}
	}

	log.Printf("gateway provisioner: configured %d/%d containers to route through containd", provisioned, len(targets))
}

// isInfraContainer returns true for containers that should NOT have their gateway changed.
func isInfraContainer(name string) bool {
	// The firewall IS the gateway — don't change its own routing
	if strings.Contains(name, "firewall") {
		return true
	}
	// The backend uses distroless (no ip/route tools) and needs mgmt_net routing
	if strings.Contains(name, "backend") {
		return true
	}
	return false
}

// pickGateway determines the correct containd gateway IP for a container.
// For multi-homed containers, we pick the gateway for the "primary" zone network.
// Priority: field > ot_ops > vendor > enterprise (most specific zone wins)
func pickGateway(c dtypes.Container, gwByNetwork map[string]string) string {
	if c.NetworkSettings == nil {
		return ""
	}

	// Priority order for gateway selection
	priority := []string{"field_net", "ot_ops_net", "vendor_net", "enterprise_net"}

	for _, suffix := range priority {
		for netName := range c.NetworkSettings.Networks {
			if strings.HasSuffix(netName, suffix) {
				if gw, ok := gwByNetwork[suffix]; ok {
					return gw
				}
			}
		}
	}

	return ""
}

// setContainerGateway sets the default gateway on a container via docker exec.
// It tries ip, then route, then installs iproute2 if needed (Debian/Ubuntu containers).
func (o *Orchestrator) setContainerGateway(ctx context.Context, containerName, gatewayIP string) error {
	// Script that tries available tools, installs iproute2 if needed
	script := `
if command -v ip >/dev/null 2>&1; then
    ip route del default 2>/dev/null
    ip route add default via ` + gatewayIP + ` 2>/dev/null
elif command -v route >/dev/null 2>&1; then
    route del default 2>/dev/null
    route add default gw ` + gatewayIP + ` 2>/dev/null
elif command -v apt-get >/dev/null 2>&1; then
    apt-get update -qq >/dev/null 2>&1 && apt-get install -y -qq iproute2 >/dev/null 2>&1
    ip route del default 2>/dev/null
    ip route add default via ` + gatewayIP + ` 2>/dev/null
elif command -v apk >/dev/null 2>&1; then
    apk add --no-cache iproute2 >/dev/null 2>&1
    ip route del default 2>/dev/null
    ip route add default via ` + gatewayIP + ` 2>/dev/null
else
    echo "WARNING: no routing command available" >&2
fi
`
	cmd := []string{"sh", "-c", script}

	execConfig := container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: false,
		AttachStderr: false,
		Privileged:   true,
	}

	execResp, err := o.dockerClient.ContainerExecCreate(ctx, containerName, execConfig)
	if err != nil {
		return err
	}

	return o.dockerClient.ContainerExecStart(ctx, execResp.ID, container.ExecStartOptions{})
}
