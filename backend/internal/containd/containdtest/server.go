// Package containdtest provides the revisioned config transaction fixture shared
// by backend tests. Callbacks own config bodies, warnings and injected failures;
// this fixture owns revision preconditions and runtime readiness.
package containdtest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
)

// NewServer wraps a fake's handlers with the mandatory 0.1.39 write/readiness contract.
// Successful empty commits receive a pending RuntimeApplyResponse; the first
// protection read reports the committed revision ready and protecting.
func NewServer(handler http.Handler) *httptest.Server {
	var mu sync.Mutex
	revision := 1
	applyID := ""
	runningRevision := ""
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/api/v1/dataplane/protection" {
			fmt.Fprintf(w, `{"ready":true,"protectingRevision":%q,"desiredRevision":%q,"appliedRevision":%q,"state":"protecting","subsystems":[]}`, runningRevision, runningRevision, runningRevision)
			return
		}
		write := r.Method == http.MethodPost && (r.URL.Path == "/api/v1/config/candidate" || r.URL.Path == "/api/v1/config/commit")
		if write {
			got := r.Header.Get("X-Containd-If-Revision")
			if got == "" {
				w.WriteHeader(http.StatusPreconditionRequired)
				fmt.Fprint(w, `{"code":"config_revision_required"}`)
				return
			}
			if got != strconv.Itoa(revision) {
				w.WriteHeader(http.StatusPreconditionFailed)
				fmt.Fprint(w, `{"code":"config_revision_mismatch"}`)
				return
			}
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		if rec.Code >= 200 && rec.Code < 300 {
			if write {
				revision++
			}
			if r.URL.Path == "/api/v1/config" || write {
				rec.Header().Set("X-Containd-Revision", strconv.Itoa(revision))
			}
			if r.URL.Path == "/api/v1/config/commit" && write {
				applyID = "apply-" + strconv.Itoa(revision)
				runningRevision = "runtime-" + strconv.Itoa(revision)
				if rec.Body.Len() == 0 {
					fmt.Fprintf(rec.Body, `{"status":"committed","applyId":%q,"runningRevision":%q,"protection":{"ready":false,"desiredRevision":%q,"state":"pending","subsystems":[]}}`, applyID, runningRevision, runningRevision)
				}
			}
		}
		for key, values := range rec.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(rec.Code)
		w.Write(rec.Body.Bytes())
	}))
}
