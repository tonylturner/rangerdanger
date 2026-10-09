package server

import "testing"

func TestNodeUIConfig(t *testing.T) {
	for _, tc := range []struct{ nodeType, uiPath, externalURL string }{
		{"containd_ngfw", "/apps/firewall/", "/containd/"},
		// The package's proxy route, not a backend UI proxy.
		{"openplc", "/apps/openplc/", ""},
		{"fuxa_hmi", "/apps/fuxa-hmi/", ""},
		{"eng_workstation", "/apps/eng-ws/", ""},
		{"kali_pentest", "", ""},
		{"historian_sim", "", ""},
	} {
		uiPath, externalURL := getNodeUIConfig(tc.nodeType)
		if uiPath != tc.uiPath || externalURL != tc.externalURL {
			t.Errorf("getNodeUIConfig(%q) = (%q, %q), want (%q, %q)", tc.nodeType, uiPath, externalURL, tc.uiPath, tc.externalURL)
		}
	}
}
