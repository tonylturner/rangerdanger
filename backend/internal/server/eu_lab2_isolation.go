package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"

	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

type isolationTarget struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	Path string `json:"path"`
}

// Do not bind the teaching source: that would conceal an accidental management
// leg. Transport failures are structured evidence; missing tools/exec, invalid
// requests and protocol failures must not turn into a successful deny check.
const isolationHTTP = `import concurrent.futures,errno,http.client,json,sys
def probe(t):
    c=http.client.HTTPConnection(t["host"],t["port"],timeout=3)
    r=dict(t,status=0,blocked=False,connected=False)
    try:
        c.connect(); r["connected"]=True
        c.request("GET",t["path"]); response=c.getresponse()
        r["status"]=response.status
    except OSError as e:
        if not r["connected"] and (isinstance(e,TimeoutError) or e.errno in (errno.ECONNREFUSED,errno.EHOSTUNREACH,errno.ENETUNREACH,errno.ETIMEDOUT)):
            r["blocked"]=True
        else:
            raise
    finally:
        c.close()
    return r
targets=json.loads(sys.argv[1])
with concurrent.futures.ThreadPoolExecutor(max_workers=len(targets)) as pool:
    print(json.dumps(list(pool.map(probe,targets))))`

func isolationTargets(gen *lifecycle.Generation) ([]isolationTarget, error) {
	// Platform services are not part of a range manifest.
	targets := []isolationTarget{
		{"10.99.99.3", 8080, "/"}, {"10.99.99.10", 8080, "/api/health"}, {"10.99.99.11", 3000, "/"},
	}
	for _, svc := range gen.Manifest.Services {
		for _, iface := range svc.Interfaces {
			if iface.Network != "mgmt_net" {
				continue
			}
			ports := []int{8080}
			for _, endpoint := range svc.Endpoints {
				u, err := url.Parse(endpoint)
				if err != nil {
					return nil, fmt.Errorf("invalid endpoint for %s: %w", svc.Key, err)
				}
				port := 80
				if u.Port() != "" {
					port, err = strconv.Atoi(u.Port())
					if err != nil {
						return nil, fmt.Errorf("invalid endpoint port for %s: %w", svc.Key, err)
					}
				}
				if !slices.Contains(ports, port) {
					ports = append(ports, port)
				}
			}
			for _, port := range ports {
				targets = append(targets, isolationTarget{iface.IPv4, port, "/"})
			}
		}
	}
	for _, host := range []string{"10.98.1.2", "10.98.2.2", "10.98.3.2"} {
		for _, path := range []string{"/", "/api/", "/containd/", "/apps/vendor-jump/", "/apps/eng-ws/", "/apps/jumpbox"} {
			targets = append(targets, isolationTarget{host, 8080, path})
		}
	}
	return targets, nil
}

func (s *Server) conduitIsolation(ctx context.Context, gen *lifecycle.Generation, node string) ValidationCheck {
	name := "Full network isolation: " + node
	fail := func(err error) ValidationCheck { return checkFail(name, "missing isolation evidence: "+err.Error()) }
	svc, err := nodeService(gen, node)
	if err != nil {
		return fail(err)
	}
	if s.execInContainer == nil {
		return fail(fmt.Errorf("container exec unavailable"))
	}
	ownProxy := map[string]string{"vendor-jump-1": "10.98.2.2", "eng-ws-1": "10.98.3.2"}[node]
	if ownProxy == "" {
		return fail(fmt.Errorf("unknown desktop %q", node))
	}
	targets, err := isolationTargets(gen)
	if err != nil {
		return fail(err)
	}
	input, err := json.Marshal(targets)
	if err != nil {
		return fail(err)
	}
	stdout, stderr, rc, err := s.execInContainer(ctx, svc.Container, []string{"python3", "-c", isolationHTTP, string(input)}, 8)
	if err != nil || rc != 0 {
		return fail(fmt.Errorf("network probe exited %d: %v; %s", rc, err, stderr))
	}
	var evidence []struct {
		isolationTarget
		Status    int  `json:"status"`
		Blocked   bool `json:"blocked"`
		Connected bool `json:"connected"`
	}
	if err := json.Unmarshal([]byte(stdout), &evidence); err != nil {
		return fail(err)
	}
	if len(evidence) != len(targets) {
		return fail(fmt.Errorf("got %d results for %d targets", len(evidence), len(targets)))
	}
	for i, result := range evidence {
		if result.isolationTarget != targets[i] {
			return fail(fmt.Errorf("probe identity mismatch at %d", i))
		}
		// Require a live 403 on the connected delivery leg. A broken proxy
		// or desktop NIC is not a passing isolation result.
		if result.Host == ownProxy {
			if result.Status == 403 && !result.Blocked && result.Connected {
				continue
			}
		} else if result.Blocked && result.Status == 0 && !result.Connected {
			continue
		} else if (result.Host == "10.98.1.2" || result.Host == "10.98.2.2" || result.Host == "10.98.3.2") && result.Status == 403 && !result.Blocked && result.Connected {
			continue
		}
		return checkFail(name, fmt.Sprintf("%s:%d%s: HTTP %d blocked=%t; management must be unreachable and delivery proxy must deny", result.Host, result.Port, result.Path, result.Status, result.Blocked))
	}
	return checkPass(name, fmt.Sprintf("%d unbound HTTP probes: management unreachable; delivery proxy returns 403 including API, apps and redirects", len(targets)))
}
