package containd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

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
	Snaplen       int        `json:"snaplen,omitempty"`   // default 262144
	MaxSizeMB     int        `json:"maxSizeMB,omitempty"` // default 64
	MaxFiles      int        `json:"maxFiles,omitempty"`  // default 8
	Mode          string     `json:"mode,omitempty"`      // "rolling" or "once"
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
func (c *Client) SetPcapConfig(ctx context.Context, cfg PcapConfig) error {
	body, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	resp, err := c.doRequestWithBody(ctx, "POST", c.BaseURL+"/api/v1/pcap/config", body)
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
func (c *Client) StartPcap(ctx context.Context, cfg *PcapConfig) (*PcapStatus, error) {
	var body []byte
	if cfg != nil {
		var err error
		body, err = json.Marshal(cfg)
		if err != nil {
			return nil, err
		}
	}
	resp, err := c.doRequestWithBody(ctx, "POST", c.BaseURL+"/api/v1/pcap/start", body)
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
func (c *Client) StopPcap(ctx context.Context) (*PcapStatus, error) {
	resp, err := c.doRequestWithBody(ctx, "POST", c.BaseURL+"/api/v1/pcap/stop", nil)
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
func (c *Client) GetPcapStatus(ctx context.Context) (*PcapStatus, error) {
	resp, err := c.doRequest(ctx, "GET", c.BaseURL+"/api/v1/pcap/status")
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
func (c *Client) ListPcapFiles(ctx context.Context) ([]PcapFileInfo, error) {
	resp, err := c.doRequest(ctx, "GET", c.BaseURL+"/api/v1/pcap/list")
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
func (c *Client) DownloadPcapFile(ctx context.Context, name string) (io.ReadCloser, string, error) {
	resp, err := c.doRequest(ctx, "GET", c.BaseURL+"/api/v1/pcap/download/"+name)
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
func (c *Client) DeletePcapFile(ctx context.Context, name string) error {
	resp, err := c.doRequest(ctx, "DELETE", c.BaseURL+"/api/v1/pcap/"+name)
	if err != nil {
		return fmt.Errorf("delete pcap file: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("delete pcap file returned %d", resp.StatusCode)
	}
	return nil
}
