package containd

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// TestListSessionsConvertsPorts feeds ListSessions containd's wire shape
// (pkg/dp/conntrack.Entry: string ports, omitempty fields).
func TestListSessionsConvertsPorts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAuthHeader(t, r)
		if r.URL.Path != "/api/v1/conntrack" || r.URL.Query().Get("limit") != "5000" {
			t.Errorf("request = %s", r.URL)
		}
		io.WriteString(w, `[
			{"proto":"tcp","state":"ESTABLISHED","src":"10.60.60.20","dst":"10.30.30.20","sport":"40312","dport":"2404","replySrc":"10.30.30.20","timeoutSecs":431999,"assured":true},
			{"proto":"icmp","src":"10.10.10.50","dst":"10.30.30.20","timeoutSecs":29}
		]`)
	}))
	defer srv.Close()

	sessions, err := newTestClient(srv.URL).ListSessions(context.Background(), 5000)
	if err != nil {
		t.Fatal(err)
	}
	sport, dport, state := 40312, 2404, "ESTABLISHED"
	want := []Session{
		{Proto: "tcp", Src: "10.60.60.20", Dst: "10.30.30.20", Sport: &sport, Dport: &dport, State: &state, Timeout: 431999},
		{Proto: "icmp", Src: "10.10.10.50", Dst: "10.30.30.20", Timeout: 29},
	}
	if !reflect.DeepEqual(sessions, want) {
		got, _ := json.Marshal(sessions)
		t.Errorf("sessions = %s", got)
	}
}

func TestListSessionsFailures(t *testing.T) {
	tests := []struct {
		name, body string
		status     int
		want       string
	}{
		{"engine error", `{"error":"engine conntrack status 500"}`, http.StatusBadGateway, "list conntrack returned 502"},
		{"bad port", `[{"proto":"tcp","src":"a","dst":"b","sport":"x","dport":"2404"}]`, http.StatusOK, `sport: invalid port "x"`},
		{"port out of range", `[{"proto":"tcp","src":"a","dst":"b","sport":"1","dport":"70000"}]`, http.StatusOK, `dport: invalid port "70000"`},
		{"not an array", `{"entries":[]}`, http.StatusOK, "decode conntrack"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(test.status)
				io.WriteString(w, test.body)
			}))
			defer srv.Close()
			_, err := newTestClient(srv.URL).ListSessions(context.Background(), 5000)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Errorf("ListSessions error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestKillSession(t *testing.T) {
	tuple := SessionTuple{Proto: "tcp", Src: "10.10.10.50", Dst: "10.30.30.20", Sport: 40312, Dport: 2404}
	tests := []struct {
		name     string
		status   int
		body     string
		notFound bool
		wantErr  string
	}{
		{"deleted", http.StatusOK, `{"status":"deleted"}`, false, ""},
		{"not found", http.StatusBadGateway, `{"error":"engine conntrack delete status 500: {\"error\":\"conntrack delete: not found\"}"}`, true, "not found"},
		{"other engine failure", http.StatusBadGateway, `{"error":"engine conntrack delete status 500: permission denied"}`, false, "returned 502"},
		{"unsupported", http.StatusNotImplemented, `{"error":"conntrack delete not supported"}`, false, "returned 501"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assertAuthHeader(t, r)
				if r.Method != http.MethodPost || r.URL.Path != "/api/v1/conntrack/kill" {
					t.Errorf("request = %s %s", r.Method, r.URL)
				}
				body, _ := io.ReadAll(r.Body)
				if string(body) != `{"proto":"tcp","src":"10.10.10.50","dst":"10.30.30.20","sport":40312,"dport":2404}` {
					t.Errorf("kill body = %s", body)
				}
				w.WriteHeader(test.status)
				io.WriteString(w, test.body)
			}))
			defer srv.Close()
			err := newTestClient(srv.URL).KillSession(context.Background(), tuple)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("KillSession = %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("KillSession error = %v, want %q", err, test.wantErr)
			}
			if errors.Is(err, ErrSessionNotFound) != test.notFound {
				t.Errorf("errors.Is(ErrSessionNotFound) = %v, want %v", !test.notFound, test.notFound)
			}
		})
	}
}

func TestListSessionsResponses(t *testing.T) {
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
			sessions, err := newTestClient(srv.URL).ListSessions(context.Background(), 5000)
			if (err != nil) != tc.fail {
				t.Fatalf("error = %v, want failure %v", err, tc.fail)
			}
			if tc.fail {
				return
			}
			if sessions == nil || len(sessions) != tc.count {
				t.Fatalf("sessions = %#v, want non-nil array of length %d", sessions, tc.count)
			}
			if tc.name == "zero and max ports" {
				if sessions[0].Sport == nil || *sessions[0].Sport != 0 || sessions[0].Dport == nil || *sessions[0].Dport != 65535 {
					t.Fatalf("port boundaries not preserved: %#v", sessions[0])
				}
			}

		})
	}
}

func TestListSessionsTransportFailure(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	if _, err := newTestClient(srv.URL).ListSessions(context.Background(), 5000); err == nil {
		t.Fatal("expected transport error")
	}
}
