// Package version exposes build metadata about the running RangerDanger
// backend. Values are populated at link time via -ldflags by the build
// pipeline (Dockerfile.backend, CI release workflow).
package version

var (
	// Version is the semver tag this binary was built from, e.g. "v0.1.0".
	// Falls back to "dev" for local untagged builds.
	Version = "dev"

	// Commit is the short git SHA of the build.
	Commit = "none"

	// Date is the UTC build timestamp in RFC3339 format.
	Date = "unknown"
)

// Info bundles the build metadata for serialisation.
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// Get returns the current build info.
func Get() Info {
	return Info{Version: Version, Commit: Commit, Date: Date}
}
