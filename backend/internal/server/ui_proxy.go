package server

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/models"
)

// handleProxyNodeUI proxies a lab instance node's web UI: the manifest's
// "ui" endpoint of the node's service.
func (s *Server) handleProxyNodeUI(c *gin.Context) {
	labID := c.Param("id")
	nodeID := c.Param("nodeId")

	var instance models.LabInstance
	if err := s.db.First(&instance, "id = ?", labID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "lab not found"})
		return
	}
	svc, err := nodeService(rangeOf(c), nodeID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	endpoint, ok := svc.Endpoints["ui"]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "ui proxy not configured for this node"})
		return
	}
	target, err := url.Parse(endpoint)
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
	target, err := url.Parse(rangeOf(c).Containd().BaseURL)
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
