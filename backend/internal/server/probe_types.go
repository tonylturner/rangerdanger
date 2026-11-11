package server

import "github.com/tturner/rangerdanger/backend/internal/labs"

// validationProbe is one row of the segmentation validation matrix,
// resolved against the range (resolveProof).
type validationProbe struct {
	Route    labs.PlanRoute
	Decision labs.Decision
	Src      string // container name
	SrcLabel string // friendly source name for display
	Dst      string
	Port     string
	Expected string // "allow" | "deny"
	Note     string
	Category string // "authorized" | "unauthorized"
	ID       string
}

type validationRow struct {
	validationProbe
	Actual     string // "allow" | "deny" | "reset" | "skipped"
	Pass       bool
	ProbeError string
}
