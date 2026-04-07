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
	// Security Infrastructure
	{Type: "containd_ngfw", Name: "containd NGFW", Description: "ICS-aware next-generation firewall with DPI", DefaultNetworks: []string{"enterprise_net", "vendor_net", "ot_ops_net", "field_net"}, Image: "ghcr.io/tonylturner/containd:latest"},

	// Enterprise Zone
	{Type: "corp_workstation", Name: "Corporate Workstation", Description: "Enterprise desktop environment (noVNC)", DefaultNetworks: []string{"enterprise_net"}, Image: "linuxserver/webtop:ubuntu-mate"},
	{Type: "kali_pentest", Name: "Kali Attacker", Description: "Full Kali Linux for penetration testing", DefaultNetworks: []string{"enterprise_net"}, Image: "kalilinux/kali-rolling", Cmd: []string{"sleep", "infinity"}},

	// Vendor / Engineering Zone
	{Type: "vendor_jumpbox", Name: "Vendor Jump Box", Description: "Vendor/contractor remote access desktop", DefaultNetworks: []string{"vendor_net"}, Image: "linuxserver/webtop:ubuntu-xfce"},
	{Type: "eng_workstation", Name: "Engineering Workstation", Description: "Engineering maintenance desktop (noVNC)", DefaultNetworks: []string{"vendor_net"}, Image: "linuxserver/webtop:ubuntu-mate"},

	// OT Operations Zone
	{Type: "fuxa_hmi", Name: "Substation HMI", Description: "FUXA HMI for substation visualization and control", DefaultNetworks: []string{"ot_ops_net"}, Image: "frangoteam/fuxa:latest"},
	{Type: "rtac_sim", Name: "RTAC / Supervisory Controller", Description: "Supervisory controller and protocol broker", DefaultNetworks: []string{"ot_ops_net", "field_net"}, Image: "rangerdanger-rtac-sim"},
	{Type: "openplc", Name: "Substation Automation PLC", Description: "OpenPLC runtime for local automation logic", DefaultNetworks: []string{"ot_ops_net", "field_net"}, Image: "tuttas/openplc_v3:latest"},

	// Field Device Zone
	{Type: "relay_sim", Name: "Feeder Breaker / Relay", Description: "Substation feeder breaker and protective relay simulator", DefaultNetworks: []string{"field_net"}, Image: "rangerdanger-relay-sim"},
	{Type: "recloser_sim", Name: "Mid-Feeder Recloser", Description: "Automatic recloser with fault recovery logic", DefaultNetworks: []string{"field_net"}, Image: "rangerdanger-recloser-sim"},
	{Type: "regulator_sim", Name: "Voltage Regulator", Description: "Load tap changer for voltage regulation", DefaultNetworks: []string{"field_net"}, Image: "rangerdanger-regulator-sim"},
	{Type: "capbank_sim", Name: "Capacitor Bank", Description: "Switched capacitor bank for reactive power support", DefaultNetworks: []string{"field_net"}, Image: "rangerdanger-capbank-sim"},

	// OT Infrastructure
	{Type: "historian_sim", Name: "Data Historian", Description: "OT data historian collecting time-series SCADA data from RTAC", DefaultNetworks: []string{"ot_ops_net"}, Image: "rangerdanger-historian-sim"},
	{Type: "gps_sim", Name: "GPS Time Server", Description: "GPS-synchronized clock providing NTP/IRIG-B time to substation devices", DefaultNetworks: []string{"ot_ops_net"}, Image: "rangerdanger-gps-sim"},

	// Legacy types (oil-plant scenario)
	{Type: "ews", Name: "Engineering Workstation (Legacy)", Description: "Legacy type - use eng_workstation instead", DefaultNetworks: []string{"vendor_net"}, Image: "linuxserver/webtop:ubuntu-mate"},
	{Type: "ubuntu_jumpbox", Name: "Ubuntu Jump Box (Legacy)", Description: "Legacy type - use vendor_jumpbox instead", DefaultNetworks: []string{"vendor_net"}, Image: "linuxserver/webtop:ubuntu-xfce"},
	{Type: "hmi_view", Name: "HMI View (Legacy)", Description: "Legacy type - use fuxa_hmi instead", DefaultNetworks: []string{"ot_ops_net"}, Image: "frangoteam/fuxa:latest"},
	{Type: "hmi_control", Name: "HMI Control (Legacy)", Description: "Legacy type - use fuxa_hmi instead", DefaultNetworks: []string{"ot_ops_net"}, Image: "frangoteam/fuxa:latest"},
	{Type: "plc_trainer", Name: "Process PLC (Legacy)", Description: "Legacy type - use openplc instead", DefaultNetworks: []string{"ot_ops_net"}, Image: "tuttas/openplc_v3:latest"},
	{Type: "sis_plc", Name: "Safety PLC (Legacy)", Description: "Legacy type", DefaultNetworks: []string{"field_net"}, Image: "tuttas/openplc_v3:latest"},
	{Type: "historian", Name: "Historian", Description: "InfluxDB for timeseries capture", DefaultNetworks: []string{"ot_ops_net"}, Image: "influxdb:2"},
	{Type: "ot_ids", Name: "OT IDS", Description: "Suricata sensor with ICS signatures", DefaultNetworks: []string{"ot_ops_net"}, Image: "jasonish/suricata:latest", Cmd: []string{"suricata", "-c", "/etc/suricata/suricata.yaml", "-i", "eth0"}},
}
