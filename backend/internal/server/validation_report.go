package server

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
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

// validationProbe is one row of the segmentation validation matrix,
// resolved against the range (packageRecipe.resolveValidation).
type validationProbe struct {
	Src              string // container name
	SrcLabel         string // friendly source name for the report
	Dst              string
	Port             string
	Expected         string // "allow" | "deny"
	Note             string
	Category         string // "authorized" | "unauthorized"
	ID               string
	PlanExpected     string
	HardenedExpected string
}

type validationRow struct {
	validationProbe
	Actual     string // "allow" | "deny" | "skipped"
	DurationMs int
	Pass       bool
	ProbeError string
}

// validationTally is the aggregated result of a probe matrix run.
type validationTally struct {
	authPass, authTotal     int
	unauthPass, unauthTotal int
	skipped                 int
	pass                    bool
}

// tallyValidation aggregates probe rows into pass/total counts and the overall
// pass/fail verdict.
//
// A "skipped" probe (its source container couldn't be exec'd into - renamed,
// stopped, or missing) did not actually test anything, so it is counted
// separately and excluded from the authorized/unauthorized pass ratios. It is
// NOT treated as a pass: any skipped probe forces the overall result to FAIL,
// because a report cannot honestly claim PASS when part of the validation
// matrix never ran. (Earlier behavior silently dropped skipped rows, letting a
// partially-running lab report PASS as long as the probes that did run matched.)
func tallyValidation(rows []validationRow) validationTally {
	var t validationTally
	for _, r := range rows {
		if r.Actual == "skipped" {
			t.skipped++
			continue
		}
		if r.Category == "authorized" {
			t.authTotal++
			if r.Pass {
				t.authPass++
			}
		} else {
			t.unauthTotal++
			if r.Pass {
				t.unauthPass++
			}
		}
	}
	t.pass = t.authPass == t.authTotal && t.unauthPass == t.unauthTotal &&
		(t.authTotal+t.unauthTotal) > 0 && t.skipped == 0
	return t
}

const validationProbeTimeoutSec = 3
const validationPcapDurationSec = 8

