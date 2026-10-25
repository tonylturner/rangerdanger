package containd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetSessionsResponses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		fail   bool
		count  int
	}{
		{"empty", 200, `[]`, false, 0},
		{"null", 200, `null`, false, 0},
		{"missing ports", 200, `[{"proto":"icmp","src":"10.20.20.10","dst":"10.30.30.20"}]`, false, 1},
		{"zero and max ports", 200, `[{"proto":"udp","sport":"0","dport":"65535"}]`, false, 1},
		{"negative port", 200, `[{"sport":"-1"}]`, true, 0},
		{"large port", 200, `[{"dport":"65536"}]`, true, 0},
		{"invalid port", 200, `[{"sport":"abc"}]`, true, 0},
		{"numeric port", 200, `[{"dport":502}]`, true, 0},
		{"obsolete wrapper", 200, `{"sessions":[]}`, true, 0},
		{"invalid JSON", 200, `[`, true, 0},
		{"upstream failure", 503, `unavailable`, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			sessions, err := newTestClient(srv.URL).GetSessions()
			if (err != nil) != tc.fail {
				t.Fatalf("error = %v, want failure %v", err, tc.fail)
			}
			if tc.fail {
				return
			}
			if sessions == nil || len(sessions) != tc.count {
				t.Fatalf("sessions = %#v, want non-nil array of length %d", sessions, tc.count)
			}
			// Keep the existing public JSON keys, even for unavailable telemetry.
			data, err := json.Marshal(sessions)
			if err != nil {
				t.Fatal(err)
			}
			var rows []map[string]any
			if err := json.Unmarshal(data, &rows); err != nil {
				t.Fatal(err)
			}
			for _, row := range rows {
				for _, key := range []string{"id", "source", "dest", "protocol", "src_port", "dst_port", "start_time", "bytes", "packets"} {
					if _, ok := row[key]; !ok {
						t.Errorf("missing session key %q", key)
					}
				}
			}
		})
	}
}

func TestGetSessionsTransportFailure(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	if _, err := newTestClient(srv.URL).GetSessions(); err == nil {
		t.Fatal("expected transport error")
	}
}
