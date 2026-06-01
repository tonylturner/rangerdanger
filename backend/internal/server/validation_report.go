package server

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/gin-gonic/gin"
)

// validation_report.go powers Lab 2.4's "Generate Validation Report"
// button. It runs the positive/negative segmentation probe matrix against
// the CURRENTLY ACTIVE containd policy - it never applies a policy, so it
// validates whatever the student built (Apply Hardened or Apply Your
// Plan) rather than clobbering it - captures a short PCAP at the firewall,
// and returns a change-board-ready markdown report. This is the in-app
// equivalent of scripts/validation-report.sh.

// validationProbe is one row of the segmentation validation matrix. Mirrors
// the matrix in scripts/validation-report.sh (kept narrow + listener-backed
// so verdicts are reliable on Docker Desktop).
type validationProbe struct {
	Src      string // container name
	SrcLabel string // friendly source name for the report
	Dst      string
	Port     string
	Expected string // "allow" | "deny"
	Note     string
	Category string // "authorized" | "unauthorized"
}

var validationMatrix = []validationProbe{
	{"rangerdanger-rtac-sim", "rtac-sim", "10.40.40.20", "502", "allow", "RTAC Modbus poll to relay", "authorized"},
	{"rangerdanger-rtac-sim", "rtac-sim", "10.40.40.21", "20000", "allow", "RTAC DNP3 poll to recloser", "authorized"},
	{"rangerdanger-rtac-sim", "rtac-sim", "10.40.40.22", "20000", "allow", "RTAC DNP3 poll to regulator", "authorized"},
	{"rangerdanger-rtac-sim", "rtac-sim", "10.30.30.30", "8080", "allow", "RTAC HTTP API to OpenPLC (intra-zone)", "authorized"},
	{"rangerdanger-fuxa-hmi", "fuxa-hmi", "10.30.30.20", "8080", "allow", "HMI to RTAC HTTP intra-zone", "authorized"},
	{"rangerdanger-historian-sim", "historian-sim", "10.30.30.20", "8080", "allow", "Historian to RTAC intra-zone", "authorized"},
	{"rangerdanger-vendor-jump", "vendor-jump", "10.30.30.20", "22", "allow", "Vendor SSH mgmt to RTAC", "authorized"},
	{"rangerdanger-vendor-jump", "vendor-jump", "10.30.30.20", "443", "allow", "Vendor HTTPS mgmt to RTAC", "authorized"},
	{"rangerdanger-kali", "kali", "10.40.40.20", "502", "deny", "Enterprise Modbus to field relay", "unauthorized"},
	{"rangerdanger-kali", "kali", "10.40.40.20", "20000", "deny", "Enterprise DNP3 to field relay", "unauthorized"},
	{"rangerdanger-kali", "kali", "10.30.30.30", "8080", "deny", "Enterprise HTTP to OpenPLC", "unauthorized"},
	{"rangerdanger-kali", "kali", "10.30.30.20", "8080", "deny", "Enterprise HTTP to RTAC", "unauthorized"},
	{"rangerdanger-kali", "kali", "10.30.30.20", "502", "deny", "Enterprise Modbus to RTAC", "unauthorized"},
	{"rangerdanger-eng-ws", "eng-ws", "10.40.40.21", "502", "deny", "Vendor Modbus to field recloser", "unauthorized"},
	{"rangerdanger-eng-ws", "eng-ws", "10.40.40.21", "20000", "deny", "Vendor DNP3 to field recloser", "unauthorized"},
	{"rangerdanger-eng-ws", "eng-ws", "10.30.30.30", "8080", "deny", "Vendor HTTP to OpenPLC (only 443/22 allowed)", "unauthorized"},
	{"rangerdanger-vendor-jump", "vendor-jump", "10.30.30.20", "502", "deny", "Vendor Modbus to RTAC (improved blocks non-mgmt)", "unauthorized"},
	{"rangerdanger-historian-sim", "historian-sim", "10.40.40.22", "502", "deny", "Non-RTAC OT (historian) to field regulator (Modbus)", "unauthorized"},
	{"rangerdanger-historian-sim", "historian-sim", "10.40.40.22", "20000", "deny", "Non-RTAC OT (historian) to field regulator (DNP3)", "unauthorized"},
}

