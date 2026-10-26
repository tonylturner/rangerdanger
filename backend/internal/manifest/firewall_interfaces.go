package manifest

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/tturner/rangerdanger/backend/internal/labs"
)

// ManagementInterface is the containd interface of the RangerDanger control
// plane. labs.CheckManagementSafety keeps every imported policy's management
// access on it.
const ManagementInterface = "lan3"

// CheckFirewallInterfaces verifies that the firewall service pins the kernel
// name of every NIC (Compose interface_name) to its containd interface name:
// the management network is ManagementInterface, each teaching-zone network is
// its topology zone, no name repeats, and the names are exactly the
// interfaces every package policy declares. Docker's own ethN order differs
// between hosts, so student commands such as `tcpdump -i lan2` rely on it.
// policies maps each package policy name to its document.
func CheckFirewallInterfaces(m *Manifest, pkg labs.Package, policies map[string][]byte, normalized []byte) error {
	if m == nil {
		return fmt.Errorf("manifest is nil")
	}
	firewall, ok := m.ServiceByRole(RoleFirewall)
	if !ok {
		return fmt.Errorf("manifest has no firewall service")
	}
	model, err := decodeCompose(normalized)
	if err != nil {
		return fmt.Errorf("decode normalized Compose: %w", err)
	}
	service, ok := model.Services[firewall.Key]
	if !ok {
		return fmt.Errorf("Compose is missing firewall service %q", firewall.Key)
	}
	zones := make(map[string]string, len(pkg.Template.Networks))
	for _, network := range pkg.Template.Networks {
		zones[network.ID] = network.Zone
	}
	networks := make(map[string]Network, len(m.Networks))
	for _, network := range m.Networks {
		networks[network.Key] = network
	}

	owners := make(map[string]string, len(firewall.Interfaces))
	for _, iface := range firewall.Interfaces {
		attachment, ok := service.Networks[iface.Network]
		if !ok {
			return fmt.Errorf("firewall service %q is missing network %q", firewall.Key, iface.Network)
		}
		name := attachment.InterfaceName
		if name == "" {
			return fmt.Errorf("firewall service %q network %q has no interface_name", firewall.Key, iface.Network)
		}
		if owner, taken := owners[name]; taken {
			return fmt.Errorf("firewall networks %q and %q share interface_name %q", owner, iface.Network, name)
		}
		owners[name] = iface.Network
		want, err := firewallInterfaceName(networks[iface.Network], zones)
		if err != nil {
			return err
		}
		if name != want {
			return fmt.Errorf("firewall network %q interface_name is %q, want %q", iface.Network, name, want)
		}
	}

	pinned := make([]string, 0, len(owners))
	for name := range owners {
		pinned = append(pinned, name)
	}
	sort.Strings(pinned)
	policyNames := make([]string, 0, len(policies))
	for name := range policies {
		policyNames = append(policyNames, name)
	}
	sort.Strings(policyNames)
	for _, name := range policyNames {
		declared, err := policyInterfaceNames(policies[name])
		if err != nil {
			return fmt.Errorf("policy %s: %w", name, err)
		}
		if !slices.Equal(declared, pinned) {
			return fmt.Errorf("policy %s declares interfaces [%s], firewall interface_name values are [%s]",
				name, strings.Join(declared, " "), strings.Join(pinned, " "))
		}
	}
	return nil
}

// firewallInterfaceName is the containd interface a firewall attachment to
// network must carry.
func firewallInterfaceName(network Network, zones map[string]string) (string, error) {
	if network.Platform && !network.Delivery && network.Name == "rangerdanger_mgmt_net" {
		return ManagementInterface, nil
	}
	if network.Zone == "" {
		return "", fmt.Errorf("firewall attaches to network %q, which is neither management nor a teaching zone", network.Key)
	}
	zone := zones[network.Zone]
	if zone == "" {
		return "", fmt.Errorf("firewall network %q topology network %q has no containd zone", network.Key, network.Zone)
	}
	return zone, nil
}

// policyInterfaceNames returns the sorted interface names a containd policy
// declares.
func policyInterfaceNames(data []byte) ([]string, error) {
	var document struct {
		Interfaces []struct {
			Name string `json:"name"`
		} `json:"interfaces"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		return nil, fmt.Errorf("decode interfaces: %w", err)
	}
	names := make([]string, 0, len(document.Interfaces))
	for _, iface := range document.Interfaces {
		names = append(names, iface.Name)
	}
	sort.Strings(names)
	return names, nil
}