// handleValidationReport runs the probe matrix against the active policy,
// captures PCAP, and returns the markdown report + a structured summary.
func (s *Server) handleValidationReport(c *gin.Context) {
	recipe, ok := supportedRecipe(c, func(r *packageRecipe) bool { return len(r.validationCapture.nodes) > 0 })
	if !ok {
		return
	}
	cli := s.orchestrator.DockerClient()
	if cli == nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "docker client not available"})
		return
	}
	ctx := c.Request.Context()
	gen := rangeOf(c)
	s.activeConfigMu.RLock()
	active, source := s.activeConfig, s.policySource
	policyAt, wantHash := s.policyChangedAt, s.lastAppliedHash
	provenance := s.planProvenance
	s.activeConfigMu.RUnlock()

	expectations, err := s.judgingExpectations(gen)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	matrix, err := resolveProof(gen, expectations, "", false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	captureHosts, captureNet, err := recipe.resolveCapture(gen)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	firewall, err := firewallContainer(gen)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	reportStart := time.Now().UTC()
	fullHash, hashErr := gen.Containd().GetFirewallHash(ctx)
	hash := strings.TrimPrefix(fullHash, "sha256:")
	if hashErr != nil || hash == "" {
		hash = "unknown"
	}

	// Start a short PCAP at the firewall covering the field zone, so the
	// report can show which sources actually reached field devices.
	ts := time.Now().UTC().Format("20060102T150405Z")
	pcapPath := recipe.report.captureDir + "/validation-" + ts + ".pcap"
	pcapHostPath := recipe.report.hostCaptureDir + "/validation-" + ts + ".pcap"
	pcapCmd := fmt.Sprintf(
		"mkdir -p %s 2>/tmp/validation-%s.log && timeout %d tcpdump -i any -w %s 'host %s' >/dev/null 2>>/tmp/validation-%s.log",
		recipe.report.captureDir, ts, validationPcapDurationSec, pcapPath, strings.Join(captureHosts, " or host "), ts)
	pcapExecID, pcapStartErr := s.execDetached(ctx, cli, firewall, []string{"sh", "-c", pcapCmd})
	time.Sleep(1 * time.Second) // let tcpdump open the file

	// Run the probe matrix concurrently (independent containers; the PCAP
	// is supporting evidence). The bounded EU matrix may outlast the short
	// capture window; PCAP completeness is not part of its verdict.
	rows := runEUProbeMatrix(ctx, matrix, func(ctx context.Context, p validationProbe) validationRow {
		return s.strictTCPProbe(ctx, cli, p)
	})

	tally := tallyValidation(rows)

	// Wait for tcpdump to exit, then read the capture. Starting a detached
	// exec is not evidence that tcpdump ran successfully or wrote packets.
	pcapSummary := "(PCAP capture unavailable)"
	pcapOK := false
	switch {
	case pcapStartErr != nil:
		pcapSummary = "Capture could not start: " + pcapStartErr.Error()
	default:
		exitCode, waitErr := s.waitDetachedExec(ctx, cli, pcapExecID, (validationPcapDurationSec+3)*time.Second)
		logPath := "/tmp/validation-" + ts + ".log"
		logOutput, _, logErr := s.execCapture(ctx, cli, firewall, []string{"sh", "-c", "cat " + logPath + " 2>/dev/null; rm -f " + logPath})
		captureErr := waitErr
		if captureErr == nil {
			captureErr = captureCommandError(exitCode, logOutput)
		}
		if captureErr != nil {
			if logErr != nil {
				pcapSummary = fmt.Sprintf("Capture failed: %v (capture log unavailable: %v)", captureErr, logErr)
			} else {
				pcapSummary = "Capture failed: " + captureErr.Error()
			}
			break
		}
		pcapSummary, err = s.analyzePcapSources(ctx, cli, firewall, pcapPath, captureNet)
		if err != nil {
			pcapSummary = "Capture analysis failed: " + err.Error()
			break
		}
		pcapOK = true
	}

	reportEnd := time.Now().UTC()
	finalHash, finalHashErr := gen.Containd().GetFirewallHash(ctx)
	s.activeConfigMu.RLock()
	stable := hashErr == nil && finalHashErr == nil && fullHash != "" && fullHash == finalHash &&
		fullHash == wantHash && s.lastAppliedHash == wantHash && s.policyChangedAt.Equal(policyAt)
	s.activeConfigMu.RUnlock()
	if !stable || !pcapOK {
		tally.pass = false
	}

	planProof, acceptance := judgeValidation(rows, source == "plan" && provenance != nil, fullHash, stable, pcapOK)
	if provenance != nil && provenance.PolicyHash != fullHash {
		planProof.Pass = false
		planProof.HashStable = false
	}
	authPass, authTotal := tally.authPass, tally.authTotal
	unauthPass, unauthTotal := tally.unauthPass, tally.unauthTotal
	skipped, result := tally.skipped, acceptance.Pass
	md := recipe.renderValidationMarkdown(active, source, hash, pcapHostPath, pcapOK, pcapSummary,
		rows, authPass, authTotal, unauthPass, unauthTotal, skipped, result)

	md += fmt.Sprintf("\nPolicy proof window: %s — %s; full firewall hash `%s`; stable: %t.\n", reportStart.Format(time.RFC3339Nano), reportEnd.Format(time.RFC3339Nano), fullHash, stable)
	md += fmt.Sprintf("\nPlan proof: applicable %t; PASS %t. Hardened acceptance: PASS %t; prevention gaps: %v.\n", planProof.Applicable, planProof.Pass, acceptance.Pass, acceptance.PreventionGaps)
	s.activeConfigMu.Lock()
	if s.lastAppliedHash == wantHash && s.policyChangedAt.Equal(policyAt) {
		s.lastValidationReport = &retainedValidationReport{Hash: fullHash, Markdown: md, RangeStart: gen.StartedAt, PolicyAt: policyAt, StartedAt: reportStart, FinishedAt: reportEnd, PlanProof: planProof, Acceptance: acceptance}
	}
	s.activeConfigMu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"markdown":   md,
		"planProof":  planProof,
		"acceptance": acceptance,
		"summary": gin.H{
			"authorized_pass":    authPass,
			"authorized_total":   authTotal,
			"unauthorized_pass":  unauthPass,
			"unauthorized_total": unauthTotal,
			"skipped":            skipped,
			"result":             map[bool]string{true: "PASS", false: "FAIL"}[result],
		},
		"policy_hash":   fullHash,
		"window_start":  reportStart,
		"window_end":    reportEnd,
		"policy_stable": stable,
		"active_config": active,
		"policy_source": source,
		"pcap_path":     pcapHostPath,
	})
}

