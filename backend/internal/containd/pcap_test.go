package containd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestStartPcapSelectorsAndPortAreEphemeral(t *testing.T) {
	want := PcapConfig{Enabled: true, Zones: []string{"lan2"}, Interfaces: []string{"lan1"}, Filter: PcapFilter{Proto: "tcp", Port: "2404"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeader(t, r)
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/pcap/start" {
			t.Errorf("capture staged/persisted config: %s %s", r.Method, r.URL)
			http.NotFound(w, r)
			return
		}
		var got PcapConfig
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("override = %+v, want %+v", got, want)
		}
		json.NewEncoder(w).Encode(PcapStatus{Running: true, Interfaces: []string{"eth2", "eth8"}})
	}))
	defer srv.Close()
	status, err := newTestClient(srv.URL).StartPcap(t.Context(), &want)
	if err != nil || !status.Running || !reflect.DeepEqual(status.Interfaces, []string{"eth2", "eth8"}) {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}

func TestStartPcapAlreadyRunning(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		code             int
	}{
		{"engine running", `{"error":"engine pcap start status 400: pcap capture already running"}`, "capture already in progress", 400},
		{"conflict", `{"error":"conflict"}`, "capture already in progress", 409},
		{"invalid zone", `{"error":"pcap zone nosuch has no configured interfaces"}`, "start pcap returned 400:", 400},
		{"other failure", `{"error":"pcap capture already running"}`, "start pcap returned 500:", 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			_, err := newTestClient(srv.URL).StartPcap(t.Context(), nil)
			if err == nil || !strings.HasPrefix(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}
