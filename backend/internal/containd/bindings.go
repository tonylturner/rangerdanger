package containd

import (
	"encoding/json"
	"fmt"
)

// mergeApplianceBindings preserves appliance-owned physical NIC assignments.
// Policy authors own the logical interface set and all its other properties.
func mergeApplianceBindings(running, policy []byte) ([]byte, error) {
	var appliance *struct {
		Interfaces []struct {
			Name   string `json:"name"`
			Device string `json:"device"`
		} `json:"interfaces"`
	}
	if err := json.Unmarshal(running, &appliance); err != nil {
		return nil, fmt.Errorf("decode running config: %w", err)
	}
	if appliance == nil {
		return nil, fmt.Errorf("running config is null")
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(policy, &doc); err != nil {
		return nil, fmt.Errorf("decode policy: %w", err)
	}
	var interfaces []map[string]json.RawMessage
	if err := json.Unmarshal(doc["interfaces"], &interfaces); err != nil {
		return nil, fmt.Errorf("decode policy interfaces: %w", err)
	}
	devices := make(map[string]string, len(appliance.Interfaces))
	for _, iface := range appliance.Interfaces {
		devices[iface.Name] = iface.Device
	}
	for _, iface := range interfaces {
		var name string
		if err := json.Unmarshal(iface["name"], &name); err != nil {
			return nil, fmt.Errorf("decode interface name: %w", err)
		}
		delete(iface, "device")
		if device := devices[name]; device != "" {
			iface["device"], _ = json.Marshal(device)
		}
	}
	doc["interfaces"], _ = json.Marshal(interfaces)
	return json.Marshal(doc)
}
