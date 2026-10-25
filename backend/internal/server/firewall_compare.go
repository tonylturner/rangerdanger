package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

// PolicyRuleDiff shows a single rule change between two configs.
type PolicyRuleDiff struct {
	ZonePair       string `json:"zone_pair"`     // e.g. "enterprise → field"
	WeakRule       string `json:"weak_rule"`     // description from weak config (empty if not present)
	ImprovedRule   string `json:"improved_rule"` // description from improved config (empty if not present)
	WeakAction     string `json:"weak_action"`
	ImprovedAction string `json:"improved_action"`
	Change         string `json:"change"` // "tightened", "added", "removed", "unchanged"
}

// PolicyComparison is the full comparison response.
type PolicyComparison struct {
	WeakConfig     string           `json:"weak_config"`
	ImprovedConfig string           `json:"improved_config"`
	Diffs          []PolicyRuleDiff `json:"diffs"`
	Summary        string           `json:"summary"`
}

// handleFirewallCompare returns a structured comparison of the two named
// policies in a package's comparison recipe.
func (s *Server) handleFirewallCompare(c *gin.Context) {
	recipe, ok := supportedRecipe(c, func(r *packageRecipe) bool { return r.compare != nil })
	if !ok {
		return
	}
	pkg := rangeOf(c).Package
	weakPath, err := s.policyPath(pkg, recipe.compare.weakPolicy)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	improvedPath, err := s.policyPath(pkg, recipe.compare.improvedPolicy)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	weakRules, err := loadFirewallRules(weakPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load " + recipe.compare.weakPolicy + " config: " + err.Error()})
		return
	}

	improvedRules, err := loadFirewallRules(improvedPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load " + recipe.compare.improvedPolicy + " config: " + err.Error()})
		return
	}

	// Build diff by zone pair
	diffs := compareRuleSets(recipe.compare, weakRules, improvedRules)

	tightened := 0
	added := 0
	for _, d := range diffs {
		switch d.Change {
		case "tightened":
			tightened++
		case "added":
			added++
		}
	}

	comparison := PolicyComparison{
		WeakConfig:     filepath.Base(weakPath),
		ImprovedConfig: filepath.Base(improvedPath),
		Diffs:          diffs,
		Summary: func() string {
			parts := []string{}
			if tightened > 0 {
				parts = append(parts, fmt.Sprintf("%d rule(s) tightened", tightened))
			}
			if added > 0 {
				parts = append(parts, "new deny rules added")
			}
			if len(parts) == 0 {
				return "No differences found"
			}
			return strings.Join(parts, ", ")
		}(),
	}

	c.JSON(http.StatusOK, comparison)
}

// handleFirewallActive returns the currently active firewall config name
// and how it was applied. policy_source lets the frontend label the
// status banner accurately (e.g. "Your custom policy (Lab 1.4 plan)" vs
// "(your containd commit)").
func (s *Server) handleFirewallActive(c *gin.Context) {
	s.activeConfigMu.RLock()
	active := s.activeConfig
	source := s.policySource
	plan := s.planProvenance
	s.activeConfigMu.RUnlock()

	response := gin.H{"active_config": active, "policy_source": source}
	if source == "plan" {
		response["plan"] = plan
	}
	c.JSON(http.StatusOK, response)
}

// handleFirewallApply applies a named firewall config to the live containd instance.
func (s *Server) handleFirewallApply(c *gin.Context) {
	var req struct {
		Config string `json:"config"` // "weak" or "improved"
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	warnings, err := s.applyFirewallConfigInternal(c.Request.Context(), rangeOf(c), req.Config)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": err.Error()})
		return
	}

	resp := gin.H{
		"status":        "applied",
		"active_config": req.Config,
	}
	if len(warnings) > 0 {
		resp["warnings"] = warnings
	}
	c.JSON(http.StatusOK, resp)
}

