package containdtest

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
)

func TestRevisionedTransactionFixture(t *testing.T) {
	srv := NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "")
	}))
	defer srv.Close()
	request := func(method, path, revision string, want int) (http.Header, map[string]any) {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		if revision != "" {
			req.Header.Set("X-Containd-If-Revision", revision)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("%s = %d, want %d", path, resp.StatusCode, want)
		}
		var body map[string]any
		if data, err := io.ReadAll(resp.Body); err != nil {
			t.Fatal(err)
		} else if len(data) > 0 {
			if err := json.Unmarshal(data, &body); err != nil {
				t.Fatal(err)
			}
		}
		return resp.Header, body
	}
	for _, path := range []string{"/api/v1/config/candidate", "/api/v1/config/commit"} {
		request("POST", path, "", 428)
	}
	read, _ := request("GET", "/api/v1/config", "", 200)
	stage, _ := request("POST", "/api/v1/config/candidate", read.Get("X-Containd-Revision"), 200)
	request("POST", "/api/v1/config/commit", read.Get("X-Containd-Revision"), 412)
	_, commit := request("POST", "/api/v1/config/commit", stage.Get("X-Containd-Revision"), 200)
	if commit["applyId"] == "" || commit["protection"].(map[string]any)["state"] != "pending" {
		t.Fatalf("invalid commit: %v", commit)
	}
	_, protection := request("GET", "/api/v1/dataplane/protection", "", 200)
	if protection["applyId"] != commit["applyId"] || protection["installedApplyId"] != commit["applyId"] || protection["state"] != "protecting" {
		t.Fatalf("invalid readiness: %v", protection)
	}
}
