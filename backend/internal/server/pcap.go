package server

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/gin-gonic/gin"
	"github.com/tturner/rangerdanger/backend/internal/containd"
)

const firewallContainer = "rangerdanger-firewall"

// ---------- PCAP capture (via containd /api/v1/pcap/*, fallback to Docker exec) ----------

func (s *Server) handlePcapStart(c *gin.Context) {
	var req struct {
		DurationSec int    `json:"duration_sec"`
		Name        string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		req.DurationSec = 0
	}
	if req.DurationSec <= 0 {
		req.DurationSec = 30
	}

	prefix := req.Name
	if prefix == "" {
		prefix = fmt.Sprintf("rangerdanger-%s", time.Now().UTC().Format("20060102-150405"))
	}

	// Build containd PCAP config: mode=once, capture all interfaces, auto-stop via rotateSeconds
	cfg := containd.PcapConfig{
		Enabled:       true,
		Interfaces:    []string{"eth0", "eth1", "eth2", "eth3"}, // wan, dmz, lan1, lan2
		Snaplen:       262144,
		MaxSizeMB:     64,
		MaxFiles:      10,
		Mode:          "once",
		Promisc:       true,
		BufferMB:      4,
		RotateSeconds: req.DurationSec,
		FilePrefix:    prefix,
		Filter:        containd.PcapFilter{Proto: "any"},
	}

	// Try containd PCAP API: start with config inline
	status, err := s.containdClient.StartPcap(&cfg)
	if err == nil {
		s.pcapMu.Lock()
		s.pcap = pcapState{
			Capturing:   true,
			DurationSec: req.DurationSec,
			StartedAt:   status.StartedAt,
			FileReady:   false,
			FilePrefix:  prefix,
		}
		s.pcapMu.Unlock()

		// Poll containd until capture stops
		go s.pollPcapCompletion(prefix, req.DurationSec)

		c.JSON(http.StatusOK, gin.H{
			"status":       "capturing",
			"duration_sec": req.DurationSec,
			"file_prefix":  prefix,
		})
		return
	}

	// Fallback: Docker exec tcpdump on firewall container
	log.Printf("[pcap] containd PCAP API not available (%v), using tcpdump fallback", err)
	s.startTcpdumpFallback(c, req.DurationSec, nil, "")
}

// pollPcapCompletion polls containd /pcap/status until running==false,
// then queries /pcap/list to find files matching our prefix.
func (s *Server) pollPcapCompletion(prefix string, durationSec int) {
	deadline := time.Now().Add(time.Duration(durationSec+15) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		status, err := s.containdClient.GetPcapStatus()
		if err != nil {
			continue
		}
		if !status.Running {
			break
		}
	}

	// Capture done — list files matching our prefix
	files, err := s.containdClient.ListPcapFiles()
	var matchedNames []string
	if err == nil {
		for _, f := range files {
			if strings.HasPrefix(f.Name, prefix) {
				matchedNames = append(matchedNames, f.Name)
			}
		}
	}

	s.pcapMu.Lock()
	s.pcap.Capturing = false
	s.pcap.FileReady = len(matchedNames) > 0
	s.pcap.Files = matchedNames
	s.pcapMu.Unlock()

	if len(matchedNames) > 0 {
		log.Printf("[pcap] containd capture complete: %d files (prefix=%s)", len(matchedNames), prefix)
	} else {
		log.Printf("[pcap] containd capture complete but no files found for prefix=%s", prefix)
	}
}

