package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/labs"
)

func TestNodeUIConfig(t *testing.T) {
	for _, tc := range []struct{ nodeType, uiPath, externalURL string }{
		{"containd_ngfw", "/apps/firewall/", "/containd/"},
		// The package's proxy route, not a backend UI proxy.
		{"openplc", "/apps/openplc/", ""},
		{"fuxa_hmi", "/apps/fuxa-hmi/", ""},
		{"eng_workstation", "/apps/eng-ws/", ""},
		{"corp_workstation", "/apps/corp-ws/", ""},
		{"vendor_jumpbox", "/apps/vendor-jump/", ""},
		{"kali_pentest", "", ""},
		{"historian_sim", "", ""},
		// No topology uses these; /apps/fuxa/ never had a proxy route.
		{"hmi_scada", "", ""},
		{"plc_trainer", "", ""},
	} {
		uiPath, externalURL := getNodeUIConfig(tc.nodeType)
		if uiPath != tc.uiPath || externalURL != tc.externalURL {
			t.Errorf("getNodeUIConfig(%q) = (%q, %q), want (%q, %q)", tc.nodeType, uiPath, externalURL, tc.uiPath, tc.externalURL)
		}
	}
}

// TestNodeUIPathsHaveProxyRoutes checks every shipped package: each UI path
// its topology's nodes get is a location in the package's proxy routes.
func TestNodeUIPathsHaveProxyRoutes(t *testing.T) {
	definitions := filepath.Join(repoRoot(), "lab-definitions")
	catalog, err := labs.NewLoader(definitions, ValidatorRequirements()).Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range catalog.Packages {
		routes, err := os.ReadFile(filepath.Join(definitions, "packages", pkg.ID, "nginx.routes.conf"))
		if err != nil {
			t.Fatal(err)
		}
		for _, node := range pkg.Template.Nodes {
			uiPath, externalURL := getNodeUIConfig(node.Type)
			for _, path := range []string{uiPath, externalURL} {
				if path != "" && !strings.Contains(string(routes), "location "+path+" {") {
					t.Errorf("package %s node %s (%s): UI path %s has no location in nginx.routes.conf", pkg.ID, node.ID, node.Type, path)
				}
			}
		}
	}
}
