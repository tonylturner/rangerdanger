package server

import (
	"archive/tar"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/gin-gonic/gin"
	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

// ---------- PCAP capture (containd API, or package-specific tcpdump) ----------

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

	// EU needs an OR of disjoint ports, not the single port/range supported
	// by containd's structured filter. Preserve its tcpdump capture.
	gen := rangeOf(c)
	if recipe, err := recipeFor(gen); err == nil && recipe.captureFilter != "" {
		s.startTcpdumpFallback(c, req.DurationSec, nil, recipe.captureFilter)
		return
	}

	// The authored topology carries containd zone names. Manifest zone
	// labels identify teaching networks, not necessarily appliance zones.
	zones := captureZones(gen)
	if len(zones) == 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "package has no capture zones"})
		return
	}
	cfg := containd.PcapConfig{
		Enabled:       true,
		Zones:         zones,
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

	// Serialize native starts and stops: a duration worker must not stop a
	// replacement capture after the user has stopped the previous one.
	s.pcapMu.Lock()
	if s.pcap.Capturing {
		s.pcapMu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"error": "capture already in progress"})
		return
	}
	// Start-only overrides are ephemeral: never stage or commit capture settings.
	status, err := gen.Containd().StartPcap(c.Request.Context(), &cfg)
	if err != nil {
		s.pcapMu.Unlock()
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}
	if len(status.Interfaces) == 0 {
		s.pcapMu.Unlock()
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "containd capture did not start", "capture": status})
		return
	}
	// containd's start acknowledgement can precede Running and StartedAt.
	// Use our acceptance time to identify this session, even with a reused prefix.
	startedAt := time.Now().UTC()
	s.pcap = pcapState{
		Capturing:   true,
		DurationSec: req.DurationSec,
		StartedAt:   startedAt.Format(time.RFC3339Nano),
		FileReady:   false,
		FilePrefix:  prefix,
	}
	capture := s.pcap
	s.pcapMu.Unlock()

	// Rotation is not a duration limit, especially on idle interfaces.
	if !gen.Go("pcap-poll", func(ctx context.Context) {
		s.pollPcapCompletion(ctx, gen, capture, startedAt.Add(time.Duration(req.DurationSec)*time.Second))
	}) {
		rangeUnavailable(c)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":       "capturing",
		"duration_sec": req.DurationSec,
		"file_prefix":  prefix,
		"interfaces":   status.Interfaces,
	})
}

func captureZones(gen *lifecycle.Generation) []string {
	seen := map[string]bool{}
	for _, network := range gen.Package.Template.Networks {
		if network.Zone != "" {
			seen[network.Zone] = true
		}
	}
	zones := make([]string, 0, len(seen))
	for zone := range seen {
		zones = append(zones, zone)
	}
	sort.Strings(zones)
	return zones
}

// pollPcapCompletion owns the native capture duration. A user stop makes this
// worker inert; a result after the range stopped is dropped.
func (s *Server) pollPcapCompletion(ctx context.Context, gen *lifecycle.Generation, capture pcapState, deadline time.Time) {
	if !sleepCtx(ctx, time.Until(deadline)) {
		return
	}
	s.pcapMu.Lock()
	if !s.pcap.Capturing || !samePcapCapture(s.pcap, capture) {
		s.pcapMu.Unlock()
		return
	}
	status, err := gen.Containd().StopPcap(ctx)
	s.pcap.Capturing = false
	s.pcapMu.Unlock()
	if err != nil {
		log.Printf("[pcap] duration stop failed (prefix=%s): %v", capture.FilePrefix, err)
	}
	s.finishNativePcap(ctx, gen, capture, status)
}

func samePcapCapture(a, b pcapState) bool {
	return !a.Fallback && a.FilePrefix == b.FilePrefix && a.StartedAt == b.StartedAt
}

// finishNativePcap bounds engine shutdown independently of the requested
// duration, then publishes whatever files are available, even on timeout.
func (s *Server) finishNativePcap(ctx context.Context, gen *lifecycle.Generation, capture pcapState, status *containd.PcapStatus) []string {
	waitCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	for status == nil || status.Running {
		var err error
		status, err = gen.Containd().GetPcapStatus(waitCtx)
		if err == nil && !status.Running {
			break
		}
		if !sleepCtx(waitCtx, 2*time.Second) {
			if ctx.Err() != nil {
				return nil
			}
			log.Printf("[pcap] shutdown not confirmed within 15s (prefix=%s)", capture.FilePrefix)
			break
		}
	}
	if ctx.Err() != nil {
		return nil
	}
	// Use the generation/request context, not the expired shutdown wait.
	files, err := gen.Containd().ListPcapFiles(ctx)
	var matchedNames []string
	if err == nil {
		for _, f := range files {
			if strings.HasPrefix(f.Name, capture.FilePrefix) {
				matchedNames = append(matchedNames, f.Name)
			}
		}
	} else {
		log.Printf("[pcap] list files failed (prefix=%s): %v", capture.FilePrefix, err)
	}

	if !gen.Commit(func() {
		s.pcapMu.Lock()
		if samePcapCapture(s.pcap, capture) {
			s.pcap.Capturing = false
			s.pcap.FileReady = len(matchedNames) > 0
			s.pcap.Files = matchedNames
		}
		s.pcapMu.Unlock()
	}) {
		return nil
	}

	if len(matchedNames) > 0 {
		log.Printf("[pcap] containd capture complete: %d files (prefix=%s)", len(matchedNames), capture.FilePrefix)
	} else {
		log.Printf("[pcap] containd capture complete but no files found for prefix=%s", capture.FilePrefix)
	}
	return matchedNames
}

func (s *Server) handlePcapStop(c *gin.Context) {
	s.pcapMu.Lock()
	capture := s.pcap

	if capture.Fallback {
		s.pcapMu.Unlock()
		s.stopTcpdumpFallback(c)
		return
	}

	status, err := rangeOf(c).Containd().StopPcap(c.Request.Context())
	if err != nil {
		s.pcapMu.Unlock()
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("stop pcap: %v", err)})
		return
	}
	s.pcap.Capturing = false
	s.pcapMu.Unlock()
	matchedNames := s.finishNativePcap(c.Request.Context(), rangeOf(c), capture, status)

	c.JSON(http.StatusOK, gin.H{"status": "stopped", "files": matchedNames})
}

func (s *Server) handlePcapStatus(c *gin.Context) {
	s.pcapMu.Lock()
	state := s.pcap
	s.pcapMu.Unlock()

	// containd supplies metadata, but our worker owns the capture lifecycle:
	// a stale Running bit must not undo an accepted start or a bounded finish.
	if !state.Fallback && state.FilePrefix != "" {
		status, err := rangeOf(c).Containd().GetPcapStatus(c.Request.Context())
		if err == nil {
			c.JSON(http.StatusOK, gin.H{
				"capturing":    state.Capturing,
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

// ---------- Package-specific capture: Docker exec tcpdump (EU) ----------

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
