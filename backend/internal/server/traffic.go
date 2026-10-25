package server

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

type trafficRun struct {
	id        string
	startedAt string
	cancel    context.CancelFunc
}

type trafficExecutor func(context.Context, resolvedTraffic) error

var (
	errTrafficBusy         = errors.New("traffic generation already in progress")
	errTrafficRangeStopped = errors.New("range stopped")
)

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
	recipe, ok := supportedRecipe(c, func(r *packageRecipe) bool { return r.traffic != nil })
	if !ok {
		return
	}
	dockerCli := s.orchestrator.DockerClient()
	if dockerCli == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "docker client not available"})
		return
	}
	gen := rangeOf(c)
	targets, err := recipe.resolveTraffic(gen)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	run, err := s.startTrafficRun(gen, targets, req.DurationSec, func(ctx context.Context, target resolvedTraffic) error {
		return executeTraffic(ctx, dockerCli, target)
	})
	if errors.Is(err, errTrafficBusy) {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if err != nil {
		rangeUnavailable(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status": "generating", "duration_sec": req.DurationSec,
		"run_id": run.id, "started_at": run.startedAt,
	})
}

func (s *Server) startTrafficRun(gen *lifecycle.Generation, targets []resolvedTraffic, durationSec int, exec trafficExecutor) (*trafficRun, error) {
	s.trafficMu.Lock()
	defer s.trafficMu.Unlock()
	if s.traffic.Generating {
		return nil, errTrafficBusy
	}
	ctx, cancel := context.WithTimeout(gen.Context(), time.Duration(durationSec)*time.Second)
	run := &trafficRun{id: rand.Text(), startedAt: time.Now().UTC().Format(time.RFC3339), cancel: cancel}
	previous := s.traffic
	s.traffic = trafficState{
		Generating: true, RunID: run.id, DurationSec: durationSec,
		StartedAt: run.startedAt, run: run,
	}
	if !gen.Go("traffic", func(context.Context) {
		defer cancel()
		// Only the returning worker clears generating. A cancelled run
		// keeps the start gate closed while its in-flight exec drains.
		defer func() {
			s.trafficMu.Lock()
			defer s.trafficMu.Unlock()
			if s.traffic.run == run {
				s.traffic.Generating = false
				s.traffic.run = nil
			}
		}()
		s.runTrafficGeneration(ctx, gen, run, targets, exec)
	}) {
		cancel()
		s.traffic = previous
		return nil, errTrafficRangeStopped
	}
	return run, nil
}

// A stop names the run observed by the caller. Missing, completed and stale
// IDs are idempotent no-ops, never authority to cancel a newer run.
func (s *Server) handleTrafficStop(c *gin.Context) {
	var req struct {
		RunID string `json:"run_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid traffic stop request"})
		return
	}
	s.trafficMu.Lock()
	run := s.traffic.run
	s.trafficMu.Unlock()
	status := "stopped"
	if run != nil && run.id == req.RunID {
		// This captured run's cancel function remains safe even if it
		// finishes and another run starts before this call executes.
		run.cancel()
		status = "stopping"
	}
	c.JSON(http.StatusOK, gin.H{"status": status})
}

func (s *Server) handleTrafficStatus(c *gin.Context) {
	s.trafficMu.Lock()
	state := s.traffic
	s.trafficMu.Unlock()
	c.JSON(http.StatusOK, gin.H{
		"generating": state.Generating, "started_at": state.StartedAt,
		"flows_generated": state.FlowsGenerated, "run_id": state.RunID,
	})
}

// Only recipe-declared scenario traffic is generated here. The autonomous
// simulator baseline is independent and is not stopped by Traffic Matrix.
func (s *Server) runTrafficGeneration(ctx context.Context, gen *lifecycle.Generation, run *trafficRun, targets []resolvedTraffic, exec trafficExecutor) {
	flows := 0
	for ctx.Err() == nil {
		for _, target := range targets {
			if ctx.Err() != nil {
				return
			}
			if err := exec(ctx, target); err != nil {
				if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
					log.Printf("[traffic] exec %s: %v", target.desc, err)
				}
				if ctx.Err() != nil {
					return
				}
				continue
			}
			flows++
			gen.Commit(func() {
				s.trafficMu.Lock()
				defer s.trafficMu.Unlock()
				if s.traffic.run == run {
					s.traffic.FlowsGenerated = flows
				}
			})
			if !sleepCtx(ctx, 200*time.Millisecond) {
				return
			}
		}
		if !sleepCtx(ctx, 500*time.Millisecond) {
			return
		}
	}
}
