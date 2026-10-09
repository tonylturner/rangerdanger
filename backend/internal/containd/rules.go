package containd

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// FirewallRule represents a single firewall rule from containd config.
type FirewallRule struct {
	ID          string     `json:"id"`
	Description string     `json:"description"`
	SourceZones []string   `json:"sourceZones,omitempty"`
	DestZones   []string   `json:"destZones,omitempty"`
	Sources     []string   `json:"sources,omitempty"`
	Protocols   []Protocol `json:"protocols,omitempty"`
	ICS         *ICSConfig `json:"ics,omitempty"`
	Action      string     `json:"action"` // "ALLOW" or "DENY"
}

// Protocol defines protocol matching for a rule.
type Protocol struct {
	Name string `json:"name"`
	Port string `json:"port,omitempty"`
}

// ICSConfig is the subset of containd's ICS rule predicate that the
// backend reads. containd's GET /api/v1/config always serialises
// functionCode as a numeric array.
type ICSConfig struct {
	Protocol      string `json:"protocol,omitempty"`     // "modbus", "dnp3", etc.
	FunctionCodes []int  `json:"functionCode,omitempty"` // Allowed function codes
	ReadOnly      bool   `json:"readOnly,omitempty"`     // containd's read-only class
}

// readFunctionCodes lists, per ICS protocol, the function codes that
// only read from the device: Modbus 1-4 (coils, discrete inputs,
// holding and input registers) and DNP3 1 (READ).
var readFunctionCodes = map[string]map[int]bool{
	"modbus": {1: true, 2: true, 3: true, 4: true},
	"dnp3":   {1: true},
}

// IsReadOnly reports whether the predicate admits only reads: either
// containd's readOnly class is set, or every listed function code is a
// read code for the protocol. An empty code list matches every
// function code, so it is not read-only.
func (ics *ICSConfig) IsReadOnly() bool {
	if ics.ReadOnly {
		return true
	}
	if len(ics.FunctionCodes) == 0 {
		return false
	}
	reads := readFunctionCodes[ics.Protocol]
	for _, fc := range ics.FunctionCodes {
		if !reads[fc] {
			return false
		}
	}
	return true
}

// FirewallConfig represents the firewall section of containd config.
type FirewallConfig struct {
	DefaultAction string         `json:"defaultAction"`
	Rules         []FirewallRule `json:"rules"`
}

// ZoneRuleSummary summarizes rules for a specific zone pair.
type ZoneRuleSummary struct {
	SourceZone  string   `json:"source_zone"`
	DestZone    string   `json:"dest_zone"`
	Summary     string   `json:"summary"`      // Brief summary for edge label
	RuleDetails []string `json:"rule_details"` // Full descriptions for tooltip
	Action      string   `json:"action"`       // Primary action (ALLOW/DENY/MIXED)
}

// GetFirewallRules returns the firewall rules from containd config.
func (c *Client) GetFirewallRules() ([]FirewallRule, error) {
	resp, err := c.doRequest("GET", c.BaseURL+"/api/v1/config")
	if err != nil {
		return nil, fmt.Errorf("get config failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get config returned %d: %s", resp.StatusCode, string(body))
	}

	var config struct {
		Firewall FirewallConfig `json:"firewall"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	return config.Firewall.Rules, nil
}

// GetZoneRuleSummaries returns summarized rules grouped by zone pairs.
func (c *Client) GetZoneRuleSummaries() ([]ZoneRuleSummary, error) {
	rules, err := c.GetFirewallRules()
	if err != nil {
		return nil, err
	}

	// Group rules by zone pair
	type zonePair struct {
		src, dst string
	}
	pairRules := make(map[zonePair][]FirewallRule)

	for _, rule := range rules {
		srcZones := rule.SourceZones
		if len(srcZones) == 0 {
			srcZones = []string{"any"}
		}
		dstZones := rule.DestZones
		if len(dstZones) == 0 {
			dstZones = []string{"any"}
		}

		for _, src := range srcZones {
			for _, dst := range dstZones {
				pair := zonePair{src: src, dst: dst}
				pairRules[pair] = append(pairRules[pair], rule)
			}
		}
	}

	// Create summaries
	var summaries []ZoneRuleSummary
	for pair, rules := range pairRules {
		summary := ZoneRuleSummary{
			SourceZone:  pair.src,
			DestZone:    pair.dst,
			RuleDetails: make([]string, 0, len(rules)),
		}

		allowCount := 0
		denyCount := 0
		var protocols []string

		for _, rule := range rules {
			summary.RuleDetails = append(summary.RuleDetails, rule.Description)

			if rule.Action == "ALLOW" {
				allowCount++
			} else if rule.Action == "DENY" {
				denyCount++
			}

			// Collect protocol info for summary
			for _, p := range rule.Protocols {
				if p.Port != "" {
					protocols = append(protocols, p.Port)
				} else if p.Name != "" {
					protocols = append(protocols, p.Name)
				}
			}

			// Check for ICS protocols
			if rule.ICS != nil && rule.ICS.Protocol != "" {
				if rule.ICS.IsReadOnly() {
					protocols = append(protocols, rule.ICS.Protocol+" R/O")
				} else {
					protocols = append(protocols, rule.ICS.Protocol)
				}
			}
		}

		// Determine action
		if denyCount > 0 && allowCount > 0 {
			summary.Action = "MIXED"
		} else if denyCount > 0 {
			summary.Action = "DENY"
		} else {
			summary.Action = "ALLOW"
		}

		// Create brief summary
		if len(protocols) > 0 {
			// Deduplicate and limit
			seen := make(map[string]bool)
			var unique []string
			for _, p := range protocols {
				if !seen[p] {
					seen[p] = true
					unique = append(unique, p)
				}
			}
			sort.Strings(unique)
			if len(unique) > 3 {
				summary.Summary = fmt.Sprintf("%s +%d more", unique[0], len(unique)-1)
			} else {
				summary.Summary = strings.Join(unique, ", ")
			}
		} else {
			summary.Summary = summary.Action
		}

		summaries = append(summaries, summary)
	}

	// Sort deterministically so frontend labels don't flicker between polls
	sort.Slice(summaries, func(i, j int) bool {
		if summaries[i].SourceZone != summaries[j].SourceZone {
			return summaries[i].SourceZone < summaries[j].SourceZone
		}
		return summaries[i].DestZone < summaries[j].DestZone
	})

	return summaries, nil
}
