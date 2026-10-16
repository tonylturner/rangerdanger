package server

// euPackageID is the package the EU recipe belongs to.
const euPackageID = "eu-iec104-substation"

// euRecipe is the EU IEC 104 telecontrol package. Its reset restores process
// and device truth; its canary is Kali's IEC 104 connect to the RTU. Its comparison
// covers only the cross-zone relationships in its package policies.
var euRecipe = &packageRecipe{
	simDevices:        map[string]string{"breaker": "breaker-1", "rmu-1": "rmu-1", "rmu-2": "rmu-2", "oltc-1": "oltc-1", "gps": "gps-1"},
	commandExecutor:   (*Server).executeSimCommand,
	sessionCuts:       map[string]sessionCutSpec{"cc-rtu": {sourceNetwork: "control_centre_net", targetNetwork: "station_net", transport: "tcp"}},
	resetTruth:        resetEUTruth,
	check:             (*Server).executeEUCheck,
	report:            euReport,
	validation:        euValidationMatrix(),
	validationCapture: captureSpec{nodes: []string{"breaker-1", "rmu-1", "rmu-2", "oltc-1"}, network: "field_net"},
	traffic: []trafficTarget{
		{"eng-ws-1", "plc-1", "station_net", "curl -sf --max-time 3 http://%s:8080/login > /dev/null", "eng-ws→PLC maintenance GET"},
		{"kali-1", "historian-1", "ops_dmz_net", "curl -sf --max-time 3 http://%s:8080/api/points > /dev/null", "enterprise→historian read"},
		{"vendor-jump-1", "historian-1", "ops_dmz_net", "curl -sf --max-time 3 http://%s:8080/api/points > /dev/null", "vendor→historian read (weak)"},
	},
	canary: canaryProbe{fromNode: "kali-1", label: "kali", toNode: "rtu-1", network: "station_net", port: 2404,
		allowUnder: "weak", denyUnder: "improved"},
	captureFilter: "tcp port 2404 or tcp port 502 or tcp port 8080 or tcp port 8081",
	compare: &compareSpec{
		weakPolicy:     "weak",
		improvedPolicy: "improved",
		pairs: []zonePair{
			{"lan4", "lan1"}, // Control Centre → Station
			{"lan1", "lan2"}, // Station → Field
			{"wan", "lan1"},  // Enterprise → Station
			{"dmz", "lan1"},  // Vendor → Station
			{"wan", "lan2"},  // Enterprise → Field
			{"dmz", "lan2"},  // Vendor → Field
			{"lan4", "lan2"}, // Control Centre → Field
			{"lan2", "lan1"}, // Field → Station
			{"lan4", "lan5"}, // Control Centre → Operations DMZ
			{"wan", "lan5"},  // Enterprise → Operations DMZ
		},
		labels: map[string]string{
			"lan4": "Control Centre",
			"lan5": "Operations DMZ",
			"lan1": "Station",
			"lan2": "Field",
			"wan":  "Enterprise",
			"dmz":  "Vendor",
		},
	},
	policyLabels: map[string]string{
		"weak":     "weak baseline",
		"improved": "improved (source-pinned IEC 104 and Modbus control, PLC maintenance and historian conduits)",
	},
}
