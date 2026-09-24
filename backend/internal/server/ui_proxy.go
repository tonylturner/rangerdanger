package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/models"
)

func (s *Server) handleProxyNodeUI(c *gin.Context) {
	labID := c.Param("id")
	nodeID := c.Param("nodeId")

	// Get lab instance with template to access topology
	var instance models.LabInstance
	if err := s.db.Preload("Template").First(&instance, "id = ?", labID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "lab not found"})
		return
	}

	// Parse topology to find node config
	var topo struct {
		Nodes []labs.NodeYAML `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(instance.Template.Topology), &topo); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid topology"})
		return
	}

	// Find the node in topology
	var nodeConfig *labs.NodeYAML
	for i := range topo.Nodes {
		if topo.Nodes[i].ID == nodeID {
			nodeConfig = &topo.Nodes[i]
			break
		}
	}
	if nodeConfig == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "node not found in topology"})
		return
	}

	// Get UI host and port based on node type and container
	host, port := getNodeUIHostPort(nodeConfig.Type, nodeConfig.Container)
	if host == "" || port == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "ui proxy not configured for this node type"})
		return
	}

	target, err := url.Parse(fmt.Sprintf("http://%s:%d", host, port))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid upstream host"})
		return
	}

	path := c.Param("path")
	if path == "" {
		path = "/"
	}

	// Base path for rewriting HTML base href
	basePath := fmt.Sprintf("/api/labs/instances/%s/nodes/%s/ui/", labID, nodeID)

	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
		req.URL.Path = singleJoiningSlash(target.Path, path)
		// Preserve query string for socket.io polling
		req.URL.RawQuery = c.Request.URL.RawQuery
	}

	// Handle WebSocket upgrades for socket.io
	if c.GetHeader("Upgrade") == "websocket" {
		proxy.ServeHTTP(c.Writer, c.Request)
		return
	}

	proxy.ModifyResponse = func(resp *http.Response) error {
		// Only modify HTML responses
		contentType := resp.Header.Get("Content-Type")
		if !strings.Contains(contentType, "text/html") {
			return nil
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		resp.Body.Close()

		// Rewrite <base href="/"> to use our proxy path
		modified := strings.Replace(string(body), `<base href="/">`, `<base href="`+basePath+`">`, 1)

		resp.Body = io.NopCloser(strings.NewReader(modified))
		resp.ContentLength = int64(len(modified))
		resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(modified)))
		return nil
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
		rw.WriteHeader(http.StatusBadGateway)
		_, _ = rw.Write([]byte("proxy error: " + err.Error()))
	}

	proxy.ServeHTTP(c.Writer, c.Request)
}

// getNodeUIHostPort returns the host and port for a node's web UI based on type.
func getNodeUIHostPort(nodeType, container string) (string, int) {
	// Use container name as host if available, otherwise derive from type
	host := container
	if host == "" {
		switch nodeType {
		case "plc_trainer":
			host = "plc_process"
		case "sis_plc":
			host = "plc_safety"
		case "openplc":
			host = "openplc"
		case "hmi_view":
			host = "hmi_view"
		case "hmi_control":
			host = "hmi_control"
		case "fuxa_hmi":
			host = "fuxa_hmi"
		case "ews":
			host = "ews"
		case "ubuntu_jumpbox":
			host = "ubuntu_jumpbox"
		case "corp_workstation":
			host = "corp_ws"
		case "vendor_jumpbox":
			host = "vendor_jump"
		case "eng_workstation":
			host = "eng_workstation"
		default:
			return "", 0
		}
	}

	switch nodeType {
	case "plc_trainer", "sis_plc", "openplc":
		return host, 8080
	case "hmi_view", "hmi_control", "hmi_scada", "fuxa_hmi":
		return host, 1881
	case "ews", "ubuntu_jumpbox", "corp_workstation", "vendor_jumpbox", "eng_workstation":
		return host, 3000
	case "historian":
		return host, 8086
	default:
		return "", 0
	}
}

// Mirrors httputil.singleJoiningSlash without importing unexported logic.
func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}

// handleProxyContaind proxies requests to the containd firewall UI.
// This enables same-origin access, allowing the containd UI to be embedded
// in iframes with full authentication/cookie support.
func (s *Server) handleProxyContaind(c *gin.Context) {
	containdURL := s.cfg.ContaindAPIURL
	if containdURL == "" {
		containdURL = "http://firewall:8080"
	}

	target, err := url.Parse(containdURL)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "invalid containd url"})
		return
	}

	path := c.Param("path")
	if path == "" {
		path = "/"
	}

	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(req *http.Request) {
		originalDirector(req)
		req.Host = target.Host
		req.URL.Path = singleJoiningSlash(target.Path, path)
		req.URL.RawQuery = c.Request.URL.RawQuery
	}
	proxy.ErrorHandler = func(rw http.ResponseWriter, req *http.Request, err error) {
		rw.WriteHeader(http.StatusBadGateway)
		_, _ = rw.Write([]byte("containd proxy error: " + err.Error()))
	}

	// Rewrite HTML responses to fix asset paths
	proxy.ModifyResponse = func(resp *http.Response) error {
		contentType := resp.Header.Get("Content-Type")
		if !strings.Contains(contentType, "text/html") {
			return nil
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		resp.Body.Close()

		// Rewrite asset and link paths to go through the proxy
		// Use a placeholder to avoid double-replacement
		modified := string(body)
		modified = strings.ReplaceAll(modified, `"/_next/`, `"__CONTAIND_PROXY__/_next/`)
		modified = strings.ReplaceAll(modified, `"/api/`, `"__CONTAIND_PROXY__/api/`)
		modified = strings.ReplaceAll(modified, `href="/`, `href="__CONTAIND_PROXY__/`)
		modified = strings.ReplaceAll(modified, `__CONTAIND_PROXY__`, `/api/containd`)

		resp.Body = io.NopCloser(strings.NewReader(modified))
		resp.ContentLength = int64(len(modified))
		resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(modified)))
		return nil
	}

	proxy.ServeHTTP(c.Writer, c.Request)
}
