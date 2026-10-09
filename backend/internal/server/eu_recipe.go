package server

// euPackageID is the package the EU recipe belongs to.
const euPackageID = "eu-iec104-substation"

// euRecipe is the EU IEC 104 telecontrol package. Its reset is the breaker's
// instructor reset; its canary is Kali's IEC 104 connect to the RTU. It has
// no validation report, traffic generator, test suite, policy comparison
// or static rule fallback: those are built on the US topology.
var euRecipe = &packageRecipe{
	resetInstructor: true,
	canary: canaryProbe{fromNode: "kali-1", label: "kali", toNode: "rtu-1", network: "station_net", port: 2404,
		allowUnder: "weak", denyUnder: "improved"},
	captureFilter: "tcp port 2404 or tcp port 502",
	policyLabels: map[string]string{
		"weak":     "weak baseline",
		"improved": "improved (control centre to RTU IEC 104, RTU to breaker Modbus only)",
	},
}
