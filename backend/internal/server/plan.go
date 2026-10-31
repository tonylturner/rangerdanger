package server

import (
	"encoding/json"
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
	var req struct {
		PackageID string   `json:"packageId"`
		Revision  int      `json:"revision"`
		ActionIDs []string `json:"actionIds"`
	}
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
	if req.PackageID != pkg.ID || req.Revision != pkg.Revision {
		c.JSON(http.StatusConflict, gin.H{"error": "plan package or revision differs from the active range"})
		return nil, false
	}
	known := map[string]bool{}
	if pkg.Curriculum != nil {
		for _, action := range pkg.Curriculum.Catalog {
			known[action.ID] = true
		}
	}
	for _, id := range req.ActionIDs {
		if !known[id] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unknown plan action " + id})
			return nil, false
		}
	}
	compiled, err := labs.CompilePlan(pkg, pkg.Curriculum, req.ActionIDs)
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
		if expectation.Labs == nil {
			expectation.Labs = []string{}
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
	fail := func(err error) {
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
	importedWarnings, err := gen.Containd().ImportConfig(c.Request.Context(), plan.Config)
	warnings = append(warnings, importedWarnings...)
	if err != nil {
		fail(err)
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
		rangeUnavailable(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"applied": true, "warnings": warnings, "plan": plan})
}

// judgingExpectations uses the applied snapshot only while its provenance holds.
// Named and manual policies are judged against Hardened, never guessed from labels.
func (s *Server) judgingExpectations(gen *lifecycle.Generation) ([]labs.PlanExpectation, error) {
	s.activeConfigMu.RLock()
	if s.policySource == "plan" && s.planProvenance != nil && s.appliedPlan != nil {
		expectations := s.appliedPlan.Expectations
		s.activeConfigMu.RUnlock()
		return expectations, nil
	}
	s.activeConfigMu.RUnlock()
	compiled, err := labs.CompilePlan(gen.Package, gen.Package.Curriculum, nil)
	if err != nil {
		return nil, err
	}
	for i := range compiled.Expectations {
		compiled.Expectations[i].PlanVerdict = compiled.Expectations[i].HardenedVerdict
	}
	return compiled.Expectations, nil
}