// analyzePcapSources reads the capture on the firewall and aggregates the
// source IPs that were actually FORWARDED toward field (direction "Out" on
// tcpdump -i any).
// captureNet is the first three octets of the captured devices' /24, whose
// own traffic the summary leaves out.
func (s *Server) analyzePcapSources(ctx context.Context, cli *client.Client, firewall, pcapPath string, captureNet []string) (string, error) {
	out, exitCode, err := s.execCapture(ctx, cli, firewall, []string{"tcpdump", "-r", pcapPath, "-nn"})
	if err != nil {
		return "", fmt.Errorf("read PCAP: %w", err)
	}
	if err := commandExecutionError(exitCode, out); err != nil {
		return "", fmt.Errorf("read PCAP: %w", err)
	}
	return summarizePcapOutput(out, captureNet)
}

func summarizePcapOutput(output string, captureNet []string) (string, error) {
	if len(captureNet) < 3 {
		return "", fmt.Errorf("capture network must contain three octets")
	}
	packetCount := 0
	sourceCounts := map[string]int{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		packetType := false
		for _, field := range fields {
			if field == "IP" || field == "IP6" {
				packetType = true
				break
			}
		}
		if !packetType {
			continue
		}
		packetCount++
		if len(fields) < 5 || fields[2] != "Out" || fields[3] != "IP" {
			continue
		}
		parts := strings.Split(strings.TrimSuffix(fields[4], ":"), ".")
		if len(parts) < 4 {
			continue
		}
		source := strings.Join(parts[:4], ".")
		if source == strings.Join(captureNet[:3], ".") || parts[0] == captureNet[0] && parts[1] == captureNet[1] && parts[2] == captureNet[2] {
			continue
		}
		sourceCounts[source]++
	}
	if packetCount == 0 {
		return "", fmt.Errorf("PCAP contains no analysable packets")
	}
	if len(sourceCounts) == 0 {
		return fmt.Sprintf("No forwarded source packets found among %d analysable packets.", packetCount), nil
	}

	sources := make([]string, 0, len(sourceCounts))
	for source := range sourceCounts {
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool {
		if sourceCounts[sources[i]] == sourceCounts[sources[j]] {
			return sources[i] < sources[j]
		}
		return sourceCounts[sources[i]] > sourceCounts[sources[j]]
	})
	if len(sources) > 10 {
		sources = sources[:10]
	}
	lines := make([]string, 0, len(sources))
	for _, source := range sources {
		lines = append(lines, fmt.Sprintf("%5d %s", sourceCounts[source], source))
	}
	return strings.Join(lines, "\n"), nil
}

func captureCommandError(exitCode int, output string) error {
	if exitCode == 0 || exitCode == 124 {
		return nil
	}
	return commandExecutionError(exitCode, output)
}

func commandExecutionError(exitCode int, output string) error {
	if exitCode == 0 {
		return nil
	}
	detail := strings.TrimSpace(output)
	if detail == "" {
		return fmt.Errorf("command exited with status %d", exitCode)
	}
	return fmt.Errorf("command exited with status %d: %s", exitCode, detail)
}

func (s *Server) waitDetachedExec(ctx context.Context, cli *client.Client, execID string, timeout time.Duration) (int, error) {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		inspection, err := cli.ContainerExecInspect(ctx, execID)
		if err != nil {
			return -1, fmt.Errorf("inspect capture exec: %w", err)
		}
		if !inspection.Running {
			return inspection.ExitCode, nil
		}
		select {
		case <-ctx.Done():
			return -1, fmt.Errorf("wait for capture exec: %w", ctx.Err())
		case <-deadline.C:
			return -1, fmt.Errorf("capture exec did not finish within %s", timeout)
		case <-ticker.C:
		}
	}
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
	_, copyErr := stdcopy.StdCopy(&outBuf, &errBuf, att.Reader) // blocks until exec exits
	output := outBuf.String() + errBuf.String()
	if copyErr != nil {
		return output, -1, fmt.Errorf("read exec output: %w", copyErr)
	}
	insp, err := cli.ContainerExecInspect(ctx, execID.ID)
	if err != nil {
		return output, -1, err
	}
	return output, insp.ExitCode, nil
}

