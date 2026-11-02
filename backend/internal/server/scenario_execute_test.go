package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

func TestStepActionRTACResponse(t *testing.T) {
	_, firewall := newUpstreamStub(t, map[string]stubResponse{
		"GET /api/v1/config": {http.StatusOK, `{"firewall":{"defaultAction":"ALLOW","rules":[]}}`},
	})
	const executed = `{"result":"executed","detail":"command executed","process_impact":"fixture impact"}`
	const limit = 1 << 20
	tests := []struct {
		name       string
		status     int
		body       string
		shortRead  bool
		success    bool
		wantDetail string
		wantImpact string
	}{
		{name: "executed", status: 200, body: executed, success: true, wantDetail: "command executed", wantImpact: "fixture impact"},
		{name: "success", status: 200, body: `{"result":"success","detail":"done"}`, success: true, wantDetail: "done"},
		{name: "last 2xx", status: 299, body: executed, success: true, wantDetail: "command executed", wantImpact: "fixture impact"},
		{name: "server error executed", status: 500, body: executed, wantDetail: "command executed", wantImpact: "fixture impact"},
		{name: "client error success", status: 400, body: `{"result":"success","detail":"refused"}`, wantDetail: "refused"},
		{name: "first non 2xx", status: 300, body: executed, wantDetail: "command executed", wantImpact: "fixture impact"},
		{name: "garbage", status: 200, body: "garbage", wantDetail: "invalid JSON response"},
		{name: "server error garbage", status: 500, body: "garbage", wantDetail: "invalid JSON response"},
		{name: "trailing garbage", status: 200, body: executed + "garbage", wantDetail: "invalid JSON response"},
		{name: "empty", status: 200, wantDetail: "invalid JSON response"},
		{name: "array", status: 200, body: `[]`, wantDetail: "invalid JSON response"},
		{name: "null", status: 200, body: `null`},
		{name: "missing result", status: 200, body: `{"detail":"no result"}`, wantDetail: "no result"},
		{name: "non string result", status: 200, body: `{"result":true}`},
		{name: "rejected", status: 200, body: `{"result":"rejected","detail":"device refused"}`, wantDetail: "device refused"},
		{name: "at limit", status: 200, body: executed + strings.Repeat(" ", limit-len(executed)), success: true, wantDetail: "command executed", wantImpact: "fixture impact"},
		{name: "over limit", status: 200, body: executed + strings.Repeat(" ", limit+1-len(executed)), wantDetail: "response exceeds 1048576 bytes"},
		{name: "short read", status: 200, body: executed, shortRead: true, wantDetail: "cannot read response"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rtac := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/api/command/recloser" {
					t.Errorf("unexpected RTAC request: %s %s", r.Method, r.URL.Path)
				}
				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Errorf("decode command: %v", err)
				}
				want := map[string]any{"command": "open", "source": "operator", "value": float64(1)}
				if !reflect.DeepEqual(payload, want) {
					t.Errorf("payload = %v, want %v", payload, want)
				}
				if test.shortRead {
					w.Header().Set("Content-Length", fmt.Sprint(len(test.body)+1))
				}
				w.WriteHeader(test.status)
				io.WriteString(w, test.body)
			}))
			defer rtac.Close()

			gen := usGeneration(containd.NewClient(firewall.URL))
			for i := range gen.Manifest.Services {
				service := &gen.Manifest.Services[i]
				for _, role := range service.Roles {
					if role == manifest.RoleRTAC {
						service.Endpoints["api"] = rtac.URL
					}
				}
			}
			s := &Server{}
			value := float64(1)
			result := s.executeRTACCommand(t.Context(), gen, usRecipe, "recloser", "open", "operator", &value)
			if result.Success != test.success || result.Blocked || result.Action != "recloser/open from operator" {
				t.Fatalf("unexpected result: %+v", result)
			}
			if result.Impact != test.wantImpact {
				t.Errorf("impact = %q, want %q", result.Impact, test.wantImpact)
			}
			if test.success {
				if result.Detail != test.wantDetail {
					t.Errorf("detail = %q, want %q", result.Detail, test.wantDetail)
				}
			} else if !strings.Contains(result.Detail, fmt.Sprintf("HTTP %d", test.status)) ||
				!strings.Contains(result.Detail, test.wantDetail) {
				t.Errorf("detail = %q, want status %d and %q", result.Detail, test.status, test.wantDetail)
			}
		})
	}
}