func (s *Server) handlePcapStop(c *gin.Context) {
	s.pcapMu.Lock()
	isFallback := s.pcap.Fallback
	prefix := s.pcap.FilePrefix
	s.pcapMu.Unlock()

	if isFallback {
		s.stopTcpdumpFallback(c)
		return
	}

	status, err := s.containdClient.StopPcap()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("stop pcap: %v", err)})
		return
	}

	// Collect files matching our prefix
	files, _ := s.containdClient.ListPcapFiles()
	var matchedNames []string
	for _, f := range files {
		if strings.HasPrefix(f.Name, prefix) {
			matchedNames = append(matchedNames, f.Name)
		}
	}

	s.pcapMu.Lock()
	s.pcap.Capturing = status.Running
	s.pcap.FileReady = len(matchedNames) > 0
	s.pcap.Files = matchedNames
	s.pcapMu.Unlock()

	c.JSON(http.StatusOK, gin.H{"status": "stopped", "files": matchedNames})
}

func (s *Server) handlePcapStatus(c *gin.Context) {
	s.pcapMu.Lock()
	state := s.pcap
	s.pcapMu.Unlock()

	// If using containd, get fresh status
	if !state.Fallback && state.FilePrefix != "" {
		status, err := s.containdClient.GetPcapStatus()
		if err == nil {
			c.JSON(http.StatusOK, gin.H{
				"capturing":    status.Running,
				"duration_sec": state.DurationSec,
				"started_at":   status.StartedAt,
				"file_ready":   state.FileReady,
				"file_prefix":  state.FilePrefix,
				"files":        state.Files,
				"last_error":   status.LastError,
			})
			return
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"capturing":    state.Capturing,
		"duration_sec": state.DurationSec,
		"started_at":   state.StartedAt,
		"file_ready":   state.FileReady,
		"file_prefix":  state.FilePrefix,
		"files":        state.Files,
	})
}

// handlePcapDownload downloads the first (or only) capture file.
// For multi-file captures, use /download/:name instead.
func (s *Server) handlePcapDownload(c *gin.Context) {
	s.pcapMu.Lock()
	state := s.pcap
	s.pcapMu.Unlock()

	// Try containd: download first matched file
	if !state.Fallback && len(state.Files) > 0 {
		name := state.Files[0]
		body, filename, err := s.containdClient.DownloadPcapFile(name)
		if err == nil {
			defer body.Close()
			c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
			c.Header("Content-Type", "application/vnd.tcpdump.pcap")
			c.Status(http.StatusOK)
			if _, err := io.Copy(c.Writer, body); err != nil {
				log.Printf("[pcap] download stream error: %v", err)
			}
			return
		}
		log.Printf("[pcap] containd download failed (%v), trying fallback", err)
	}

	// Fallback: copy from container
	s.downloadFromContainerFallback(c)
}

// handlePcapDownloadFile downloads a specific PCAP file by name from containd.
func (s *Server) handlePcapDownloadFile(c *gin.Context) {
	name := c.Param("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file name required"})
		return
	}

	body, filename, err := s.containdClient.DownloadPcapFile(name)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("download failed: %v", err)})
		return
	}
	defer body.Close()

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Header("Content-Type", "application/vnd.tcpdump.pcap")
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, body); err != nil {
		log.Printf("[pcap] download stream error: %v", err)
	}
}

func (s *Server) handlePcapList(c *gin.Context) {
	files, err := s.containdClient.ListPcapFiles()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"files": []interface{}{}, "error": err.Error()})
		return
	}

	// Convert to response format
	type fileEntry struct {
		Name      string   `json:"name"`
		Interface string   `json:"interface"`
		SizeBytes int64    `json:"sizeBytes"`
		CreatedAt string   `json:"createdAt"`
		Tags      []string `json:"tags"`
		Status    string   `json:"status"`
	}
	entries := make([]fileEntry, 0, len(files))
	for _, f := range files {
		tags := f.Tags
		if tags == nil {
			tags = []string{}
		}
		entries = append(entries, fileEntry{
			Name:      f.Name,
			Interface: f.Interface,
			SizeBytes: f.SizeBytes,
			CreatedAt: f.CreatedAt,
			Tags:      tags,
			Status:    f.Status,
		})
	}
	c.JSON(http.StatusOK, gin.H{"files": entries})
}

