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
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

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
	if req.Name != "" && !validPcapComponent(req.Name, 64) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid capture name"})
		return
	}

	prefix := req.Name
	if prefix == "" {
		prefix = fmt.Sprintf("rangerdanger-%s", time.Now().UTC().Format("20060102-150405"))
	}

	// containd's /api/v1/pcap/start resolves Interfaces[] entries
	// via netlink as literal kernel interface names — it does NOT
	// autobind zone names the way policy interfaces do (commit
	// 5f31128). And docker's ethN ordering is non-deterministic
	// across hosts (alphabetical network name, not compose order),
	// so any ethN pin shuffles on the wrong host and the field
	// zone silently drops out of the capture set. We leave Interfaces
	// empty here — containd will reject the call with
	// "config validation failed: pcap.enabled requires at least one
	// interface", and the backend falls through to its `tcpdump -i any`
	// path which is fully drift-proof. Once containd's PCAP API
	// accepts zone names, the right pin is `[wan,dmz,lan1,lan2,lan3]`.
	cfg := containd.PcapConfig{
		Enabled:       true,
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
	gen := rangeOf(c)
	status, err := gen.Containd().StartPcap(c.Request.Context(), &cfg)
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
		if !gen.Go("pcap-poll", func(ctx context.Context) {
			s.pollPcapCompletion(ctx, gen, prefix, req.DurationSec)
		}) {
			rangeUnavailable(c)
			return
		}

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
// then queries /pcap/list to find files matching our prefix. It is a
// generation worker; a result after the range stopped is dropped.
func (s *Server) pollPcapCompletion(ctx context.Context, gen *lifecycle.Generation, prefix string, durationSec int) {
	deadline := time.Now().Add(time.Duration(durationSec+15) * time.Second)
	for time.Now().Before(deadline) {
		if !sleepCtx(ctx, 2*time.Second) {
			return
		}
		status, err := gen.Containd().GetPcapStatus(ctx)
		if err != nil {
			continue
		}
		if !status.Running {
			break
		}
	}

	// Capture done — list files matching our prefix
	files, err := gen.Containd().ListPcapFiles(ctx)
	var matchedNames []string
	if err == nil {
		for _, f := range files {
			if strings.HasPrefix(f.Name, prefix) {
				matchedNames = append(matchedNames, f.Name)
			}
		}
	}

	if !gen.Commit(func() {
		s.pcapMu.Lock()
		s.pcap.Capturing = false
		s.pcap.FileReady = len(matchedNames) > 0
		s.pcap.Files = matchedNames
		s.pcapMu.Unlock()
	}) {
		return
	}

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

	status, err := rangeOf(c).Containd().StopPcap(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("stop pcap: %v", err)})
		return
	}

	// Collect files matching our prefix
	files, _ := rangeOf(c).Containd().ListPcapFiles(c.Request.Context())
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
		status, err := rangeOf(c).Containd().GetPcapStatus(c.Request.Context())
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
	if !state.Fallback && len(state.Files) > 0 && validPcapName(state.Files[0]) {
		name := state.Files[0]
		body, filename, err := rangeOf(c).Containd().DownloadPcapFile(c.Request.Context(), name)
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
	if !validPcapName(name) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid pcap file name"})
		return
	}

	body, filename, err := rangeOf(c).Containd().DownloadPcapFile(c.Request.Context(), name)
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

// validPcapName accepts only containd PCAP basenames that are safe to pass
// as a single download path segment.
func validPcapName(name string) bool {
	if !validPcapComponent(name, 255) {
		return false
	}
	return strings.HasSuffix(name, ".pcap") || strings.HasSuffix(name, ".pcapng")
}

// validPcapComponent checks the shared filename character class and bound.
func validPcapComponent(name string, maxLen int) bool {
	if len(name) == 0 || len(name) > maxLen || name == "." || strings.Contains(name, "..") {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '.' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func (s *Server) handlePcapList(c *gin.Context) {
	files, err := rangeOf(c).Containd().ListPcapFiles(c.Request.Context())
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
	firewall, err := firewallContainer(rangeOf(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
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

	gen := rangeOf(c)
	finish := func(fileReady bool) {
		gen.Commit(func() {
			s.pcapMu.Lock()
			s.pcap.Capturing = false
			s.pcap.FileReady = fileReady
			s.pcapMu.Unlock()
		})
	}
	if !gen.Go("pcap-tcpdump", func(ctx context.Context) {
		execCfg := container.ExecOptions{
			Cmd:          []string{"sh", "-c", cmd},
			AttachStdout: false,
			AttachStderr: false,
			Privileged:   true,
		}
		execID, err := dockerCli.ContainerExecCreate(ctx, firewall, execCfg)
		if err != nil {
			log.Printf("[pcap] exec create failed: %v", err)
			finish(false)
			return
		}
		if err := dockerCli.ContainerExecStart(ctx, execID.ID, container.ExecStartOptions{}); err != nil {
			log.Printf("[pcap] exec start failed: %v", err)
			finish(false)
			return
		}
		for {
			inspect, err := dockerCli.ContainerExecInspect(ctx, execID.ID)
			if err != nil || !inspect.Running || !sleepCtx(ctx, time.Second) {
				break
			}
		}
		finish(true)
		log.Println("[pcap] fallback capture complete")
	}) {
		rangeUnavailable(c)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":       "capturing",
		"duration_sec": durationSec,
		"mode":         "fallback",
	})
}

func (s *Server) stopTcpdumpFallback(c *gin.Context) {
	firewall, err := firewallContainer(rangeOf(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
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
	execID, err := dockerCli.ContainerExecCreate(ctx, firewall, execCfg)
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
	firewall, err := firewallContainer(rangeOf(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	dockerCli := s.orchestrator.DockerClient()
	if dockerCli == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "docker client not available"})
		return
	}

	ctx := c.Request.Context()
	reader, _, err := dockerCli.CopyFromContainer(ctx, firewall, "/tmp/capture.pcap")
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
	gen := rangeOf(c)
	targets, err := resolveTraffic(gen)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
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

	if !gen.Go("traffic", func(ctx context.Context) { s.runTrafficGeneration(ctx, gen, targets, req.DurationSec) }) {
		rangeUnavailable(c)
		return
	}

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
//
// It is a generation worker: the range stopping ends it, and its counts
// are dropped once the generation is gone.
func (s *Server) runTrafficGeneration(ctx context.Context, gen *lifecycle.Generation, targets []resolvedTraffic, durationSec int) {
	deadline := time.Now().Add(time.Duration(durationSec) * time.Second)
	flows := 0

	record := func(generating bool) {
		gen.Commit(func() {
			s.trafficMu.Lock()
			s.traffic.Generating = generating
			s.traffic.FlowsGenerated = flows
			s.trafficMu.Unlock()
		})
	}
	dockerCli := s.orchestrator.DockerClient()
	if dockerCli == nil {
		log.Println("[traffic] docker client not available")
		record(false)
		return
	}

	for time.Now().Before(deadline) && ctx.Err() == nil {
		for _, t := range targets {
			if time.Now().After(deadline) || ctx.Err() != nil {
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
			record(true)
			sleepCtx(ctx, 200*time.Millisecond)
		}
		// Brief pause between polling cycles — mimics 1-sec SCADA scan rate
		if time.Now().Before(deadline) {
			sleepCtx(ctx, 500*time.Millisecond)
		}
	}

	record(false)
	log.Printf("[traffic] generation complete: %d flows generated", flows)
}
