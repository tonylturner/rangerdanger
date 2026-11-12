package server

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

type probeMeasurement struct {
	ID       string `json:"id"`
	Observed string `json:"observed"`
	Error    string `json:"error"`
	RuleID   string `json:"ruleId,omitempty"`
}

type probeMatrixResult struct {
	PolicyHash      string             `json:"policyHash"`
	PolicySource    string             `json:"policySource"`
	PolicyStable    bool               `json:"policyStable"`
	PolicyChangedAt time.Time          `json:"policyChangedAt"`
	StartedAt       time.Time          `json:"startedAt"`
	FinishedAt      time.Time          `json:"finishedAt"`
	Rows            []probeMeasurement `json:"rows"`
}

// handleProbeMatrix is automation-only by intended use, not authorization.
// It measures the server-owned proof universe without changing policy or
// capturing, rendering or retaining student evidence.
func (s *Server) handleProbeMatrix(c *gin.Context) {
	if rangeOf(c).Package.SegmentationPlan() == nil {
		notSupported(c)
		return
	}
	if _, ok := supportedRecipe(c, func(r *packageRecipe) bool { return r.testSuite }); !ok {
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 1))
	if err != nil || len(body) != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "probe matrix takes no request body"})
		return
	}
	if s.orchestrator == nil || s.orchestrator.DockerClient() == nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "docker client not available"})
		return
	}
	cli := s.orchestrator.DockerClient()
	result, err := s.measureProbeMatrix(c.Request.Context(), rangeOf(c), func(ctx context.Context, p validationProbe) validationRow {
		return s.strictTCPProbe(ctx, cli, p)
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}

func (s *Server) measureProbeMatrix(ctx context.Context, gen *lifecycle.Generation, probe euProbeFunc) (probeMatrixResult, error) {
	s.activeConfigMu.RLock()
	source, wantHash, policyAt := s.policySource, s.lastAppliedHash, s.policyChangedAt
	provenance, plan := s.planProvenance, s.appliedPlan
	s.activeConfigMu.RUnlock()
	result := probeMatrixResult{PolicySource: source, PolicyChangedAt: policyAt, StartedAt: time.Now().UTC(), Rows: []probeMeasurement{}}
	expectations, err := s.judgingExpectations(gen)
	if err != nil {
		return result, err
	}
	matrix, err := resolveProof(gen, expectations, "", true)
	if err != nil {
		return result, err
	}
	policy, hash, hashErr := readRunningFirewall(ctx, gen, wantHash)
	if hashErr == nil {
		attributeProbes(matrix, policy)
	}
	result.PolicyHash = hash
	measure := probe
	if hashErr != nil {
		measure = func(_ context.Context, p validationProbe) validationRow {
			return validationRow{validationProbe: p, Actual: "skipped", ProbeError: hashErr.Error()}
		}
	}
	rows := runEUProbeMatrix(ctx, matrix, measure)
	for _, row := range rows {
		observed := row.Actual
		if observed != "allow" && observed != "deny" && observed != "reset" {
			observed = "untested"
		}
		result.Rows = append(result.Rows, probeMeasurement{ID: row.ID, Observed: observed, Error: row.ProbeError, RuleID: row.Decision.RuleID})
	}
	finalHash, finalErr := gen.Containd().GetFirewallHash(ctx)
	result.FinishedAt = time.Now().UTC()
	s.activeConfigMu.RLock()
	result.PolicyStable = ctx.Err() == nil && gen.Context().Err() == nil && hashErr == nil && finalErr == nil && hash != "" &&
		hash == finalHash && hash == wantHash && s.lastAppliedHash == wantHash &&
		s.policyChangedAt.Equal(policyAt) && s.policySource == source && s.planProvenance == provenance && s.appliedPlan == plan &&
		!policyAt.IsZero() && !policyAt.Before(gen.StartedAt) && !policyAt.After(result.StartedAt)
	if source == "plan" {
		result.PolicyStable = result.PolicyStable && provenance != nil && plan != nil &&
			provenance.PolicyHash == hash && provenance.PackageID == gen.Package.ID && provenance.Revision == gen.Package.Revision
	}
	s.activeConfigMu.RUnlock()
	return result, nil
}
