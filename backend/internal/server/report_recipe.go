package server

// reportSpec keeps package-specific claims and artifact locations out of the
// shared TCP matrix renderer. PASS is deliberately not an evidence-pack grade.
type reportSpec struct {
	title, passText, captureIntro, captureLimits, limits string
	captureDir, hostCaptureDir                           string
	strictProbes                                         bool
}

var usReport = reportSpec{
	title:         "Substation Segmentation - Validation Report",
	passText:      "**Result: PASS** - every authorized flow works and every unauthorized flow is blocked. The active policy is enforcing what the design specified.",
	captureIntro:  "Sources whose traffic the firewall forwarded toward the field zone (`10.40.40.0/24`) during the capture window:",
	captureLimits: "Under a hardened policy only the RTAC (`10.30.30.20`) and the GPS/NTP server (`10.30.30.50`) should appear. Any other source IP indicates a missing rule or a leaky deny.",
	captureDir:    "/data/captures", hostCaptureDir: "data/firewall/captures",
}

var euReport = reportSpec{
	title:         "European Feeder Segmentation - TCP Reachability Report",
	passText:      "**Result: PASS** - every listed TCP reachability probe matched its expected verdict. This is not certification of protocol operations, command authorization, process continuity, PCAP completeness or the whole policy.",
	captureIntro:  "Sources observed forwarded toward EU field devices during the capture window (best-effort tcpdump summary):",
	captureLimits: "Expected operational sources include the RTU and PLC; same-subnet PLC-to-OLTC traffic bypasses the firewall. Absence here is not proof of denial. Retain and inspect the raw PCAP separately.",
	limits:        "- PASS covers only this TCP matrix. A refused connection proves reachability, not a functioning application. A timeout does not identify a decoded command or prove the firewall caused the loss. Missing tools are untested, not denied. PCAP is optional supporting evidence, not part of PASS; continuity and fresh observations require the scenario validator. No DPI or regulatory conformance is certified.\n",
	captureDir:    "/data/captures", hostCaptureDir: "data/firewall/captures",
	strictProbes: true,
}
