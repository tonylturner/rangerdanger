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

// runTrafficGeneration generates SCENARIO-DRIVEN traffic only. The
// autonomous OT baseline (RTAC polling, HMI polling, historian polling,
// GPS NTP broadcast) runs continuously inside the simulator services
// themselves and does NOT need to be triggered by the generator.
//
// What this function produces:
//
//   - Eng WS → RTAC:         HTTP (cross-zone engineering maintenance access)
//   - Eng WS → OpenPLC:      HTTP (cross-zone PLC programming access)
//   - Vendor jump → HMI:     HTTP (cross-zone vendor remote monitoring)
//
// These are "scenario setup" flows — they represent things a student
// would see while running an exercise that models engineering or vendor
// activity. They do not represent the 24x7 steady-state baseline.
//
// What the autonomous model handles instead (see services/*/):
//
//   - RTAC → field devices:  Modbus TCP reads  (rtac-sim:modbus_poll.go)
//   - RTAC → field devices:  DNP3 class 0 polls (rtac-sim:dnp3_poll.go)
//   - RTAC → field devices:  HTTP REST polling (rtac-sim:main.go pollDevices)
//   - HMI → RTAC:            Modbus via hmi_poller sidecar (docker-compose)
//   - Historian → RTAC:      HTTP via historian-sim pollRTAC()
//   - GPS → field devices:   NTP broadcast via gps-sim broadcastNTP()
func (s *Server) runTrafficGeneration(durationSec int) {
	ctx := context.Background()
	deadline := time.Now().Add(time.Duration(durationSec) * time.Second)
	flows := 0

	// Scenario-driven traffic only. The autonomous baseline runs
	// continuously in the simulator services and is NEVER produced here.
	//
	// IPs below are hard-coded against the substation lab topology
	// (lab-definitions/substation-segmentation.yml) and the static
	// docker-compose.yml network assignments. If the topology IPs
	// change, this list must change too — there is no auto-discovery
	// path. The relevant IPs:
	//   eng-ws       10.20.20.20  (vendor zone)
	//   rtac         10.30.30.20  (OT ops zone)
	//   openplc      10.30.30.30
	//   relay        10.40.40.20  (field zone)
	//   recloser     10.40.40.21
	//   regulator    10.40.40.22
	//   capbank      10.40.40.23
	targets := []struct {
		container string
		cmd       string
		desc      string // for logging
	}{
		// ─────────────────────────────────────────────────────────
		// Engineering workstation (10.20.20.20) — vendor zone
		//
		// eng-ws has mbpoll, dnp3poll, dnp3cmd, nc, ssh, nmap,
		// curl, and python3. We use real protocol tools so the
		// generated traffic is indistinguishable from what a human
		// engineer or attacker would produce.
		// ─────────────────────────────────────────────────────────

		// Eng-ws → RTAC: HTTP maintenance access (vendor→OT ops)
		{"rangerdanger-eng-ws", "curl -sf http://10.30.30.20:8080/api/state > /dev/null 2>&1 || true", "eng-ws→rtac http"},
		{"rangerdanger-eng-ws", "curl -sf http://10.30.30.20:8080/api/health > /dev/null 2>&1 || true", "eng-ws→rtac http health"},

		// Eng-ws → OpenPLC: PLC programming HTTP (vendor→OT ops)
		{"rangerdanger-eng-ws", "curl -sf http://10.30.30.30:8080/ > /dev/null 2>&1 || true", "eng-ws→openplc http"},

		// Eng-ws → HMI: FUXA web interface (vendor→OT ops)
		{"rangerdanger-eng-ws", "curl -sf http://10.30.30.10:1881/ > /dev/null 2>&1 || true", "eng-ws→hmi fuxa"},

		// ── Eng-ws → field devices: real ICS protocol traffic ──
		// WEAK baseline allows these; hardened blocks them. Students
		// see Modbus/DNP3 frames on the wire — not just HTTP curls.

		// Modbus FC03 reads (mbpoll: read 5 holding registers, 1 poll, 1s timeout)
		{"rangerdanger-eng-ws", "mbpoll -m tcp -a 1 -r 1 -c 5 -1 -t 1 10.40.40.20 > /dev/null 2>&1 || true", "eng-ws→relay modbus FC03 (WEAK)"},
		{"rangerdanger-eng-ws", "mbpoll -m tcp -a 1 -r 1 -c 5 -1 -t 1 10.40.40.21 > /dev/null 2>&1 || true", "eng-ws→recloser modbus FC03 (WEAK)"},
		{"rangerdanger-eng-ws", "mbpoll -m tcp -a 1 -r 1 -c 5 -1 -t 1 10.40.40.22 > /dev/null 2>&1 || true", "eng-ws→regulator modbus FC03 (WEAK)"},
		{"rangerdanger-eng-ws", "mbpoll -m tcp -a 1 -r 1 -c 5 -1 -t 1 10.40.40.23 > /dev/null 2>&1 || true", "eng-ws→capbank modbus FC03 (WEAK)"},

		// DNP3 class 0 polls (dnp3poll: read all static data, 2s timeout)
		{"rangerdanger-eng-ws", "dnp3poll 10.40.40.20:20000 -a 1 > /dev/null 2>&1 || true", "eng-ws→relay dnp3 poll (WEAK)"},
		{"rangerdanger-eng-ws", "dnp3poll 10.40.40.21:20000 -a 2 > /dev/null 2>&1 || true", "eng-ws→recloser dnp3 poll (WEAK)"},
		{"rangerdanger-eng-ws", "dnp3poll 10.40.40.22:20000 -a 3 > /dev/null 2>&1 || true", "eng-ws→regulator dnp3 poll (WEAK)"},
		{"rangerdanger-eng-ws", "dnp3poll 10.40.40.23:20000 -a 4 > /dev/null 2>&1 || true", "eng-ws→capbank dnp3 poll (WEAK)"},

		// NOTE: No HTTP/8080 to field devices. Real relays, reclosers,
		// regulators, and cap banks don't run web servers — Modbus
		// and DNP3 are the realistic access protocols. The HTTP API
		// on port 8080 is a lab convenience for the Go simulators;
		// including it in generated traffic would teach the wrong
		// mental model. Students should see Modbus FC03 + DNP3
		// class-0 polls only when eng-ws reaches into the field zone.

		// ─────────────────────────────────────────────────────────
		// Vendor jump box (10.20.20.10) — vendor zone
		// ─────────────────────────────────────────────────────────

		// Vendor → HMI: remote monitoring via FUXA (vendor→OT ops)
		{"rangerdanger-vendor-jump", "curl -sf http://10.30.30.10:1881/ > /dev/null 2>&1 || true", "vendor→hmi fuxa"},

		// Vendor → RTAC: support health check HTTP (vendor→OT ops)
		{"rangerdanger-vendor-jump", "curl -sf http://10.30.30.20:8080/api/health > /dev/null 2>&1 || true", "vendor→rtac http health"},
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