type validationRow struct {
	validationProbe
	Actual     string // "allow" | "deny" | "skipped"
	DurationMs int
	Pass       bool
}

const validationProbeTimeoutSec = 3
const validationPcapDurationSec = 8

// handleValidationReport runs the probe matrix against the active policy,
// captures PCAP, and returns the markdown report + a structured summary.
func (s *Server) handleValidationReport(c *gin.Context) {
	cli := s.orchestrator.DockerClient()
	if cli == nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "docker client not available"})
		return
	}
	ctx := context.Background()

	s.activeConfigMu.RLock()
	active, source := s.activeConfig, s.policySource
	s.activeConfigMu.RUnlock()

	hash := "unknown"
	if s.containdClient != nil {
		if h, err := s.containdClient.GetFirewallHash(); err == nil && h != "" {
			hash = strings.TrimPrefix(h, "sha256:")
			if len(hash) > 12 {
				hash = hash[:12]
			}
		}
	}

	// Start a short PCAP at the firewall covering the field zone, so the
	// report can show which sources actually reached field devices.
	ts := time.Now().UTC().Format("20060102T150405Z")
	pcapPath := "/data/captures/validation-" + ts + ".pcap"
	pcapHostPath := "data/firewall/captures/validation-" + ts + ".pcap"
	pcapCmd := fmt.Sprintf(
		"mkdir -p /data/captures; timeout %d tcpdump -i any -w %s 'host 10.40.40.20 or host 10.40.40.21 or host 10.40.40.22 or host 10.40.40.23' >/dev/null 2>&1",
		validationPcapDurationSec, pcapPath)
	pcapOK := s.execDetached(ctx, cli, firewallContainer, []string{"sh", "-c", pcapCmd}) == nil
	pcapStart := time.Now()
	time.Sleep(1 * time.Second) // let tcpdump open the file

	// Run the probe matrix concurrently (independent containers; the PCAP
	// captures all of it). Verdicts land well inside the PCAP window.
	rows := make([]validationRow, len(validationMatrix))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8) // cap concurrent docker execs to limit contention
	for i, p := range validationMatrix {
		wg.Add(1)
		go func(i int, p validationProbe) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			verdict, ms := s.probeTCP(ctx, cli, p.Src, p.Dst, p.Port, validationProbeTimeoutSec)
			// Authorized flows can momentarily lose a race to container /
			// routing warmup; retry once so a transient hiccup doesn't read
			// as a policy failure. Never retry unauthorized - a real leak
			// must surface, not be masked by a lucky second attempt.
			if p.Category == "authorized" && verdict != p.Expected {
				verdict, ms = s.probeTCP(ctx, cli, p.Src, p.Dst, p.Port, validationProbeTimeoutSec)
			}
			rows[i] = validationRow{
				validationProbe: p, Actual: verdict, DurationMs: ms,
				Pass: verdict == p.Expected,
			}
		}(i, p)
	}
	wg.Wait()

	var authPass, authTotal, unauthPass, unauthTotal int
	for _, r := range rows {
		if r.Actual == "skipped" {
			continue
		}
		if r.Category == "authorized" {
			authTotal++
			if r.Pass {
				authPass++
			}
		} else {
			unauthTotal++
			if r.Pass {
				unauthPass++
			}
		}
	}

	// Wait for the PCAP window to close, then analyze source IPs.
	pcapSummary := "(PCAP capture unavailable)"
	if pcapOK {
		if remain := time.Duration(validationPcapDurationSec)*time.Second - time.Since(pcapStart); remain > 0 {
			time.Sleep(remain)
		}
		pcapSummary = s.analyzePcapSources(ctx, cli, pcapPath)
	}

	result := authPass == authTotal && unauthPass == unauthTotal && (authTotal+unauthTotal) > 0
	md := renderValidationMarkdown(active, source, hash, pcapHostPath, pcapOK, pcapSummary,
		rows, authPass, authTotal, unauthPass, unauthTotal, result)

	c.JSON(http.StatusOK, gin.H{
		"markdown": md,
		"summary": gin.H{
			"authorized_pass":   authPass,
			"authorized_total":  authTotal,
			"unauthorized_pass": unauthPass,
			"unauthorized_total": unauthTotal,
			"result":            map[bool]string{true: "PASS", false: "FAIL"}[result],
		},
		"active_config": active,
		"policy_source": source,
		"pcap_path":     pcapHostPath,
	})
}

