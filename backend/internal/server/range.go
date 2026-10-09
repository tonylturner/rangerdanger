package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

// rangeLifecycle is what the server needs from the range lifecycle;
// *lifecycle.Manager implements it.
type rangeLifecycle interface {
	Start()
	Status() lifecycle.Status
	ActivePackage() string
	Request(packageID string) (lifecycle.Status, error)
	Enter(name string) (*lifecycle.Generation, func(), lifecycle.Status, bool)
}

var _ rangeLifecycle = (*lifecycle.Manager)(nil)

// generationKey holds the request's generation in the gin context.
const generationKey = "rangerdanger.generation"

// rangeBound admits a request only while a range generation serves, binds
// it to that generation, and gives the handler a context that ends with
// either the request or the generation. The generation's stopping barrier
// waits for the handler to return.
func (s *Server) rangeBound() gin.HandlerFunc {
	return func(c *gin.Context) {
		gen, release, status, ok := s.rng.Enter(c.Request.Method + " " + c.FullPath())
		if !ok {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "range not ready", "phase": status.Phase})
			return
		}
		defer release()
		ctx, cancel := context.WithCancel(c.Request.Context())
		defer cancel()
		stop := context.AfterFunc(gen.Context(), cancel)
		defer stop()
		c.Request = c.Request.WithContext(ctx)
		c.Set(generationKey, gen)
		c.Next()
	}
}

// rangeOf is the generation a range-bound request runs against.
func rangeOf(c *gin.Context) *lifecycle.Generation {
	return c.MustGet(generationKey).(*lifecycle.Generation)
}

// activateRange resets the per-range state for a new generation and starts
// its background workers. The lifecycle calls it before the generation
// serves requests.
func (s *Server) activateRange(gen *lifecycle.Generation) {
	// The range starts on the package's default policy, which every range
	// start imports.
	activeConfig := "weak"
	if strings.Contains(gen.Package.FirewallConfigPath, "improved") {
		activeConfig = "improved"
	}
	s.activeConfigMu.Lock()
	s.activeConfig = activeConfig
	s.policySource = ""
	s.lastAppliedHash = ""
	s.lastAppliedAt = time.Time{}
	s.activeConfigMu.Unlock()
	s.pcapMu.Lock()
	s.pcap = pcapState{}
	s.pcapMu.Unlock()
	s.trafficMu.Lock()
	s.traffic = trafficState{}
	s.trafficMu.Unlock()

	gen.Go("policy-observer", func(ctx context.Context) { s.policyObserverLoop(ctx, gen) })
}

// errRangeStopped reports work whose generation stopped before it could
// publish its result.
var errRangeStopped = errors.New("range stopped before the change was recorded")

// rangeUnavailable answers a range-bound request whose generation began
// stopping while it ran.
func rangeUnavailable(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{"error": "range is stopping"})
}

func (s *Server) handleGetRange(c *gin.Context) {
	c.JSON(http.StatusOK, s.rng.Status())
}

// maxRangeRequestBody bounds POST /api/range bodies.
const maxRangeRequestBody = 4 << 10

func (s *Server) handlePostRange(c *gin.Context) {
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxRangeRequestBody))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request body too large or unreadable"})
		return
	}
	var req struct {
		Package string `json:"package"`
	}
	if len(bytes.TrimSpace(body)) > 0 {
		decoder := json.NewDecoder(bytes.NewReader(body))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body: " + err.Error()})
			return
		}
	}

	status, err := s.rng.Request(req.Package)
	switch {
	case errors.Is(err, lifecycle.ErrBusy):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "phase": status.Phase})
	case errors.Is(err, lifecycle.ErrUnknownPackage):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	case err != nil:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
	default:
		c.JSON(http.StatusAccepted, status)
	}
}
