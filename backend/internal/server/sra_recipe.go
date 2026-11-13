package server

const sraPackageID = "us-sra-substation"

// SRA keeps the US RTAC and field devices, but not the engineering
// workstation or OpenPLC. Do not inherit US proofs, traffic or static
// policy summaries that describe those absent nodes or the US policies.
var sraRecipe = &packageRecipe{
	commandExecutor:   (*Server).executeRTACCommand,
	resetCommands:     resetDeviceCommands,
	resetRestartNodes: []string{"sra-broker-1", "sra-guacd-1"},
	canary:            canaryProbe{fromNode: "kali-1", label: "kali", toNode: "rtac-1", network: "ot_ops_net", port: 502},
	workshopName:      "US distribution substation: secure remote access",
}