// ---------- Fallback: Docker exec tcpdump ----------

func (s *Server) startTcpdumpFallback(c *gin.Context, durationSec int, interfaces []string, filter string) {
	dockerCli := s.orchestrator.DockerClient()
	if dockerCli == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "docker client not available"})
		return
	}

	s.pcapMu.Lock()
	if s.pcap.Capturing {
		s.pcapMu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "capture already in progress"})
		return
	}
	s.pcap = pcapState{
		Capturing:   true,
		DurationSec: durationSec,
		StartedAt:   time.Now().UTC().Format(time.RFC3339),
		FileReady:   false,
		Fallback:    true,
	}
	s.pcapMu.Unlock()

	iface := "any"
	if len(interfaces) == 1 {
		iface = interfaces[0]
	}
	cmd := fmt.Sprintf("tcpdump -i %s -w /tmp/capture.pcap -G %d -W 1", iface, durationSec)
	if filter != "" {
		cmd += " " + filter
	}

	go func() {
		ctx := context.Background()
		execCfg := container.ExecOptions{
			Cmd:          []string{"sh", "-c", cmd},
			AttachStdout: false,
			AttachStderr: false,
			Privileged:   true,
		}
		execID, err := dockerCli.ContainerExecCreate(ctx, firewallContainer, execCfg)
		if err != nil {
			log.Printf("[pcap] exec create failed: %v", err)
			s.pcapMu.Lock()
			s.pcap.Capturing = false
			s.pcapMu.Unlock()
			return
		}
		if err := dockerCli.ContainerExecStart(ctx, execID.ID, container.ExecStartOptions{}); err != nil {
			log.Printf("[pcap] exec start failed: %v", err)
			s.pcapMu.Lock()
			s.pcap.Capturing = false
			s.pcapMu.Unlock()
			return
		}
		for {
			inspect, err := dockerCli.ContainerExecInspect(ctx, execID.ID)
			if err != nil {
				break
			}
			if !inspect.Running {
				break
			}
			time.Sleep(1 * time.Second)
		}
		s.pcapMu.Lock()
		s.pcap.Capturing = false
		s.pcap.FileReady = true
		s.pcapMu.Unlock()
		log.Println("[pcap] fallback capture complete")
	}()

	c.JSON(http.StatusOK, gin.H{
		"status":       "capturing",
		"duration_sec": durationSec,
		"mode":         "fallback",
	})
}

func (s *Server) stopTcpdumpFallback(c *gin.Context) {
	dockerCli := s.orchestrator.DockerClient()
	if dockerCli == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "docker client not available"})
		return
	}

	ctx := c.Request.Context()
	execCfg := container.ExecOptions{
		Cmd:          []string{"sh", "-c", "killall tcpdump 2>/dev/null; true"},
		AttachStdout: false,
		AttachStderr: false,
		Privileged:   true,
	}
	execID, err := dockerCli.ContainerExecCreate(ctx, firewallContainer, execCfg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("exec create: %v", err)})
		return
	}
	if err := dockerCli.ContainerExecStart(ctx, execID.ID, container.ExecStartOptions{}); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("exec start: %v", err)})
		return
	}

	s.pcapMu.Lock()
	s.pcap.Capturing = false
	s.pcap.FileReady = true
	s.pcapMu.Unlock()

	c.JSON(http.StatusOK, gin.H{"status": "stopped"})
}

