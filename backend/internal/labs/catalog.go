package labs

// NodeTemplate documents supported OT components for orchestration and UI palettes.
type NodeTemplate struct {
	Type            string   `json:"type"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	DefaultNetworks []string `json:"default_networks"`
	Image           string   `json:"image"`
	Cmd             []string `json:"cmd,omitempty"` // Override default container command
}

// NodeCatalog enumerates built-in node templates aligned with the frontend palette.
var NodeCatalog = []NodeTemplate{
	{Type: "containd_ngfw", Name: "containd NGFW", Description: "ICS-aware next-generation firewall with Modbus DPI", DefaultNetworks: []string{"it_net", "dmz_net", "ot_control_net", "ot_safety_net"}, Image: "ghcr.io/tturner/containd:latest"},

	// IT Network
	{Type: "kali_pentest", Name: "Kali Pentest Box", Description: "Full Kali Linux for penetration testing", DefaultNetworks: []string{"it_net"}, Image: "kalilinux/kali-rolling", Cmd: []string{"sleep", "infinity"}},

	// DMZ
	{Type: "ews", Name: "Engineering Workstation", Description: "Ubuntu-based desktop with noVNC for engineering tasks", DefaultNetworks: []string{"dmz_net"}, Image: "linuxserver/webtop:ubuntu-mate"},
	{Type: "ubuntu_jumpbox", Name: "Ubuntu Jump Box", Description: "Ubuntu desktop with VPN clients - sole gateway to OT Control", DefaultNetworks: []string{"dmz_net"}, Image: "linuxserver/webtop:ubuntu-xfce"},
	{Type: "hmi_view", Name: "HMI View (Read-Only)", Description: "FUXA HMI for monitoring only - firewall enforces read-only access", DefaultNetworks: []string{"dmz_net", "ot_control_net", "ot_safety_net"}, Image: "frangoteam/fuxa:latest"},
	{Type: "historian", Name: "Historian", Description: "InfluxDB or Timescale for timeseries capture", DefaultNetworks: []string{"dmz_net"}, Image: "influxdb:2"},
	{Type: "ot_ids", Name: "OT IDS", Description: "Suricata sensor with ICS signatures", DefaultNetworks: []string{"dmz_net"}, Image: "jasonish/suricata:latest", Cmd: []string{"suricata", "-c", "/etc/suricata/suricata.yaml", "-i", "eth0"}},

	// OT Control
	{Type: "hmi_control", Name: "HMI Control (Full Access)", Description: "FUXA HMI with full read/write control access", DefaultNetworks: []string{"ot_control_net"}, Image: "frangoteam/fuxa:latest"},
	{Type: "plc_trainer", Name: "Process PLC", Description: "OpenPLC runtime for primary process", DefaultNetworks: []string{"ot_control_net"}, Image: "tuttas/openplc_v3:latest"},

	// OT Safety
	{Type: "sis_plc", Name: "Safety PLC", Description: "Safety instrumented system logic (read-only access enforced)", DefaultNetworks: []string{"ot_safety_net"}, Image: "tuttas/openplc_v3:latest"},

	// Legacy/External
	{Type: "opnsense_external", Name: "OPNsense Firewall", Description: "External VM tracked by metadata only", DefaultNetworks: []string{"it_net", "dmz_net", "ot_control_net", "ot_safety_net"}, Image: "external"},
	{Type: "jump_host", Name: "Pentest Jump Host (Legacy)", Description: "Legacy type - use kali_pentest or ubuntu_jumpbox instead", DefaultNetworks: []string{"it_net"}, Image: "kalilinux/kali-rolling", Cmd: []string{"sleep", "infinity"}},
	{Type: "hmi_scada", Name: "HMI / SCADA (Legacy)", Description: "Legacy type - use hmi_view or hmi_control instead", DefaultNetworks: []string{"dmz_net", "ot_control_net"}, Image: "frangoteam/fuxa:latest"},
}
