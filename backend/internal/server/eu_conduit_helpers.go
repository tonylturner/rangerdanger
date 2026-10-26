package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/labs"
	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

func conduitDenyMatrix(gen *lifecycle.Generation, prior error) ([]conduitDeny, error) {
	var rows []conduitDeny
	for _, source := range []struct{ node, network string }{{"kali-1", "enterprise_net"}, {"vendor-jump-1", "vendor_net"}, {"eng-ws-1", "vendor_net"}} {
		from, err := nodeIP(gen, source.node, source.network)
		if err != nil {
			prior = errors.Join(prior, err)
			continue
		}
		for _, target := range []struct {
			node, network string
			port          int
		}{
			{"breaker-1", "field_net", 502}, {"f2-breaker", "field_net", 502}, {"rmu-1", "field_net", 502}, {"rmu-2", "field_net", 502}, {"oltc-1", "field_net", 502},
			{"rtu-1", "station_net", 2404}, {"historian-1", "ops_dmz_net", 8081},
		} {
			to, err := nodeIP(gen, target.node, target.network)
			if err != nil {
				prior = errors.Join(prior, err)
				continue
			}
			rows = append(rows, conduitDeny{from, to, target.port})
		}
		if source.node != "eng-ws-1" {
			to, err := nodeIP(gen, "plc-1", "station_net")
			if err != nil {
				prior = errors.Join(prior, err)
			} else {
				rows = append(rows, conduitDeny{from, to, 8080})
			}
		}
	}
	return rows, prior
}

// Python is baked into both source images. No curl/tool fallback is used:
// missing tooling or exec is missing evidence, never a blocked-flow pass.
const conduitHTTPGet = `import http.client,json,sys
c=http.client.HTTPConnection(sys.argv[2],int(sys.argv[3]),timeout=3,source_address=(sys.argv[1],0))
c.request("GET",sys.argv[4]); r=c.getresponse(); b=r.read(1048576)
print(json.dumps({"status":r.status,"body":b.decode("utf-8")}))
c.close()`

func (s *Server) conduitRead(ctx context.Context, gen *lifecycle.Generation, fromNode, fromNetwork, toNode, toNetwork, path, name string) ValidationCheck {
	fail := func(err error) ValidationCheck { return checkFail(name, "missing evidence: "+err.Error()) }
	from, err := nodeService(gen, fromNode)
	if err != nil {
		return fail(err)
	}
	fromIP, err := interfaceIP(from, fromNetwork)
	if err != nil {
		return fail(err)
	}
	toIP, err := nodeIP(gen, toNode, toNetwork)
	if err != nil {
		return fail(err)
	}
	if s.execInContainer == nil {
		return fail(errors.New("container exec unavailable"))
	}
	port := 8080
	began := time.Now().UTC()
	stdout, stderr, rc, err := s.execInContainer(ctx, from.Container, []string{"python3", "-c", conduitHTTPGet, fromIP, toIP, fmt.Sprint(port), path}, 5)
	if err != nil {
		return fail(err)
	}
	if rc != 0 {
		return fail(fmt.Errorf("GET from %s to %s:%d exited %d: %s", fromIP, toIP, port, rc, stderr))
	}
	var response struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal([]byte(stdout), &response); err != nil {
		return fail(fmt.Errorf("decode GET evidence: %w", err))
	}
	if response.Status != 200 || response.Body == "" {
		return fail(fmt.Errorf("GET returned HTTP %d or empty body", response.Status))
	}
	inventoryDetail := ""
	if toNode == "historian-1" {
		inventory := gen.Package.StationPoints
		count := len(inventory)
		if count == 0 {
			return fail(errors.New("active package station inventory unavailable (0 points)"))
		}
		var points []labs.IEC104Point
		if err := json.Unmarshal([]byte(response.Body), &points); err != nil {
			return fail(fmt.Errorf("historian GET could not decode the %d-point station inventory: %w", count, err))
		}
		if len(points) != count {
			return fail(fmt.Errorf("historian GET returned %d points; expected the full %d-point station inventory", len(points), count))
		}
		expected := make(map[int]string, count)
		for _, point := range inventory {
			expected[point.IOA] = point.ID
		}
		seen := map[int]bool{}
		for _, point := range points {
			if point.ID == "" || seen[point.IOA] || expected[point.IOA] != point.ID {
				return fail(fmt.Errorf("historian GET returned missing, duplicate or unknown point identities; expected the full %d-point station inventory", count))
			}
			seen[point.IOA] = true
		}
		inventoryDetail = fmt.Sprintf("; full %d-point station inventory verified", count)
	}
	return checkPass(name, fmt.Sprintf("GET %s:%d%s from teaching source %s returned HTTP 200 at %s (completed %s)%s; no write performed", toIP, port, path, fromIP, began.Format(time.RFC3339Nano), time.Now().UTC().Format(time.RFC3339Nano), inventoryDetail))
}

// GI forms are deliberately untimed and overwrite source_time in the CC
// cache. Prove the clock conduit with an actual HTTP operation from the RTU,
// not a race against the next GI or an invented timestamp in observed state.
func (s *Server) conduitClockRead(ctx context.Context, gen *lifecycle.Generation) ValidationCheck {
	const name = "RTU station-clock HTTP operation"
	fail := func(err error) ValidationCheck { return checkFail(name, "missing evidence: "+err.Error()) }
	rtu, err := nodeService(gen, "rtu-1")
	if err != nil {
		return fail(err)
	}
	gps, err := nodeIP(gen, "gps-1", "station_net")
	if err != nil {
		return fail(err)
	}
	if s.execInContainer == nil {
		return fail(errors.New("container exec unavailable"))
	}
	began := time.Now().UTC()
	stdout, stderr, rc, err := s.execInContainer(ctx, rtu.Container, []string{"wget", "-q", "-O", "-", "-T", "3", "http://" + gps + ":8080/api/state"}, 5)
	if err != nil {
		return fail(err)
	}
	if rc != 0 {
		return fail(fmt.Errorf("RTU GPS GET exited %d: %s", rc, stderr))
	}
	var clock struct {
		SyncStatus   string    `json:"sync_status"`
		ReportedTime time.Time `json:"reported_time"`
	}
	if err := json.Unmarshal([]byte(stdout), &clock); err != nil {
		return fail(err)
	}
	ended := time.Now().UTC()
	if clock.SyncStatus != "locked" || !insideEvidenceWindow(clock.ReportedTime, began.Add(-5*time.Second), ended.Add(5*time.Second)) {
		return fail(errors.New("GPS must return locked and current reported_time; stale/offset/unknown is not a healthy clock"))
	}
	return checkPass(name, fmt.Sprintf("RTU GET http://%s:8080/api/state completed %s, locked time %s; same-subnet operation, not firewall enforcement", gps, ended.Format(time.RFC3339Nano), clock.ReportedTime.Format(time.RFC3339Nano)))
}
