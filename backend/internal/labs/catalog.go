package labs

// NodeTemplate documents supported OT components for orchestration and UI palettes.
type NodeTemplate struct {
	Type            string   `json:"type"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	DefaultNetworks []string `json:"default_networks"`
	Image           string   `json:"image"`
}

// NodeCatalog enumerates built-in node templates aligned with the frontend palette.
var NodeCatalog = []NodeTemplate{
	{Type: "ews", Name: "Engineering Workstation", Description: "Ubuntu-based desktop with noVNC and OpenPLC IDE", DefaultNetworks: []string{"it_net"}, Image: "ghcr.io/linuxserver/webtop:ubuntu-mate"},
	{Type: "jump_host", Name: "Pentest Jump Host", Description: "Kali-lite tooling for trainees", DefaultNetworks: []string{"it_net"}, Image: "kalilinux/kali-rolling"},
	{Type: "plc_trainer", Name: "Process PLC", Description: "OpenPLC runtime for primary process", DefaultNetworks: []string{"ot_control_net"}, Image: "openplc/openplc:latest"},
	{Type: "sis_plc", Name: "Safety PLC", Description: "Safety instrumented system logic", DefaultNetworks: []string{"ot_safety_net"}, Image: "openplc/openplc:latest"},
	{Type: "hmi_scada", Name: "HMI / SCADA", Description: "FUXA HMI web server", DefaultNetworks: []string{"dmz_net", "ot_control_net"}, Image: "ghcr.io/frangoteam/fuxa:latest"},
	{Type: "historian", Name: "Historian", Description: "InfluxDB or Timescale for timeseries capture", DefaultNetworks: []string{"dmz_net"}, Image: "influxdb:2"},
	{Type: "ot_ids", Name: "OT IDS", Description: "Suricata sensor with ICS signatures", DefaultNetworks: []string{"dmz_net"}, Image: "jasonish/suricata:latest"},
	{Type: "opnsense_external", Name: "OPNsense Firewall", Description: "External VM tracked by metadata only", DefaultNetworks: []string{"it_net", "dmz_net", "ot_control_net", "ot_safety_net"}, Image: "external"},
}