// readPolicyJSONWithRetry reads a policy JSON file from the lab-definitions
// bind mount, retrying briefly if json.Valid fails. On macOS Docker Desktop,
// host writes to a bind-mounted file aren't atomic in the container's view:
// the FUSE/VirtIOFS layer briefly exposes a truncated mid-write file. Most
// often this happens when the host process writes the file while the
// container reads it concurrently (e.g. a config tweak then immediate apply).
// Three reads at 100ms intervals is enough to ride out the window in
// practice; if it's still invalid after that, the error message includes
// the byte count + a head/tail snippet so an actual JSON-syntax mistake
// is distinguishable from a transient read.
func readPolicyJSONWithRetry(path string) ([]byte, error) {
	const attempts = 3
	const backoff = 100 * time.Millisecond
	var lastData []byte
	for i := 0; i < attempts; i++ {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read config %s: %w", path, err)
		}
		if json.Valid(data) {
			return data, nil
		}
		lastData = data
		if i < attempts-1 {
			time.Sleep(backoff)
		}
	}
	head := lastData
	if len(head) > 120 {
		head = head[:120]
	}
	tail := lastData
	if len(tail) > 120 {
		tail = tail[len(tail)-120:]
	}
	return nil, fmt.Errorf("config file is not valid JSON after %d reads (size=%d, head=%q, tail=%q)",
		attempts, len(lastData), string(head), string(tail))
}

// applyFirewallConfigInternal applies a named firewall config (reusable by step
// execution). Returns containd's commit warnings (parsed from
// X-Containd-Warnings) so callers can surface partial failures — typical
// causes: nft apply hit "operation not permitted", interface reconfigure
// partial, pcap config invalid. Empty warnings + nil error = clean apply.
func (s *Server) applyFirewallConfigInternal(ctx context.Context, gen *lifecycle.Generation, configName string) ([]string, error) {
	s.policyApplyMu.Lock()
	defer s.policyApplyMu.Unlock()
	configPath, err := s.policyPath(gen.Package, configName)
	if err != nil {
		return nil, err
	}
	data, err := readPolicyJSONWithRetry(configPath)
	if err != nil {
		return nil, err
	}
	// The catalog linted the file at load; it is re-read from disk here.
	if err := labs.LintPolicy(gen.Package.Policies[configName], data, gen.Package.ICSRules); err != nil {
		return nil, err
	}

	applyStarted := time.Now().UTC()
	warnings, err := gen.Containd().ImportConfig(ctx, data)
	if err != nil {
		return nil, fmt.Errorf("failed to apply config to containd: %w", err)
	}

	// Tag the source so the frontend banner can label state correctly.
	// "weak" stays "weak"; "improved" becomes "hardened-reference" since
	// the user-facing term is "hardened" (this matches the canned-policy
	// JSON file name and the lab UI's "Apply Hardened" button label).
	source := configName
	if configName == "improved" {
		source = "hardened-reference"
	}

	if !gen.Commit(func() {
		s.activeConfigMu.Lock()
		s.activeConfig = configName
		s.policySource = source
		s.planProvenance, s.appliedPlan = nil, nil
		s.recordApplyLocked(data)
		s.policyEvidenceSince = applyStarted
		s.activeConfigMu.Unlock()
	}) {
		return nil, errRangeStopped
	}

	return warnings, nil
}

// policyPath is the file of the package's named policy.
func (s *Server) policyPath(pkg labs.Package, name string) (string, error) {
	relative, ok := pkg.Policies[name]
	if !ok {
		names := make([]string, 0, len(pkg.Policies))
		for known := range pkg.Policies {
			names = append(names, "'"+known+"'")
		}
		sort.Strings(names)
		return "", fmt.Errorf("config must be one of %s", strings.Join(names, ", "))
	}
	return filepath.Join(s.cfg.LabDefinitionsPath, relative), nil
}

type fwConfigFile struct {
	Firewall struct {
		DefaultAction string `json:"defaultAction"`
		Rules         []struct {
			ID          string   `json:"id"`
			Description string   `json:"description"`
			SourceZones []string `json:"sourceZones"`
			DestZones   []string `json:"destZones"`
			Sources     []string `json:"sources"`
			Protocols   []struct {
				Name string `json:"name"`
				Port string `json:"port"`
			} `json:"protocols"`
			Action string `json:"action"`
		} `json:"rules"`
	} `json:"firewall"`
}

