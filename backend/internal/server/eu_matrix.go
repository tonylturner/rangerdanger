package server

// The six policy intents expand to nine positive endpoints: RTU access to
// every field device matters independently. Negatives likewise cover all four
// field devices, not just one representative of a subnet-wide rule.
func euValidationMatrix() []validationProbeSpec {
	rows := []validationProbeSpec{
		{"cc-1", "control-centre", "rtu-1", "station_net", "2404", "allow", "CC IEC 104 to RTU", "authorized"},
		{"plc-1", "PLC", "oltc-1", "field_net", "502", "allow", "PLC to OLTC (same subnet; not firewall enforcement)", "authorized"},
		{"cc-1", "control-centre", "historian-1", "ops_dmz_net", "8081", "allow", "CC historian ingest", "authorized"},
		{"kali-1", "enterprise", "historian-1", "ops_dmz_net", "8080", "allow", "Enterprise historian read", "authorized"},
		{"eng-ws-1", "engineering", "plc-1", "station_net", "8080", "allow", "Named engineering PLC maintenance", "authorized"},
		{"kali-1", "kali", "rtu-1", "station_net", "2404", "deny", "Foreign IEC 104 connect", "unauthorized"},
		{"kali-1", "kali", "plc-1", "station_net", "8080", "deny", "Enterprise PLC web", "unauthorized"},
		{"kali-1", "kali", "vendor-jump-1", "vendor_net", "3389", "deny", "Enterprise vendor RDP", "unauthorized"},
		{"kali-1", "kali", "vendor-jump-1", "vendor_net", "5900", "deny", "Enterprise vendor VNC", "unauthorized"},
	}
	for _, device := range []string{"breaker-1", "rmu-1", "rmu-2", "oltc-1"} {
		rows = append(rows, validationProbeSpec{"rtu-1", "RTU", device, "field_net", "502", "allow", "RTU field Modbus", "authorized"})
		for _, source := range []string{"kali-1", "vendor-jump-1", "eng-ws-1"} {
			rows = append(rows, validationProbeSpec{source, source, device, "field_net", "502", "deny", "Foreign field Modbus connect", "unauthorized"})
		}
	}
	return rows
}
