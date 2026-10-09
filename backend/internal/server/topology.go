package server

import (
	"github.com/gin-gonic/gin"
)

type graphNodeData struct {
	Label         string            `json:"label"`
	Zone          string            `json:"zone"`
	Networks      []string          `json:"networks"`
	Status        string            `json:"status,omitempty"`
	IP            string            `json:"ip,omitempty"`
	InterfaceIPs  map[string]string `json:"interface_ips,omitempty"` // network -> IP for multi-homed nodes
	UIPath        string            `json:"ui_path,omitempty"`
	ExternalUIURL string            `json:"external_ui_url,omitempty"` // direct URL for external UI access
}

type graphNode struct {
	ID       string        `json:"id"`
	Type     string        `json:"type"`
	Position gin.H         `json:"position"`
	Data     graphNodeData `json:"data"`
}

type graphEdge struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
	Label  string `json:"label,omitempty"`
}

// getNodeUIConfig returns the proxy path of a node type's web UI and an
// external URL for it. Both are same-origin paths served by the package's
// proxy routes; types without a web UI get neither. Only node types some
// topology uses are listed.
func getNodeUIConfig(nodeType string) (uiPath, externalURL string) {
	switch nodeType {
	case "containd_ngfw":
		// Same-origin paths only: hardcoding localhost:9080 here would
		// break any deployment that isn't bound to loopback.
		return "/apps/firewall/", "/containd/"
	case "fuxa_hmi":
		return "/apps/fuxa-hmi/", ""
	case "openplc":
		return "/apps/openplc/", ""
	case "corp_workstation":
		return "/apps/corp-ws/", ""
	case "vendor_jumpbox":
		return "/apps/vendor-jump/", ""
	case "eng_workstation":
		return "/apps/eng-ws/", ""
	default:
		return "", ""
	}
}
