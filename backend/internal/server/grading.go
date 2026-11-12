package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

var errPlanScope = errors.New("plan package or revision differs from the active range")

type planSelection struct {
	PackageID string   `json:"packageId"`
	Revision  int      `json:"revision"`
	ActionIDs []string `json:"actionIds"`
}

// validatePlanRequest is shared by applying a plan and grading a request.
// Neither path accepts caller-authored verdicts or firewall tuples.
func validatePlanRequest(pkg labs.Package, req planSelection) error {
	if req.PackageID != pkg.ID || req.Revision != pkg.Revision {
		return errPlanScope
	}
	known := map[string]bool{}
	if pkg.SegmentationPlan() != nil {
		for _, action := range pkg.SegmentationPlan().Catalog {
			known[action.ID] = true
		}
	}
	for _, id := range req.ActionIDs {
		if !known[id] {
			return fmt.Errorf("unknown plan action %s", id)
		}
	}
	return nil
}

func compileRequestedPlan(pkg labs.Package, req planSelection) (labs.CompiledPlan, error) {
	if err := validatePlanRequest(pkg, req); err != nil {
		return labs.CompiledPlan{}, err
	}
	return labs.CompilePlan(pkg, pkg.SegmentationPlan(), req.ActionIDs)
}

type changeRequestKey struct{}
type compiledChangeRequest struct {
	plan labs.CompiledPlan
}

// gradingContext bounds and validates even requests that an applied plan will
// supersede. Bad input must never silently become full-reference grading.
func gradingContext(c *gin.Context) (context.Context, bool) {
	ctx := c.Request.Context()
	var body struct {
		ChangeRequest *planSelection `json:"changeRequest"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&body)
	if err == io.EOF {
		return ctx, true // automation may POST with no body
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid grading request: " + err.Error()})
		return ctx, false
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		c.JSON(http.StatusBadRequest, gin.H{"error": "expected one JSON document"})
		return ctx, false
	}
	if body.ChangeRequest == nil {
		return ctx, true
	}
	compiled, err := compileRequestedPlan(rangeOf(c).Package, *body.ChangeRequest)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return ctx, false
	}
	return context.WithValue(ctx, changeRequestKey{}, compiledChangeRequest{compiled}), true
}

type gradingTarget struct {
	plan   labs.CompiledPlan
	detail string
}

// judgingTarget is the sole selection authority. Requests live only in the
// interactive HTTP context; automation supplies none.
func (s *Server) judgingTarget(ctx context.Context, gen *lifecycle.Generation) (gradingTarget, error) {
	s.activeConfigMu.RLock()
	source := s.policySource
	if source == "plan" && s.planProvenance != nil && s.appliedPlan != nil {
		plan := *s.appliedPlan
		s.activeConfigMu.RUnlock()
		return gradingTarget{plan, "applied plan"}, nil
	}
	s.activeConfigMu.RUnlock()
	if req, ok := ctx.Value(changeRequestKey{}).(compiledChangeRequest); ok && source != "plan" {
		count := 0
		for _, action := range req.plan.Actions {
			if action.Chosen {
				count++
			}
		}
		return gradingTarget{req.plan, fmt.Sprintf("your %s change request (%d actions)", planningLabel(gen.Package), count)}, nil
	}
	// All selected actions also supply the reference config used by the
	// retained-command audit; verdicts explicitly remain Hardened's.
	var ids []string
	if gen.Package.SegmentationPlan() != nil {
		for _, action := range gen.Package.SegmentationPlan().Catalog {
			ids = append(ids, action.ID)
		}
	}
	plan, err := labs.CompilePlan(gen.Package, gen.Package.SegmentationPlan(), ids)
	for i := range plan.Expectations {
		plan.Expectations[i].PlanVerdict = plan.Expectations[i].HardenedVerdict
	}
	return gradingTarget{plan, "full reference (no change request saved)"}, err
}

func planningLabel(pkg labs.Package) string {
	a, _ := pkg.Course.Activity(pkg.Course.Features.SegmentationPlan.Activities.Planning)
	return a.Label
}