func (s *Server) downloadFromContainerFallback(c *gin.Context) {
	s.pcapMu.Lock()
	ready := s.pcap.FileReady
	s.pcapMu.Unlock()

	if !ready {
		c.JSON(http.StatusNotFound, gin.H{"error": "no capture file available"})
		return
	}

	dockerCli := s.orchestrator.DockerClient()
	if dockerCli == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "docker client not available"})
		return
	}

	ctx := c.Request.Context()
	reader, _, err := dockerCli.CopyFromContainer(ctx, firewallContainer, "/tmp/capture.pcap")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("copy from container: %v", err)})
		return
	}
	defer reader.Close()

	// CopyFromContainer returns a tar archive — extract the file
	tr := tar.NewReader(reader)
	if _, err := tr.Next(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("read tar header: %v", err)})
		return
	}

	c.Header("Content-Disposition", "attachment; filename=substation-capture.pcap")
	c.Header("Content-Type", "application/vnd.tcpdump.pcap")
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, tr); err != nil {
		log.Printf("[pcap] download stream error: %v", err)
	}
}

// ---------- Traffic generation ----------

func (s *Server) handleTrafficGenerate(c *gin.Context) {
	var req struct {
		DurationSec int `json:"duration_sec"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		req.DurationSec = 0
	}
	if req.DurationSec <= 0 {
		req.DurationSec = 30
	}

	dockerCli := s.orchestrator.DockerClient()
	if dockerCli == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "docker client not available"})
		return
	}

	s.trafficMu.Lock()
	if s.traffic.Generating {
		s.trafficMu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "traffic generation already in progress"})
		return
	}
	s.traffic = trafficState{
		Generating:     true,
		DurationSec:    req.DurationSec,
		StartedAt:      time.Now().UTC().Format(time.RFC3339),
		FlowsGenerated: 0,
	}
	s.trafficMu.Unlock()

	go s.runTrafficGeneration(req.DurationSec)

	c.JSON(http.StatusOK, gin.H{
		"status":       "generating",
		"duration_sec": req.DurationSec,
	})
}

func (s *Server) handleTrafficStatus(c *gin.Context) {
	s.trafficMu.Lock()
	state := s.traffic
	s.trafficMu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"generating":      state.Generating,
		"started_at":      state.StartedAt,
		"flows_generated": state.FlowsGenerated,
	})
}

// Raw Modbus TCP frames for realistic OT traffic generation.
// All frames use Unit ID 0x01 and transaction ID 0x00 0x01.
//
// Frame format: [txn_id:2][proto_id:2][length:2][unit_id:1][fc:1][data...]
const (
	// FC3: Read Holding Registers — start=0, count=10
	// Used for: RTAC reading device config, historian collecting data
	modbusFC3Read10 = `\x00\x01\x00\x00\x00\x06\x01\x03\x00\x00\x00\x0a`

	// FC4: Read Input Registers — start=0, count=8
	// Used for: RTAC reading analog measurements (voltage, current)
	modbusFC4Read8 = `\x00\x02\x00\x00\x00\x06\x01\x04\x00\x00\x00\x08`

	// FC1: Read Coils — start=0, count=16
	// Used for: RTAC reading device status bits (breaker state, alarms)
	modbusFC1Read16 = `\x00\x03\x00\x00\x00\x06\x01\x01\x00\x00\x00\x10`

	// FC2: Read Discrete Inputs — start=0, count=8
	// Used for: RTAC reading alarm/status discrete inputs
	modbusFC2Read8 = `\x00\x04\x00\x00\x00\x06\x01\x02\x00\x00\x00\x08`
)

// modbusRead sends a raw Modbus TCP read frame and discards the response.
// Uses busybox nc available in Alpine containers.
func modbusReadCmd(host string, frame string) string {
	return fmt.Sprintf("echo -ne '%s' | nc -w 2 %s 502 > /dev/null 2>&1 || true", frame, host)
}

// runTrafficGeneration generates representative OT traffic from actual source
// containers using correct protocols for each device type:
//
//   - RTAC → field devices:  Modbus TCP reads (FC1/FC3/FC4) on port 502
//   - HMI → RTAC:           Modbus TCP reads (FC3/FC4) on port 502
//   - Historian → RTAC:     Modbus TCP reads (FC3/FC4) on port 502
//   - GPS → RTAC/devices:   NTP time sync (UDP 123)
//   - Eng WS → RTAC:        HTTP (engineering management access)
//   - Vendor jump → HMI:    HTTP (HMI web UI)
//   - Corp WS → proxy:      HTTP (web browsing)
//   - Kali → OT devices:    Modbus scan (suspicious cross-zone)
func (s *Server) runTrafficGeneration(durationSec int) {
	ctx := context.Background()
	deadline := time.Now().Add(time.Duration(durationSec) * time.Second)
	flows := 0

	// Traffic patterns organized by source container and protocol.
	// Each entry represents one realistic communication flow.
	targets := []struct {
		container string
		cmd       string
		desc      string // for logging
	}{
		// ── RTAC → field devices: Modbus TCP polling (ot_ops → field, cross-zone) ──
		// RTAC supervisory controller polls all field devices via Modbus reads.
		// This is the primary OT control traffic and the most important to preserve.
		{"rangerdanger-rtac-sim", modbusReadCmd("10.40.40.20", modbusFC1Read16), "rtac→relay modbus FC1"},
		{"rangerdanger-rtac-sim", modbusReadCmd("10.40.40.20", modbusFC4Read8), "rtac→relay modbus FC4"},
		{"rangerdanger-rtac-sim", modbusReadCmd("10.40.40.21", modbusFC1Read16), "rtac→recloser modbus FC1"},
		{"rangerdanger-rtac-sim", modbusReadCmd("10.40.40.21", modbusFC3Read10), "rtac→recloser modbus FC3"},
		{"rangerdanger-rtac-sim", modbusReadCmd("10.40.40.22", modbusFC3Read10), "rtac→regulator modbus FC3"},
		{"rangerdanger-rtac-sim", modbusReadCmd("10.40.40.22", modbusFC4Read8), "rtac→regulator modbus FC4"},
		{"rangerdanger-rtac-sim", modbusReadCmd("10.40.40.23", modbusFC1Read16), "rtac→capbank modbus FC1"},
		{"rangerdanger-rtac-sim", modbusReadCmd("10.40.40.23", modbusFC3Read10), "rtac→capbank modbus FC3"},

		// ── RTAC → physics engine: HTTP (internal simulation, not firewalled) ──
		{"rangerdanger-rtac-sim", "wget -qO- http://10.50.50.20:8080/api/electrical 2>/dev/null || true", "rtac→opendss http"},

		// ── HMI → RTAC: Modbus TCP reads (intra ot_ops) ──
		// Operator HMI reads aggregated state from RTAC via Modbus.
		{"rangerdanger-fuxa-hmi", modbusReadCmd("10.30.30.20", modbusFC3Read10), "hmi→rtac modbus FC3"},
		{"rangerdanger-fuxa-hmi", modbusReadCmd("10.30.30.20", modbusFC1Read16), "hmi→rtac modbus FC1"},
		{"rangerdanger-fuxa-hmi", modbusReadCmd("10.30.30.20", modbusFC4Read8), "hmi→rtac modbus FC4"},

		// ── Historian → RTAC: Modbus TCP reads (intra ot_ops) ──
		// Data historian collects measurements from RTAC for trending/archival.
		{"rangerdanger-historian-sim", modbusReadCmd("10.30.30.20", modbusFC3Read10), "historian→rtac modbus FC3"},
		{"rangerdanger-historian-sim", modbusReadCmd("10.30.30.20", modbusFC4Read8), "historian→rtac modbus FC4"},
		{"rangerdanger-historian-sim", modbusReadCmd("10.30.30.20", modbusFC2Read8), "historian→rtac modbus FC2"},

		// ── GPS time server → devices: NTP time sync (intra ot_ops + cross-zone) ──
		// GPS clock broadcasts time to RTAC and field devices for SOE timestamping.
		// Use ntpdate-style UDP to port 123. Alpine busybox nc supports -u for UDP.
		{"rangerdanger-gps-sim", "echo -ne '\\x1b\\x00\\x00\\x00\\x00\\x00\\x00\\x00' | nc -u -w 1 10.30.30.20 123 2>/dev/null || true", "gps→rtac ntp"},
		{"rangerdanger-gps-sim", "echo -ne '\\x1b\\x00\\x00\\x00\\x00\\x00\\x00\\x00' | nc -u -w 1 10.40.40.20 123 2>/dev/null || true", "gps→relay ntp"},
		{"rangerdanger-gps-sim", "echo -ne '\\x1b\\x00\\x00\\x00\\x00\\x00\\x00\\x00' | nc -u -w 1 10.40.40.21 123 2>/dev/null || true", "gps→recloser ntp"},

		// ── Engineering workstation → RTAC: HTTP (vendor → ot_ops, cross-zone) ──
		// Engineering maintenance access — legitimate but should be monitored.
		{"rangerdanger-eng-ws", "wget -qO- http://10.30.30.20:8080/api/state 2>/dev/null || true", "eng-ws→rtac http"},
		{"rangerdanger-eng-ws", "wget -qO- http://10.30.30.20:8080/api/health 2>/dev/null || true", "eng-ws→rtac http health"},

		// ── Vendor jump box → HMI: HTTP (vendor → ot_ops, cross-zone) ──
		// Vendor remote access to HMI web interface for monitoring.
		{"rangerdanger-vendor-jump", "wget -qO- http://10.30.30.10:1881/ 2>/dev/null || true", "vendor→hmi http"},

		// ── Corporate workstation → proxy: HTTP (intra enterprise) ──
		{"rangerdanger-corp-ws", "wget -qO- http://10.10.10.3:8080/ 2>/dev/null || true", "corp→proxy http"},

		// ── Suspicious/attack traffic (visible in baseline as anomalies) ──

		// Enterprise → field devices: Modbus scan (should be blocked with improved config)
		{"rangerdanger-kali", modbusReadCmd("10.40.40.20", modbusFC1Read16), "kali→relay modbus scan"},
		{"rangerdanger-kali", modbusReadCmd("10.40.40.21", modbusFC1Read16), "kali→recloser modbus scan"},

		// Enterprise → OT ops: Modbus probe (suspicious cross-zone)
		{"rangerdanger-kali", modbusReadCmd("10.30.30.20", modbusFC3Read10), "kali→rtac modbus probe"},
	}

	dockerCli := s.orchestrator.DockerClient()
	if dockerCli == nil {
		log.Println("[traffic] docker client not available")
		s.trafficMu.Lock()
		s.traffic.Generating = false
		s.trafficMu.Unlock()
		return
	}

	for time.Now().Before(deadline) {
		for _, t := range targets {
			if time.Now().After(deadline) {
				break
			}
			execCfg := container.ExecOptions{
				Cmd:          []string{"sh", "-c", t.cmd},
				AttachStdout: false,
				AttachStderr: false,
			}
			execID, err := dockerCli.ContainerExecCreate(ctx, t.container, execCfg)
			if err != nil {
				log.Printf("[traffic] exec create %s: %v", t.desc, err)
				continue
			}
			if err := dockerCli.ContainerExecStart(ctx, execID.ID, container.ExecStartOptions{}); err != nil {
				log.Printf("[traffic] exec start %s: %v", t.desc, err)
				continue
			}
			flows++
			s.trafficMu.Lock()
			s.traffic.FlowsGenerated = flows
			s.trafficMu.Unlock()
			time.Sleep(200 * time.Millisecond)
		}
		// Brief pause between polling cycles — mimics 1-sec SCADA scan rate
		if time.Now().Before(deadline) {
			time.Sleep(500 * time.Millisecond)
		}
	}

	s.trafficMu.Lock()
	s.traffic.Generating = false
	s.traffic.FlowsGenerated = flows
	s.trafficMu.Unlock()
	log.Printf("[traffic] generation complete: %d flows generated", flows)
}
