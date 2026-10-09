package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tturner/rangerdanger/backend/internal/config"
	"github.com/tturner/rangerdanger/backend/internal/containd"
	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

const euPackageID = "eu-iec104-substation"

// euStartedAt is the start of every EU test generation.
var euStartedAt = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

// euGeneration is a generation of the real EU package. endpoints replaces
// the base URL of every endpoint of a service, by service key, with a test
// server; client is the firewall API.
func euGeneration(t *testing.T, client *containd.Client, endpoints map[string]string) *lifecycle.Generation {
	t.Helper()
	man, err := manifest.Load(repoRoot(), euPackageID)
	if err != nil {
		t.Fatal(err)
	}
	for i, svc := range man.Services {
		base, ok := endpoints[svc.Key]
		if !ok {
			continue
		}
		replaced := map[string]string{}
		for name := range svc.Endpoints {
			replaced[name] = base
		}
		man.Services[i].Endpoints = replaced
	}
	pkg, err := labs.ReadDefinition(filepath.Join(repoRoot(), "lab-definitions"), filepath.Join(manifest.Dir(repoRoot(), euPackageID), "package.yml"))
	if err != nil {
		t.Fatal(err)
	}
	return lifecycle.NewGeneration(7, pkg, man, client, euStartedAt)
}

// rangeServer is a server whose ready range is gen, with activeConfig set.
func rangeServer(gen *lifecycle.Generation, activeConfig string) *Server {
	gin.SetMode(gin.TestMode)
	return &Server{
		engine: gin.New(),
		cfg:    &config.Config{LabDefinitionsPath: filepath.Join(repoRoot(), "lab-definitions")},
		rng: &fakeRange{
			gen:    gen,
			status: lifecycle.Status{Generation: gen.ID, Phase: lifecycle.PhaseReady, Package: gen.Package.ID, Mode: manifest.ModeSource},
			active: gen.Package.ID,
		},
		activeConfig: activeConfig,
	}
}

// callRoute serves one request to handler, registered range-bound at pattern.
func callRoute(s *Server, handler gin.HandlerFunc, method, pattern, target, body string) *httptest.ResponseRecorder {
	router := gin.New()
	router.Handle(method, pattern, s.rangeBound(), handler)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

// upstreamStub is a test range service answering fixed routes; any other
// request fails the test.
type upstreamStub struct {
	t        *testing.T
	routes   map[string]stubResponse // "METHOD /path?query" → response
	requests []string
	bodies   []string
}

type stubResponse struct {
	status int
	body   string
}

func newUpstreamStub(t *testing.T, routes map[string]stubResponse) (*upstreamStub, *httptest.Server) {
	stub := &upstreamStub{t: t, routes: routes}
	srv := httptest.NewServer(stub)
	t.Cleanup(srv.Close)
	return stub, srv
}

func (u *upstreamStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	key := r.Method + " " + r.URL.RequestURI()
	u.requests = append(u.requests, key)
	body := new(strings.Builder)
	if r.Body != nil {
		buf := make([]byte, 4096)
		for {
			n, err := r.Body.Read(buf)
			body.Write(buf[:n])
			if err != nil {
				break
			}
		}
	}
	u.bodies = append(u.bodies, body.String())
	resp, ok := u.routes[key]
	if !ok {
		u.t.Errorf("unexpected upstream request %s", key)
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.status)
	_, _ = w.Write([]byte(resp.body))
}

// closedURL is the address of a server that is no longer listening.
func closedURL(t *testing.T) string {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	return url
}