// probeTCP opens a TCP connection from src to dst:port and classifies the
// result as allow (reached) / deny (dropped) by exit code + duration -
// same heuristic as scripts/firewall-smoke.sh and validation-report.sh.
func (s *Server) probeTCP(ctx context.Context, cli *client.Client, src, dst, port string, timeoutSec int) (string, int) {
	cmd := []string{"sh", "-c", fmt.Sprintf(
		`if command -v bash >/dev/null 2>&1; then timeout %d bash -c "exec 3<>/dev/tcp/%s/%s"; else timeout %d nc -w %d %s %s </dev/null; fi`,
		timeoutSec, dst, port, timeoutSec, timeoutSec, dst, port)}
	start := time.Now()
	_, rc, err := s.execCapture(ctx, cli, src, cmd)
	ms := int(time.Since(start).Milliseconds())
	if err != nil {
		return "skipped", ms
	}
	switch {
	case rc == 0:
		return "allow", ms
	case rc == 124 || rc == 143:
		return "deny", ms
	case rc == 1:
		if ms < 500 {
			return "allow", ms
		}
		return "deny", ms
	default:
		return "deny", ms
	}
}

// analyzePcapSources reads the capture on the firewall and aggregates the
// source IPs that were actually FORWARDED toward field (direction "Out" on
// tcpdump -i any). Same awk as validation-report.sh.
func (s *Server) analyzePcapSources(ctx context.Context, cli *client.Client, pcapPath string) string {
	awk := `tcpdump -r ` + pcapPath + ` -nn 2>/dev/null | awk '` +
		`/^[0-9]/ { if ($3 != "Out") next; ip=$5; n=split(ip,a,"."); if (n<4) next; ` +
		`if (a[1]=="10" && a[2]=="40" && a[3]=="40") next; print a[1]"."a[2]"."a[3]"."a[4] }' ` +
		`| sort | uniq -c | sort -rn | head -10`
	out, _, err := s.execCapture(ctx, cli, firewallContainer, []string{"sh", "-c", awk})
	out = strings.TrimRight(out, "\n")
	if err != nil || strings.TrimSpace(out) == "" {
		return "(no packets captured during window)"
	}
	return out
}

// execCapture runs cmd in containerName, returns combined stdout, the exit
// code, and any error. Blocks until the command exits.
func (s *Server) execCapture(ctx context.Context, cli *client.Client, containerName string, cmd []string) (string, int, error) {
	execID, err := cli.ContainerExecCreate(ctx, containerName, container.ExecOptions{
		Cmd: cmd, AttachStdout: true, AttachStderr: true,
	})
	if err != nil {
		return "", -1, err
	}
	att, err := cli.ContainerExecAttach(ctx, execID.ID, container.ExecAttachOptions{})
	if err != nil {
		return "", -1, err
	}
	defer att.Close()
	var outBuf, errBuf bytes.Buffer
	_, _ = stdcopy.StdCopy(&outBuf, &errBuf, att.Reader) // blocks until exec exits
	insp, err := cli.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return outBuf.String(), -1, err
	}
	return outBuf.String(), insp.ExitCode, nil
}

// execDetached fires a command without waiting for it (used for the
// background tcpdump capture).
func (s *Server) execDetached(ctx context.Context, cli *client.Client, containerName string, cmd []string) error {
	execID, err := cli.ContainerExecCreate(ctx, containerName, container.ExecOptions{Cmd: cmd})
	if err != nil {
		return err
	}
	return cli.ContainerExecStart(ctx, execID.ID, container.ExecStartOptions{Detach: true})
}

