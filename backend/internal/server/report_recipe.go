package server

// reportSpec keeps package-specific claims and artifact locations out of the
// shared TCP matrix renderer. PASS is deliberately not an evidence-pack grade.
type reportSpec struct {
	title, passText, captureIntro, captureLimits, limits string
	skippedText, methodology, unauthorizedIntro          string
	captureDir, hostCaptureDir                           string
}

var usReport = reportSpec{
	unauthorizedIntro: "Hardened expects these TCP connects to time out. Refusal is reached (allow); tool and exec errors are untested. A timeout does not identify its cause.",
	methodology: "- **allow** = TCP connect or explicit refusal (reached). **deny** = only a connect timeout; tool, signal and exec errors are untested, never denied.\n" +
		"- Strict connect-only probes use Bash `/dev/tcp` or BusyBox `nc -e /bin/true`; no command payload is sent.\n" +
		"- This report validates the **currently active** policy; it does not apply or change any policy.\n",

	skippedText:   "**Result: FAIL** - %d probe(s) could not run (source container missing, renamed, or stopped - shown as ⊘ below), so the validation matrix did not fully execute. A report cannot claim PASS unless every probe ran. Bring the full lab stack up and re-run. Inspect any ✗ rows for genuine policy mismatches as well.",
	title:         "Substation Segmentation - Validation Report",
	passText:      "**Result: PASS** - every listed TCP tuple matches Hardened, with stable policy identity and capture evidence. Reachability is not application health or firewall-causation proof.",
	captureIntro:  "Sources whose traffic the firewall forwarded toward the field zone (`10.40.40.0/24`) during the capture window:",
	captureLimits: "Hardened permits the RTAC (`10.30.30.20`) and GPS/NTP server (`10.30.30.50`). A partial plan may retain other sources. Absence here is not proof of denial; inspect the raw capture separately.",
	captureDir:    "/data/captures", hostCaptureDir: "data/firewall/captures",
}

var euReport = reportSpec{
	unauthorizedIntro: "Forbidden TCP connects must time out. A refused connection is reached (allow), so an unauthorized row fails even without a destination listener. Timeouts do not establish why a connection failed or certify firewall enforcement.",
	methodology: "- **allow** = TCP connect succeeded or the destination refused the connection (reached, but no listener). Containd drops rather than rejects, so refusal is not denial. **deny** = only a connect timeout (3 s); a timeout does not identify its cause.\n" +
		"- Strict connect-only probes originate inside the real source containers using Bash `/dev/tcp` or BusyBox `nc -e /bin/true`; no command payload is sent. Tool/container errors are untested (⊘), never denial.\n" +
		"- At most four probes run concurrently, each exec is bounded to 5 s, and the matrix is bounded to 30 s. Only authorized flows retry once.\n" +
		"- This report tests the currently active policy without applying or changing it.\n",

	skippedText:   "**Result: FAIL** - %d probe(s) were untested (⊘): a source container, probe tool or evidence collection failed. Inspect the probe errors and container/tool availability, then re-run. Untested rows are not denied and cannot certify PASS.",
	title:         "European Feeder Segmentation - TCP Reachability Report",
	passText:      "**Result: PASS** - every listed TCP reachability probe matched its expected verdict. Acceptance also requires capture availability and stable policy identity. This is not certification of protocol operations, command authorization, process continuity, PCAP completeness or the whole policy.",
	captureIntro:  "Sources observed forwarded toward EU field devices during the capture window (best-effort tcpdump summary):",
	captureLimits: "Expected operational sources include the RTU and PLC; the pinned PLC-to-OLTC conduit crosses the firewall from station to field. Absence here is not proof of denial. Retain and inspect the raw PCAP separately.",
	limits:        "- PASS covers only this TCP matrix. A refused connection proves reachability, not a functioning application, and fails an unauthorized row. A timeout does not identify a decoded command or prove the firewall caused the loss. Missing tools are untested, not denied. Capture availability and stable hash are required by acceptance; PCAP completeness is not certified. Continuity and fresh observations require the scenario validator. No DPI or regulatory conformance is certified.\n",
	captureDir:    "/data/captures", hostCaptureDir: "data/firewall/captures",
}
