package server

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestHistorianProxy(t *testing.T) {
	query := "?point=oltc-1-tap&since=2026-10-15T10%3A00%3A00Z&until=2026-10-15T11%3A00%3A00Z"
	history := `{"point":"oltc-1-tap","segments":[{"cache_epoch":"cc1","samples":[{"value":3,"quality":{"iv":true},"stale_reason":"link"}]}],"gaps":[{"from":"2026-10-15T10:00:00Z","to":"2026-10-15T10:00:10Z"}]}`
	points := `[{"id":"oltc-1-tap","ioa":1301,"asdu":"M_ST_TB_1","last":null}]`
	tests := []struct {
		name, path, upstreamPath string
		handler                  func(*Server) gin.HandlerFunc
		upstream                 stubResponse
		code                     int
		want                     string
	}{
		{"points", "/points", "/api/points", func(s *Server) gin.HandlerFunc { return s.handleHistorianPoints },
			stubResponse{200, points}, 200, points},
		{"empty points", "/points", "/api/points", func(s *Server) gin.HandlerFunc { return s.handleHistorianPoints },
			stubResponse{200, `[]`}, 200, `[]`},
		{"history", "/history" + query, "/api/history" + query, func(s *Server) gin.HandlerFunc { return s.handleHistorianHistory },
			stubResponse{200, history}, 200, history},
		{"invalid query", "/history", "/api/history", func(s *Server) gin.HandlerFunc { return s.handleHistorianHistory },
			stubResponse{400, `{"error":"point is required"}`}, 400, `{"error":"point is required"}`},
		{"unknown point", "/history?point=unknown", "/api/history?point=unknown", func(s *Server) gin.HandlerFunc { return s.handleHistorianHistory },
			stubResponse{404, `{"error":"unknown point"}`}, 404, `{"error":"unknown point"}`},
		{"malformed not found", "/history?point=unknown", "/api/history?point=unknown", func(s *Server) gin.HandlerFunc { return s.handleHistorianHistory },
			stubResponse{404, `not found`}, 502, `{"error":"historian returned a body that is not a JSON object"}`},
		{"upstream failure", "/points", "/api/points", func(s *Server) gin.HandlerFunc { return s.handleHistorianPoints },
			stubResponse{503, `down`}, 502, `{"error":"historian returned 503: down"}`},
		{"not array", "/points", "/api/points", func(s *Server) gin.HandlerFunc { return s.handleHistorianPoints },
			stubResponse{200, `{}`}, 502, `{"error":"historian returned an invalid read response"}`},
		{"null points", "/points", "/api/points", func(s *Server) gin.HandlerFunc { return s.handleHistorianPoints },
			stubResponse{200, `null`}, 502, `{"error":"historian returned an invalid read response"}`},
		{"malformed history", "/history", "/api/history", func(s *Server) gin.HandlerFunc { return s.handleHistorianHistory },
			stubResponse{200, `{"segments":`}, 502, `{"error":"historian returned an invalid read response"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stub, upstream := newUpstreamStub(t, map[string]stubResponse{
				"GET /historian-1" + test.upstreamPath: test.upstream,
			})
			s := rangeServer(multiDeviceGeneration(t, upstream.URL), "weak")
			pattern := "/api/historian/points"
			if test.path != "/points" {
				pattern = "/api/historian/history"
			}
			rec := callRoute(s, test.handler(s), http.MethodGet, pattern, "/api/historian"+test.path, "")
			if rec.Code != test.code || rec.Body.String() != test.want {
				t.Fatalf("proxy = %d %s; want %d %s", rec.Code, rec.Body, test.code, test.want)
			}
			if !reflect.DeepEqual(stub.requests, []string{"GET /historian-1" + test.upstreamPath}) {
				t.Fatalf("requests = %q", stub.requests)
			}
		})
	}
}
