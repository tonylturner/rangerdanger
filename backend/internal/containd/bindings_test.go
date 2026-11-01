package containd

import (
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/tturner/rangerdanger/backend/internal/containd/containdtest"
)

func TestMergeApplianceBindings(t *testing.T) {
	running := []byte(`{"interfaces":[{"name":"lan3","device":"eth8"},{"name":"wan","device":"eth2"},{"name":"removed","device":"eth1"}]}`)
	policy := []byte(`{"interfaces":[{"name":"wan","device":"wrong","zone":"new"},{"name":"lan3","access":{"mgmt":true}},{"name":"new","device":"eth99"}],"firewall":{"rules":[]}}`)
	merged, err := mergeApplianceBindings(running, policy)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	json.Unmarshal(merged, &got)
	want := []any{
		map[string]any{"name": "wan", "device": "eth2", "zone": "new"},
		map[string]any{"name": "lan3", "device": "eth8", "access": map[string]any{"mgmt": true}},
		map[string]any{"name": "new"},
	}
	if !reflect.DeepEqual(got["interfaces"], want) {
		t.Fatalf("interfaces = %s", merged)
	}
	before, _ := FirewallHashFromBytes(policy)
	after, _ := FirewallHashFromBytes(merged)
	if before != after {
		t.Fatal("appliance binding changed policy identity")
	}
}

func TestImportPreservesBindingsAndOrder(t *testing.T) {
	var calls []string
	srv := containdtest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api/v1/config":
			io.WriteString(w, `{"interfaces":[{"name":"lan3","device":"eth8"}]}`)
		case "/api/v1/config/candidate":
			body, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(body), `"device":"eth8"`) {
				t.Error("candidate lost binding")
			}
		}
	}))
	defer srv.Close()
	_, err := newTestClient(srv.URL).ImportConfig(t.Context(), []byte(`{"interfaces":[{"name":"lan3","zone":"lan3"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"GET /api/v1/config", "POST /api/v1/config/candidate", "POST /api/v1/config/commit"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v", calls)
	}
}

func TestImportFailsClosedWithoutRunningConfig(t *testing.T) {
	for _, body := range []string{"invalid", "", "null", "[]", `{"interfaces":{}}`} {
		t.Run(body, func(t *testing.T) {
			srv := containdtest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Error("staged without running config")
				}
				io.WriteString(w, body)
			}))
			defer srv.Close()
			if _, err := newTestClient(srv.URL).ImportConfig(t.Context(), []byte(`{"interfaces":[{"name":"lan3","zone":"lan3"}]}`)); err == nil {
				t.Fatal("accepted unreadable running config")
			}
		})
	}
}
