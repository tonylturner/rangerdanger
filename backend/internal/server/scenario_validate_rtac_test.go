package server

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

func TestValidateRTACResponse(t *testing.T) {
	const limit = 1 << 20
	for _, endpoint := range []struct {
		name string
		path string
		body string
		want any
	}{
		{
			name: "state",
			path: "/api/state",
			body: `{"electrical":{"recloser_closed":true}}`,
			want: map[string]any{"electrical": map[string]any{"recloser_closed": true}},
		},
		{
			name: "audit",
			path: "/api/audit",
			body: `{"entries":[{"command":"open","source":"operator"}]}`,
			want: []map[string]any{{"command": "open", "source": "operator"}},
		},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			for _, test := range []struct {
				name      string
				status    int
				body      string
				shortRead bool
				wantError string
			}{
				{name: "valid", status: 200, body: endpoint.body},
				{name: "last 2xx", status: 299, body: endpoint.body},
				{name: "server error", status: 500, body: endpoint.body, wantError: "HTTP 500"},
				{name: "first non 2xx", status: 300, body: endpoint.body, wantError: "HTTP 300"},
				{name: "garbage", status: 200, body: "garbage", wantError: "invalid JSON response"},
				{name: "trailing garbage", status: 200, body: endpoint.body + "garbage", wantError: "invalid JSON response"},
				{name: "empty", status: 200, wantError: "invalid JSON response"},
				{name: "wrong type", status: 200, body: `[]`, wantError: "invalid JSON response"},
				{name: "at limit", status: 200, body: endpoint.body + strings.Repeat(" ", limit-len(endpoint.body))},
				{name: "over limit", status: 200, body: endpoint.body + strings.Repeat(" ", limit+1-len(endpoint.body)), wantError: "response exceeds 1048576 bytes"},
				{name: "short read", status: 200, body: endpoint.body, shortRead: true, wantError: "cannot read response"},
			} {
				t.Run(test.name, func(t *testing.T) {
					rtac := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.Method != http.MethodGet || r.URL.Path != endpoint.path {
							t.Errorf("unexpected RTAC request: %s %s", r.Method, r.URL.Path)
						}
						if test.shortRead {
							w.Header().Set("Content-Length", fmt.Sprint(len(test.body)+1))
						}
						w.WriteHeader(test.status)
						io.WriteString(w, test.body)
					}))
					defer rtac.Close()

					gen := usGeneration(nil)
					for i := range gen.Manifest.Services {
						service := &gen.Manifest.Services[i]
						for _, role := range service.Roles {
							if role == manifest.RoleRTAC {
								service.Endpoints["api"] = rtac.URL
							}
						}
					}
					s := &Server{}
					var result any
					var err error
					if endpoint.name == "state" {
						result, err = s.fetchRTACState(t.Context(), gen)
					} else {
						result, err = s.fetchRTACAudit(t.Context(), gen)
					}
					if test.wantError != "" {
						if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", test.status)) ||
							!strings.Contains(err.Error(), test.wantError) {
							t.Fatalf("error = %v, want HTTP %d and %q", err, test.status, test.wantError)
						}
						return
					}
					if err != nil {
						t.Fatalf("fetch RTAC: %v", err)
					}
					if !reflect.DeepEqual(result, endpoint.want) {
						t.Errorf("result = %#v, want %#v", result, endpoint.want)
					}
				})
			}
		})
	}
}
