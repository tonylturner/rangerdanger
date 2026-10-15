package server

// euPackageID is the package the EU recipe belongs to.
const euPackageID = "eu-iec104-substation"

// euRecipe is the EU IEC 104 telecontrol package. Its reset is the breaker's
// instructor reset; its canary is Kali's IEC 104 connect to the RTU. It has
// no validation report, traffic generator, or test suite. Its comparison
// covers only the cross-zone relationships in its package policies.
var euRecipe = &packageRecipe{
	resetInstructor: true,
	canary: canaryProbe{fromNode: "kali-1", label: "kali", toNode: "rtu-1", network: "station_net", port: 2404,
		allowUnder: "weak", denyUnder: "improved"},
	captureFilter: "tcp port 2404 or tcp port 502",
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
		},
		labels: map[string]string{
			"lan4": "Control Centre",
			"lan1": "Station",
			"lan2": "Field",
			"wan":  "Enterprise",
			"dmz":  "Vendor",
		},
	},
	policyLabels: map[string]string{
		"weak":     "weak baseline",
		"improved": "improved (control centre to RTU IEC 104, RTU to breaker Modbus only)",
	},
}
