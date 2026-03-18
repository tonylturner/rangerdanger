package containd

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// Client communicates with the containd NGFW API.
type Client struct {
	BaseURL    string
	AuthToken  string
	httpClient *http.Client
}

// EventID handles containd returning IDs as either strings or numbers.
type EventID string

func (e *EventID) UnmarshalJSON(data []byte) error {
	// Try string first, then number
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*e = EventID(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err == nil {
		*e = EventID(n.String())
		return nil
	}
	*e = EventID(string(data))
	return nil
}

// Event represents a containd DPI/IDS event.
type Event struct {
	ID        EventID   `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`      // "connection", "modbus", "dns", "alert"
	Source    string    `json:"source"`    // Source IP
	Dest      string    `json:"dest"`      // Destination IP
	Protocol  string    `json:"protocol"`  // "modbus", "tcp", "udp", etc.
	SrcPort   int       `json:"src_port"`
	DstPort   int       `json:"dst_port"`
	Details   string    `json:"details"`   // Human-readable description
	Severity  string    `json:"severity"`  // "info", "warning", "critical"
	Zone      string    `json:"zone"`      // Source zone
}

// Session represents an active connection through the firewall.
type Session struct {
	ID        string    `json:"id"`
	Source    string    `json:"source"`
	Dest      string    `json:"dest"`
	Protocol  string    `json:"protocol"`
	SrcPort   int       `json:"src_port"`
	DstPort   int       `json:"dst_port"`
	StartTime time.Time `json:"start_time"`
	Bytes     int64     `json:"bytes"`
	Packets   int64     `json:"packets"`
}

// HealthStatus represents the firewall health.
type HealthStatus struct {
	Status    string `json:"status"` // "healthy", "degraded", "unhealthy"
	Version   string `json:"version"`
	Uptime    int64  `json:"uptime"`
	Zones     int    `json:"zones"`
	Sessions  int    `json:"sessions"`
	EventRate int    `json:"event_rate"` // Events per second
}

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

// ICSConfig defines ICS-specific DPI settings.
type ICSConfig struct {
	Protocol      string `json:"protocol,omitempty"`      // "modbus", "dnp3", etc.
	FunctionCodes []int  `json:"functionCodes,omitempty"` // Allowed function codes
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

// NewClient creates a containd API client with JWT authentication.
func NewClient(baseURL string) *Client {
	// Get JWT secret from environment (same as containd uses)
	secret := os.Getenv("CONTAIND_JWT_SECRET")
	if secret == "" {
		secret = "rangerdanger-dev" // Default matches docker-compose
	}

	// Generate a simple JWT token for API access
	token := generateJWT(secret)

	return &Client{
		BaseURL:   baseURL,
		AuthToken: token,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// generateJWT creates a minimal JWT token for containd API auth.
func generateJWT(secret string) string {
	// JWT header: {"alg":"HS256","typ":"JWT"}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

	// JWT payload with admin role and long expiry
	exp := time.Now().Add(24 * time.Hour).Unix()
	payload := fmt.Sprintf(`{"sub":"rangerdanger-backend","role":"admin","exp":%d}`, exp)
	payloadEnc := base64.RawURLEncoding.EncodeToString([]byte(payload))

	// Sign with HMAC-SHA256
	message := header + "." + payloadEnc
	h := hmac.New(sha256.New, []byte(secret))
	h.Write([]byte(message))
	signature := base64.RawURLEncoding.EncodeToString(h.Sum(nil))

	return message + "." + signature
}

// doRequest performs an authenticated HTTP request.
func (c *Client) doRequest(method, url string) (*http.Response, error) {
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}
	return c.httpClient.Do(req)
}

// doRequestWithBody performs an authenticated HTTP request with a body.
func (c *Client) doRequestWithBody(method, url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}
	return c.httpClient.Do(req)
}

// GetHealth returns the firewall health status.
func (c *Client) GetHealth() (*HealthStatus, error) {
	resp, err := c.doRequest("GET", c.BaseURL+"/api/v1/health")
	if err != nil {
		return nil, fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("health check returned %d", resp.StatusCode)
	}

	var status HealthStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decode health: %w", err)
	}

	return &status, nil
}

// GetEvents returns recent events, optionally filtered by time.
func (c *Client) GetEvents(since string, limit int) ([]Event, error) {
	url := fmt.Sprintf("%s/api/v1/events?limit=%d", c.BaseURL, limit)
	if since != "" {
		url += "&since=" + since
	}

	resp, err := c.doRequest("GET", url)
	if err != nil {
		return nil, fmt.Errorf("get events failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get events returned %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read events body: %w", err)
	}

	// containd may return events as a raw array or as {"events": [...]}
	var events []Event
	if err := json.Unmarshal(body, &events); err != nil {
		// Try wrapped format
		var result struct {
			Events []Event `json:"events"`
		}
		if err2 := json.Unmarshal(body, &result); err2 != nil {
			return nil, fmt.Errorf("decode events: %w (also tried array: %w)", err2, err)
		}
		events = result.Events
	}

	return events, nil
}

// GetSessions returns active sessions through the firewall.
func (c *Client) GetSessions() ([]Session, error) {
	resp, err := c.doRequest("GET", c.BaseURL+"/api/v1/sessions")
	if err != nil {
		return nil, fmt.Errorf("get sessions failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get sessions returned %d", resp.StatusCode)
	}

	var result struct {
		Sessions []Session `json:"sessions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode sessions: %w", err)
	}

	return result.Sessions, nil
}

// IsAvailable checks if containd is reachable.
func (c *Client) IsAvailable() bool {
	status, err := c.GetHealth()
	return err == nil && status.Status != ""
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
				if len(rule.ICS.FunctionCodes) > 0 && len(rule.ICS.FunctionCodes) <= 4 {
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

// ImportConfig sends a full JSON config to containd using the candidate/commit flow.
// Flow: POST /api/v1/config/candidate → POST /api/v1/config/commit
// Falls back to legacy /api/v1/config/import if candidate endpoint is unavailable.
func (c *Client) ImportConfig(configJSON []byte) error {
	// Try the preferred candidate/commit flow first
	err := c.importViaCandidate(configJSON)
	if err == nil {
		return nil
	}

	// Fall back to legacy import endpoint
	log.Printf("containd: candidate/commit flow failed (%v), falling back to /config/import", err)
	return c.importLegacy(configJSON)
}

// importViaCandidate uses the appliance candidate/commit flow:
// 1. POST /api/v1/config/candidate — stages the config
// 2. POST /api/v1/config/commit — applies it (triggers nftables compilation)
func (c *Client) importViaCandidate(configJSON []byte) error {
	// Stage the candidate config
	resp, err := c.doRequestWithBody("POST", c.BaseURL+"/api/v1/config/candidate", configJSON)
	if err != nil {
		return fmt.Errorf("post candidate config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("candidate endpoint not available (404)")
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("post candidate returned %d: %s", resp.StatusCode, string(body))
	}

	// Commit the staged config
	resp2, err := c.doRequestWithBody("POST", c.BaseURL+"/api/v1/config/commit", nil)
	if err != nil {
		return fmt.Errorf("commit config: %w", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK && resp2.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp2.Body)
		return fmt.Errorf("commit returned %d: %s", resp2.StatusCode, string(body))
	}

	return nil
}

// importLegacy uses the older /api/v1/config/import endpoint.
func (c *Client) importLegacy(configJSON []byte) error {
	resp, err := c.doRequestWithBody("POST", c.BaseURL+"/api/v1/config/import", configJSON)
	if err != nil {
		return fmt.Errorf("import config request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("import config returned %d: %s", resp.StatusCode, string(body))
	}

	return nil
}

// ── PCAP Capture API (containd /api/v1/pcap/*) ─────────────────

// PcapFilter defines the structured filter for containd captures.
// containd does NOT accept BPF strings — only structured src/dst/proto.
type PcapFilter struct {
	Src   string `json:"src,omitempty"`   // source CIDR or IP
	Dst   string `json:"dst,omitempty"`   // destination CIDR or IP
	Proto string `json:"proto,omitempty"` // "tcp", "udp", "icmp", "any"
}

// PcapConfig is the body for POST /api/v1/pcap/config and /api/v1/pcap/start.
type PcapConfig struct {
	Enabled       bool       `json:"enabled,omitempty"`
	Interfaces    []string   `json:"interfaces,omitempty"`
	Snaplen       int        `json:"snaplen,omitempty"`       // default 262144
	MaxSizeMB     int        `json:"maxSizeMB,omitempty"`     // default 64
	MaxFiles      int        `json:"maxFiles,omitempty"`      // default 8
	Mode          string     `json:"mode,omitempty"`          // "rolling" or "once"
	Promisc       bool       `json:"promisc,omitempty"`
	BufferMB      int        `json:"bufferMB,omitempty"`      // default 4
	RotateSeconds int        `json:"rotateSeconds,omitempty"` // default 300
	FilePrefix    string     `json:"filePrefix,omitempty"`    // default "capture"
	Filter        PcapFilter `json:"filter,omitempty"`
}

// PcapStatus is the response from GET /api/v1/pcap/status and POST start/stop.
type PcapStatus struct {
	Running    bool     `json:"running"`
	Interfaces []string `json:"interfaces,omitempty"`
	StartedAt  string   `json:"startedAt,omitempty"`
	LastError  string   `json:"lastError,omitempty"`
}

// PcapFileInfo is one item from GET /api/v1/pcap/list (returns raw array).
// File name format: {filePrefix}_{interface}_{timestamp}_{seq}.pcap
type PcapFileInfo struct {
	Name      string   `json:"name"`
	Interface string   `json:"interface"`
	SizeBytes int64    `json:"sizeBytes"`
	CreatedAt string   `json:"createdAt"`
	Tags      []string `json:"tags,omitempty"`
	Status    string   `json:"status,omitempty"` // "ready", "capturing"
}

// SetPcapConfig updates the PCAP configuration on containd.
func (c *Client) SetPcapConfig(cfg PcapConfig) error {
	body, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	resp, err := c.doRequestWithBody("POST", c.BaseURL+"/api/v1/pcap/config", body)
	if err != nil {
		return fmt.Errorf("set pcap config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("set pcap config returned %d: %s", resp.StatusCode, string(b))
	}
	return nil
}

// StartPcap starts packet capture. Accepts an optional PcapConfig override.
// Returns the capture status.
func (c *Client) StartPcap(cfg *PcapConfig) (*PcapStatus, error) {
	var body []byte
	if cfg != nil {
		var err error
		body, err = json.Marshal(cfg)
		if err != nil {
			return nil, err
		}
	}
	resp, err := c.doRequestWithBody("POST", c.BaseURL+"/api/v1/pcap/start", body)
	if err != nil {
		return nil, fmt.Errorf("start pcap: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusConflict {
		return nil, fmt.Errorf("capture already in progress")
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("start pcap returned %d: %s", resp.StatusCode, string(b))
	}

	var status PcapStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decode pcap start response: %w", err)
	}
	return &status, nil
}

// StopPcap stops an active capture.
func (c *Client) StopPcap() (*PcapStatus, error) {
	resp, err := c.doRequestWithBody("POST", c.BaseURL+"/api/v1/pcap/stop", nil)
	if err != nil {
		return nil, fmt.Errorf("stop pcap: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("stop pcap returned %d: %s", resp.StatusCode, string(b))
	}

	var status PcapStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decode pcap stop response: %w", err)
	}
	return &status, nil
}

// GetPcapStatus returns the current capture status.
func (c *Client) GetPcapStatus() (*PcapStatus, error) {
	resp, err := c.doRequest("GET", c.BaseURL+"/api/v1/pcap/status")
	if err != nil {
		return nil, fmt.Errorf("pcap status: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pcap status returned %d", resp.StatusCode)
	}

	var status PcapStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, fmt.Errorf("decode pcap status: %w", err)
	}
	return &status, nil
}

// ListPcapFiles returns all stored PCAP files. containd returns a raw array.
func (c *Client) ListPcapFiles() ([]PcapFileInfo, error) {
	resp, err := c.doRequest("GET", c.BaseURL+"/api/v1/pcap/list")
	if err != nil {
		return nil, fmt.Errorf("list pcap files: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list pcap files returned %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read pcap list body: %w", err)
	}

	// containd returns a raw JSON array, not wrapped in an object
	var files []PcapFileInfo
	if err := json.Unmarshal(body, &files); err != nil {
		// Try wrapped format as fallback
		var wrapped struct {
			Files []PcapFileInfo `json:"files"`
		}
		if err2 := json.Unmarshal(body, &wrapped); err2 != nil {
			return nil, fmt.Errorf("decode pcap list: %w (also tried array: %w)", err2, err)
		}
		files = wrapped.Files
	}
	return files, nil
}

// DownloadPcapFile streams a PCAP file from containd by filename.
func (c *Client) DownloadPcapFile(name string) (io.ReadCloser, string, error) {
	resp, err := c.doRequest("GET", c.BaseURL+"/api/v1/pcap/download/"+name)
	if err != nil {
		return nil, "", fmt.Errorf("download pcap: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, "", fmt.Errorf("download pcap returned %d", resp.StatusCode)
	}

	filename := name
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if i := strings.Index(cd, "filename="); i != -1 {
			filename = strings.Trim(cd[i+9:], "\" ")
		}
	}

	return resp.Body, filename, nil
}

// DeletePcapFile removes a stored PCAP file by name.
func (c *Client) DeletePcapFile(name string) error {
	req, err := http.NewRequest("DELETE", c.BaseURL+"/api/v1/pcap/"+name, nil)
	if err != nil {
		return err
	}
	if c.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.AuthToken)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete pcap file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("delete pcap file returned %d", resp.StatusCode)
	}
	return nil
}

// WaitReady polls containd health until it responds or the context is cancelled.
func (c *Client) WaitReady(ctx context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("containd not ready after %s: %w", timeout, ctx.Err())
		case <-ticker.C:
			if c.IsAvailable() {
				return nil
			}
		}
	}
}
