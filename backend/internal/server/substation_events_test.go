package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNetworkEventsQueriesCurrentPolicyWindowAndPages(t *testing.T) {
	boundary := time.Now().UTC().Add(-time.Minute)
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("since") != boundary.Format(time.RFC3339Nano) {
			t.Errorf("missing current-policy window: %s", r.URL)
		}
		if r.URL.Query().Get("cursor") == "" {
			// A page of irrelevant management traffic must not hide the
			// next page's teaching-zone event.
			io.WriteString(w, `{"events":[{"id":4,"srcIp":"10.99.99.10"}],"nextCursor":"older","historyTruncated":true}`)
		} else {
			fmt.Fprintf(w, `{"events":[{"id":3,"kind":"response","proto":"modbus","srcIp":"10.40.40.20","timestamp":%q}],"historyTruncated":false}`, boundary.Format(time.RFC3339Nano))
		}
	}))
	defer upstream.Close()
	s := newTestServer(t, upstream.URL)
	s.policyChangedAt = boundary
	rec, body := invoke(s, s.handleSubstationNetworkEvents, "GET", "/events", nil)
	var result struct {
		Events []struct {
			ID string `json:"id"`
		} `json:"events"`
	}
	json.Unmarshal(rec.Body.Bytes(), &result)
	if rec.Code != 200 || calls != 2 || len(result.Events) != 1 || result.Events[0].ID != "3" || body["historyTruncated"] != true {
		t.Fatalf("calls=%d response=%s", calls, rec.Body)
	}
}

func TestNetworkEventsSurfacesEmptyEngineFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"events":[],"historyTruncated":true,"engineError":"engine down"}`)
	}))
	defer upstream.Close()
	s := newTestServer(t, upstream.URL)
	rec, body := invoke(s, s.handleSubstationNetworkEvents, "GET", "/events", nil)
	if rec.Code != 200 || body["engineError"] != "engine down" || body["historyTruncated"] != true {
		t.Fatalf("lost diagnostics: %s", rec.Body)
	}
}
