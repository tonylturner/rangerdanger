package server

// validationProbe is one row of the segmentation validation matrix,
// resolved against the range (resolveProof).
type validationProbe struct {
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
	Actual     string // "allow" | "deny" | "skipped"
	Pass       bool
	ProbeError string
}
