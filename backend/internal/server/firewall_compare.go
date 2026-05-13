package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// PolicyRuleDiff shows a single rule change between two configs.
type PolicyRuleDiff struct {
	ZonePair    string `json:"zone_pair"`    // e.g. "enterprise → field"
	WeakRule    string `json:"weak_rule"`    // description from weak config (empty if not present)
	ImprovedRule string `json:"improved_rule"` // description from improved config (empty if not present)
	WeakAction  string `json:"weak_action"`
	ImprovedAction string `json:"improved_action"`
	Change      string `json:"change"` // "tightened", "added", "removed", "unchanged"
}

// PolicyComparison is the full comparison response.
type PolicyComparison struct {
	WeakConfig     string           `json:"weak_config"`
	ImprovedConfig string           `json:"improved_config"`
	Diffs          []PolicyRuleDiff `json:"diffs"`
	Summary        string           `json:"summary"`
}

// handleFirewallCompare returns a structured comparison of weak vs improved firewall configs.
func (s *Server) handleFirewallCompare(c *gin.Context) {
	labDefsDir := s.cfg.LabDefinitionsPath
	if labDefsDir == "" {
		labDefsDir = "lab-definitions"
	}

	weakPath := filepath.Join(labDefsDir, "firewall", "substation-weak.json")
	improvedPath := filepath.Join(labDefsDir, "firewall", "substation-improved.json")

	weakRules, err := loadFirewallRules(weakPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load weak config: " + err.Error()})
		return
	}

	improvedRules, err := loadFirewallRules(improvedPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load improved config: " + err.Error()})
		return
	}

	// Build diff by zone pair
	diffs := compareRuleSets(weakRules, improvedRules)

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
		WeakConfig:     "substation-weak.json",
		ImprovedConfig: "substation-improved.json",
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

// handleFirewallActive returns the currently active firewall config name.
func (s *Server) handleFirewallActive(c *gin.Context) {
	s.activeConfigMu.RLock()
	active := s.activeConfig
	s.activeConfigMu.RUnlock()

	c.JSON(http.StatusOK, gin.H{"active_config": active})
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

	warnings, err := s.applyFirewallConfigInternal(req.Config)
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
func (s *Server) applyFirewallConfigInternal(configName string) ([]string, error) {
	if configName != "weak" && configName != "improved" {
		return nil, fmt.Errorf("config must be 'weak' or 'improved'")
	}

	labDefsDir := s.cfg.LabDefinitionsPath
	if labDefsDir == "" {
		labDefsDir = "lab-definitions"
	}

	configPath := filepath.Join(labDefsDir, "firewall", "substation-"+configName+".json")
	data, err := readPolicyJSONWithRetry(configPath)
	if err != nil {
		return nil, err
	}

	if s.containdClient == nil {
		return nil, fmt.Errorf("containd client not configured")
	}

	warnings, err := s.containdClient.ImportConfig(data)
	if err != nil {
		return nil, fmt.Errorf("failed to apply config to containd: %w", err)
	}

	s.activeConfigMu.Lock()
	s.activeConfig = configName
	s.activeConfigMu.Unlock()

	return warnings, nil
}

// handleFirewallApplyCustom accepts a raw JSON config and applies it to containd.
// Used by the dynamic Exercise 3 to apply the student's remediation-plan-derived config.
func (s *Server) handleFirewallApplyCustom(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 512*1024)
	data, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request body too large or unreadable"})
		return
	}

	var config struct {
		Firewall struct {
			Rules []json.RawMessage `json:"rules"`
		} `json:"firewall"`
		Interfaces []json.RawMessage `json:"interfaces"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid config JSON: " + err.Error()})
		return
	}
	if len(config.Firewall.Rules) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "config must include firewall.rules"})
		return
	}
	if len(config.Interfaces) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "config must include interfaces"})
		return
	}

	if s.containdClient == nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "containd client not configured"})
		return
	}

	warnings, err := s.containdClient.ImportConfig(data)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("failed to apply config to containd: %v", err)})
		return
	}

	s.activeConfigMu.Lock()
	s.activeConfig = "custom"
	s.activeConfigMu.Unlock()

	resp := gin.H{
		"status":        "applied",
		"active_config": "custom",
	}
	if len(warnings) > 0 {
		resp["warnings"] = warnings
	}
	c.JSON(http.StatusOK, resp)
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

// zonePairLabel maps containd zone names to human-readable labels.
func zonePairLabel(src, dst string) string {
	names := map[string]string{
		"wan":  "Enterprise",
		"dmz":  "Vendor",
		"lan1": "OT Ops",
		"lan2": "Field",
	}
	s := names[src]
	if s == "" {
		s = src
	}
	d := names[dst]
	if d == "" {
		d = dst
	}
	return s + " → " + d
}

func compareRuleSets(weak, improved *fwConfigFile) []PolicyRuleDiff {
	// Index rules by zone pair for each config
	type zonePair struct{ src, dst string }
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

	// Collect all zone pairs
	allPairs := map[zonePair]bool{}
	for p := range weakByPair {
		allPairs[p] = true
	}
	for p := range improvedByPair {
		allPairs[p] = true
	}

	// Ordered zone pairs for consistent output
	orderedPairs := []zonePair{
		{"wan", "dmz"}, {"wan", "lan1"}, {"wan", "lan2"},
		{"dmz", "lan1"}, {"dmz", "lan2"},
		{"lan1", "lan2"}, {"lan1", "lan1"}, {"lan2", "lan2"},
	}

	var diffs []PolicyRuleDiff

	for _, pair := range orderedPairs {
		wRules := weakByPair[pair]
		iRules := improvedByPair[pair]

		if len(wRules) == 0 && len(iRules) == 0 {
			continue
		}

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
			ZonePair:       zonePairLabel(pair.src, pair.dst),
			WeakRule:       strings.Join(wDesc, "; "),
			ImprovedRule:   strings.Join(iDesc, "; "),
			WeakAction:     wAction,
			ImprovedAction: iAction,
			Change:         change,
		})
	}

	return diffs
}
