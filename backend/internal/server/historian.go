package server

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// Historian routes relay only observations over the management read API.
// Neither the ingest listener nor a write operation is exposed here.
func (s *Server) handleHistorianPoints(c *gin.Context) {
	s.proxyHistorian(c, "/api/points", true)
}

func (s *Server) handleHistorianHistory(c *gin.Context) {
	s.proxyHistorian(c, "/api/history", false)
}

func (s *Server) proxyHistorian(c *gin.Context, path string, array bool) {
	url, ok := rangeOf(c).Manifest.Endpoint(manifest.RoleHistorian, "api")
	if !ok {
		notSupported(c)
		return
	}
	if c.Request.URL.RawQuery != "" {
		path += "?" + c.Request.URL.RawQuery
	}
	status, body, err := callUpstream(c.Request.Context(), http.MethodGet, url+path, nil, upstreamReadTimeout)
	if err != nil {
		upstreamFailed(c, fmt.Errorf("historian unreachable: %w", err))
		return
	}
	if status == http.StatusBadRequest || status == http.StatusNotFound {
		// The historian owns point and time-window validation.
		result, err := jsonObject("historian", body)
		if err != nil {
			upstreamFailed(c, err)
			return
		}
		c.Data(status, "application/json; charset=utf-8", result)
		return
	}
	if status != http.StatusOK {
		upstreamFailed(c, &upstreamError{service: "historian", status: status, body: body})
		return
	}
	var valid bool
	if array {
		var points []json.RawMessage
		valid = json.Unmarshal(body, &points) == nil && points != nil
	} else {
		_, err = jsonObject("historian", body)
		valid = err == nil
	}
	if !valid {
		upstreamFailed(c, fmt.Errorf("historian returned an invalid read response"))
		return
	}
	c.Data(status, "application/json; charset=utf-8", body)
}
