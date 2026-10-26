package containd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
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