func loadFirewallRules(path string) (*fwConfigFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg fwConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// compareRuleSets diffs the two policies over spec's zone pairs, in order.
func compareRuleSets(spec *compareSpec, weak, improved *fwConfigFile) []PolicyRuleDiff {
	// Index rules by zone pair for each config
	type ruleInfo struct {
		desc   string
		action string
	}

	weakByPair := map[zonePair][]ruleInfo{}
	for _, r := range weak.Firewall.Rules {
		if len(r.SourceZones) == 0 || len(r.DestZones) == 0 {
			continue // skip global rules
		}
		for _, s := range r.SourceZones {
			for _, d := range r.DestZones {
				weakByPair[zonePair{s, d}] = append(weakByPair[zonePair{s, d}], ruleInfo{r.Description, r.Action})
			}
		}
	}

	improvedByPair := map[zonePair][]ruleInfo{}
	for _, r := range improved.Firewall.Rules {
		if len(r.SourceZones) == 0 || len(r.DestZones) == 0 {
			continue
		}
		for _, s := range r.SourceZones {
			for _, d := range r.DestZones {
				improvedByPair[zonePair{s, d}] = append(improvedByPair[zonePair{s, d}], ruleInfo{r.Description, r.Action})
			}
		}
	}

	weakDefault := strings.ToUpper(weak.Firewall.DefaultAction)
	if weakDefault == "" {
		weakDefault = "DENY" // containd's runtime default if the field is absent
	}
	improvedDefault := strings.ToUpper(improved.Firewall.DefaultAction)
	if improvedDefault == "" {
		improvedDefault = "DENY"
	}

	var diffs []PolicyRuleDiff

	for _, pair := range spec.pairs {
		wRules := weakByPair[pair]
		iRules := improvedByPair[pair]

		// Combine descriptions
		wDesc := []string{}
		wAction := ""
		for _, r := range wRules {
			wDesc = append(wDesc, r.desc)
			if wAction == "" {
				wAction = r.action
			} else if wAction != r.action {
				wAction = "MIXED"
			}
		}

		iDesc := []string{}
		iAction := ""
		for _, r := range iRules {
			iDesc = append(iDesc, r.desc)
			if iAction == "" {
				iAction = r.action
			} else if iAction != r.action {
				iAction = "MIXED"
			}
		}

		// Fall back to the policy's defaultAction when no explicit
		// rule exists for this zone pair. Label the description
		// distinctively so the renderer can tell a real rule apart
		// from an implicit fallthrough at a glance.
		if len(wRules) == 0 {
			wAction = weakDefault
			wDesc = []string{"(implicit default-" + strings.ToLower(weakDefault) + " - no explicit rule)"}
		}
		if len(iRules) == 0 {
			iAction = improvedDefault
			iDesc = []string{"(implicit default-" + strings.ToLower(improvedDefault) + " - no explicit rule)"}
		}

		change := "unchanged"
		wDescJoined := strings.Join(wDesc, "; ")
		iDescJoined := strings.Join(iDesc, "; ")
		if wAction == "ALLOW" && (iAction == "DENY" || iAction == "MIXED") {
			change = "tightened"
		} else if wAction == "" && iAction != "" {
			change = "added"
		} else if wAction != "" && iAction == "" {
			change = "removed"
		} else if wAction != iAction {
			change = "tightened"
		} else if wDescJoined != iDescJoined {
			// Same action but different scope/description = narrowed
			change = "tightened"
		}

		diffs = append(diffs, PolicyRuleDiff{
			ZonePair:       spec.label(pair.src, pair.dst),
			WeakRule:       strings.Join(wDesc, "; "),
			ImprovedRule:   strings.Join(iDesc, "; "),
			WeakAction:     wAction,
			ImprovedAction: iAction,
			Change:         change,
		})
	}

	return diffs
}
