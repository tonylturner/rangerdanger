package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

// planProvenance identifies an immutable applied compilation, not a policy label.
type planProvenance struct {
	Label      string    `json:"label"`
	ActionIDs  []string  `json:"actionIds"`
	PackageID  string    `json:"packageId"`
	Revision   int       `json:"revision"`
	PolicyHash string    `json:"policyHash"`
	AppliedAt  time.Time `json:"appliedAt"`
}

type planExpectation struct {
	labs.PlanExpectation
	FromIP string `json:"fromIp"`
	ToIP   string `json:"toIp"`
}

type planResponse struct {
	labs.CompiledPlan
	PackageID    string            `json:"packageId"`
	Revision     int               `json:"revision"`
	PolicyHash   string            `json:"policyHash"`
	Expectations []planExpectation `json:"expectations"`
}

func (s *Server) compilePlanRequest(c *gin.Context, failureStatus int) (*planResponse, bool) {
	if rangeOf(c).Package.SegmentationPlan() == nil {
		notSupported(c)
		return nil, false
	}
	var req planSelection
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid plan request: " + err.Error()})
		return nil, false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expected one JSON document"})
		return nil, false
	}
	pkg := rangeOf(c).Package
	if err := validatePlanRequest(pkg, req); err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errPlanScope) {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return nil, false
	}
	compiled, err := labs.CompilePlan(pkg, pkg.SegmentationPlan(), req.ActionIDs)
	if err != nil {
		c.JSON(failureStatus, gin.H{"error": err.Error(), "warnings": []string{}})
		return nil, false
	}
	hash, err := containd.FirewallHashFromBytes(compiled.Config)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return nil, false
	}
	response := &planResponse{CompiledPlan: compiled, PackageID: pkg.ID, Revision: pkg.Revision, PolicyHash: hash, Expectations: []planExpectation{}}
	for _, expectation := range compiled.Expectations {
		route, err := labs.ResolvePlanTuple(pkg, labs.PlanProof{ID: expectation.ID, From: expectation.From, To: expectation.To, Network: expectation.Network, Port: expectation.Port})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return nil, false
		}
		if expectation.Activities == nil {
			expectation.Activities = []string{}
		}
		if expectation.Owners == nil {
			expectation.Owners = []string{}
		}
		response.Expectations = append(response.Expectations, planExpectation{expectation, route.Tuple.SrcIP, route.Tuple.DstIP})
	}
	return response, true
}

func (s *Server) handlePlanCompile(c *gin.Context) {
	if plan, ok := s.compilePlanRequest(c, http.StatusInternalServerError); ok {
		c.JSON(http.StatusOK, plan)
	}
}

func (s *Server) handleFirewallApplyPlan(c *gin.Context) {
	plan, ok := s.compilePlanRequest(c, http.StatusUnprocessableEntity)
	if !ok {
		return
	}
	gen := rangeOf(c)
	warnings := []string{}
	var applied *containd.ApplyResult
	fail := func(err error) {
		if applied != nil {
			writeApplyError(c, err, applied, gin.H{"warnings": warnings})
			return
		}
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error(), "warnings": warnings})
	}
	if err := labs.LintPolicy("compiled plan", plan.Config, gen.Package.ICSRules); err != nil {
		fail(err)
		return
	}
	if err := labs.CheckManagementSafety(plan.Config); err != nil {
		fail(err)
		return
	}
	s.policyApplyMu.Lock()
	defer s.policyApplyMu.Unlock()
	started := time.Now().UTC()
	applied, err := gen.Containd().ImportConfig(c.Request.Context(), plan.Config)
	if applied != nil {
		warnings = append(warnings, applied.Warnings...)
	}
	if err != nil {
		writeApplyError(c, err, applied, gin.H{"warnings": warnings})
		return
	}
	hash, err := gen.Containd().GetFirewallHash(c.Request.Context())
	if err != nil {
		fail(err)
		return
	}
	if hash != plan.PolicyHash {
		fail(fmt.Errorf("running firewall hash does not match compiled plan"))
		return
	}
	actions := []string{}
	for _, action := range plan.Actions {
		if action.Chosen {
			actions = append(actions, action.ID)
		}
	}
	if !gen.Commit(func() {
		s.activeConfigMu.Lock()
		defer s.activeConfigMu.Unlock()
		s.activeConfig, s.policySource = "custom", "plan"
		s.recordApplyLocked(plan.Config)
		s.policyEvidenceSince = started
		s.planProvenance = &planProvenance{plan.Label, actions, plan.PackageID, plan.Revision, plan.PolicyHash, time.Now().UTC()}
		snapshot := plan.CompiledPlan
		s.appliedPlan = &snapshot
	}) {
		writeApplyError(c, errRangeStopped, applied, gin.H{"warnings": warnings})
		return
	}
	c.JSON(http.StatusOK, gin.H{"applied": true, "warnings": warnings, "plan": plan, "containd": applied.Receipt})
}

// judgingExpectations is the automation entry point: no student request.
func (s *Server) judgingExpectations(gen *lifecycle.Generation) ([]labs.PlanExpectation, error) {
	target, err := s.judgingTarget(context.Background(), gen)
	return target.plan.Expectations, err
}