// execDetached fires a command without waiting for it (used for the
// background tcpdump capture) and returns its exec ID for completion checks.
func (s *Server) execDetached(ctx context.Context, cli *client.Client, containerName string, cmd []string) (string, error) {
	execID, err := cli.ContainerExecCreate(ctx, containerName, container.ExecOptions{Cmd: cmd})
	if err != nil {
		return "", err
	}
	if err := cli.ContainerExecStart(ctx, execID.ID, container.ExecStartOptions{Detach: true}); err != nil {
		return "", err
	}
	return execID.ID, nil
}

func (recipe *packageRecipe) renderValidationMarkdown(active, source, hash, pcapHostPath string, pcapOK bool, pcapSummary string,
	rows []validationRow, authPass, authTotal, unauthPass, unauthTotal, skipped int, result bool) string {

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
	fmt.Fprintf(&b, "# %s\n\n", recipe.report.title)
	fmt.Fprintf(&b, "| Field | Value |\n|---|---|\n")
	fmt.Fprintf(&b, "| **Generated** | %s |\n", now)
	fmt.Fprintf(&b, "| **Policy** | %s (source: `%s`, sha256:%s) |\n", policyLabel, source, hash)
	if pcapOK {
		fmt.Fprintf(&b, "| **PCAP evidence** | `%s` |\n", pcapHostPath)
	} else if strings.HasPrefix(pcapSummary, "Capture ") {
		detail := strings.NewReplacer("\n", " ", "|", "/").Replace(pcapSummary)
		fmt.Fprintf(&b, "| **PCAP evidence** | FAIL — %s |\n", detail)
	}
	fmt.Fprintf(&b, "| **Probe timeout** | %ds |\n\n", validationProbeTimeoutSec)

	fmt.Fprintf(&b, "## Summary\n\n| Category | Confirmed | Total |\n|---|---|---|\n")
	fmt.Fprintf(&b, "| **Authorized flows working** | %d | %d |\n", authPass, authTotal)
	fmt.Fprintf(&b, "| **Unauthorized flows blocked** | %d | %d |\n", unauthPass, unauthTotal)
	if skipped > 0 {
		fmt.Fprintf(&b, "| **Probes skipped (did not run)** | %d | %d |\n", skipped, len(rows))
	}
	b.WriteString("\n")
	if result {
		b.WriteString(recipe.report.passText + "\n\n")
	} else if skipped > 0 {
		fmt.Fprintf(&b, recipe.report.skippedText+"\n\n", skipped)
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
			note := r.Note
			if r.ProbeError != "" {
				note += " — Untested: " + strings.NewReplacer("\n", " ", "|", "/").Replace(r.ProbeError)
			}
			fmt.Fprintf(&b, "| %d | %s %s | `%s` | %s | %s | %s | %dms | %s |\n",
				n, mark, r.SrcLabel, r.Dst, r.Port, r.Expected, r.Actual, r.DurationMs, note)
		}
		b.WriteString("\n")
	}
	writeTable("Authorized flow tests", "Flows the substation depends on; each must succeed under the active policy.", "authorized")
	writeTable("Unauthorized flow tests", recipe.report.unauthorizedIntro, "unauthorized")

	if pcapOK {
		fmt.Fprintf(&b, "## PCAP source analysis\n\n")
		fmt.Fprintf(&b, "%s\n\n```\n%s\n```\n\n", recipe.report.captureIntro, pcapSummary)
		b.WriteString(recipe.report.captureLimits + "\n\n")
	}

	fmt.Fprintf(&b, "## Methodology\n\n")
	b.WriteString(recipe.report.methodology)
	b.WriteString(recipe.report.limits)
	return b.String()
}