func renderValidationMarkdown(active, source, hash, pcapHostPath string, pcapOK bool, pcapSummary string,
	rows []validationRow, authPass, authTotal, unauthPass, unauthTotal int, result bool) string {

	policyLabel := map[string]string{
		"weak":     "weak baseline",
		"improved": "hardened reference",
		"custom":   "your custom policy",
	}[active]
	if policyLabel == "" {
		policyLabel = active
	}

	var b strings.Builder
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	fmt.Fprintf(&b, "# Substation Segmentation - Validation Report\n\n")
	fmt.Fprintf(&b, "| Field | Value |\n|---|---|\n")
	fmt.Fprintf(&b, "| **Generated** | %s |\n", now)
	fmt.Fprintf(&b, "| **Policy** | %s (source: `%s`, sha256:%s) |\n", policyLabel, source, hash)
	if pcapOK {
		fmt.Fprintf(&b, "| **PCAP evidence** | `%s` |\n", pcapHostPath)
	}
	fmt.Fprintf(&b, "| **Probe timeout** | %ds |\n\n", validationProbeTimeoutSec)

	fmt.Fprintf(&b, "## Summary\n\n| Category | Confirmed | Total |\n|---|---|---|\n")
	fmt.Fprintf(&b, "| **Authorized flows working** | %d | %d |\n", authPass, authTotal)
	fmt.Fprintf(&b, "| **Unauthorized flows blocked** | %d | %d |\n\n", unauthPass, unauthTotal)
	if result {
		fmt.Fprintf(&b, "**Result: PASS** - every authorized flow works and every unauthorized flow is blocked. The active policy is enforcing what the design specified.\n\n")
	} else {
		fmt.Fprintf(&b, "**Result: FAIL** - one or more rows did not match the expected verdict. Inspect the ✗ rows below: an authorized flow that's blocked means a rule is too tight; an unauthorized flow that succeeds means a rule is too loose or missing.\n\n")
	}

	writeTable := func(title, intro, category string) {
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", title, intro)
		fmt.Fprintf(&b, "| # | Source | Destination | Port | Expected | Actual | Duration | Test |\n|---|---|---|---|---|---|---|---|\n")
		n := 0
		for _, r := range rows {
			if r.Category != category {
				continue
			}
			n++
			mark := "✓"
			if r.Actual == "skipped" {
				mark = "⊘"
			} else if !r.Pass {
				mark = "✗"
			}
			fmt.Fprintf(&b, "| %d | %s %s | `%s` | %s | %s | %s | %dms | %s |\n",
				n, mark, r.SrcLabel, r.Dst, r.Port, r.Expected, r.Actual, r.DurationMs, r.Note)
		}
		b.WriteString("\n")
	}
	writeTable("Authorized flow tests", "Flows the substation depends on; each must succeed under the active policy.", "authorized")
	writeTable("Unauthorized flow tests", "Flows the policy must deny. Each blocked attempt is positive evidence the segmentation is enforcing.", "unauthorized")

	if pcapOK {
		fmt.Fprintf(&b, "## PCAP source analysis\n\n")
		fmt.Fprintf(&b, "Sources whose traffic the firewall forwarded toward the field zone (`10.40.40.0/24`) during the capture window:\n\n```\n%s\n```\n\n", pcapSummary)
		fmt.Fprintf(&b, "Under a hardened policy only the RTAC (`10.30.30.20`) and the GPS/NTP server (`10.30.30.50`) should appear. Any other source IP indicates a missing rule or a leaky deny.\n\n")
	}

	fmt.Fprintf(&b, "## Methodology\n\n")
	fmt.Fprintf(&b, "- **allow** = the source container's TCP SYN reached the destination (connect succeeded, or a fast RST came back). **deny** = the SYN was dropped (timeout at the probe budget).\n")
	fmt.Fprintf(&b, "- Probes ran from inside each source container via `/dev/tcp` (or `nc` on busybox-only hosts) - the same primitive as `scripts/firewall-smoke.sh`.\n")
	fmt.Fprintf(&b, "- This report validates the **currently active** policy; it does not apply or change any policy.\n")
	return b.String()
}
