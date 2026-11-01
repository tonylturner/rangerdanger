package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestContaindProxyPreservesRevisionContract(t *testing.T) {
	for _, code := range []int{200, 400, 412, 428} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v1/config/candidate" || r.Header.Get("X-Containd-If-Revision") != `"opaque-client-revision"` {
					t.Errorf("proxy changed write: %s %v", r.URL.Path, r.Header)
				}
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("X-Containd-Revision", `"opaque-server-revision"`)
				w.WriteHeader(code)
				fmt.Fprint(w, `{"code":"unchanged-upstream-body"}`)
			}))
			defer upstream.Close()
			s := newTestServer(t, upstream.URL)
			router := gin.New()
			router.POST("/api/containd/*path", s.rangeBound(), s.handleProxyContaind)
			req := httptest.NewRequest("POST", "/api/containd/api/v1/config/candidate", nil)
			req.Header.Set("X-Containd-If-Revision", `"opaque-client-revision"`)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != code || rec.Header().Get("X-Containd-Revision") != `"opaque-server-revision"` ||
				rec.Body.String() != `{"code":"unchanged-upstream-body"}` {
				t.Fatalf("proxy changed response: %d %v %s", rec.Code, rec.Header(), rec.Body)
			}
		})
	}
}
