package manifest

import "fmt"

// These addresses are platform infrastructure, not containd zones. Keep the
// proxy and each desktop on distinct bridges so neither desktop is a transit
// path to another delivery network.
var deliveryDesktops = map[string]struct {
	name, subnet, gateway, proxyIP, desktopIP, service string
}{
	"desk_corp_net":   {"rangerdanger_desk_corp", "10.98.1.0/28", "10.98.1.1", "10.98.1.2", "10.98.1.3", "corp_ws"},
	"desk_vendor_net": {"rangerdanger_desk_vendor", "10.98.2.0/28", "10.98.2.1", "10.98.2.2", "10.98.2.3", "vendor_jump"},
	"desk_eng_net":    {"rangerdanger_desk_eng", "10.98.3.0/28", "10.98.3.1", "10.98.3.2", "10.98.3.3", "eng_workstation"},
}

func checkDeliveryNetwork(network Network) []string {
	want, reserved := deliveryDesktops[network.Key]
	if !network.Delivery {
		if reserved {
			return []string{fmt.Sprintf("network %q must be a delivery network", network.Key)}
		}
		return nil
	}
	var problems []string
	if !reserved || network.Name != want.name || network.Subnet != want.subnet || network.Gateway != want.gateway {
		problems = append(problems, fmt.Sprintf("delivery network %q must use its fixed platform name and IPAM", network.Key))
	}
	if !network.Platform || network.Zone != "" {
		problems = append(problems, fmt.Sprintf("delivery network %q must be platform-owned and have no zone", network.Key))
	}
	return problems
}
