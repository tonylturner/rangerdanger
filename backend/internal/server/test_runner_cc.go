package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/tturner/rangerdanger/backend/internal/lifecycle"
)

const suiteCCPoll = 200 * time.Millisecond

// suiteCCBudget reads the serving package's timers, not the firewall canary's
// unrelated deadline. Allow one reconnect interval, TCP dial (T0), STARTDT
// (T1+1s in the CC), and GI timeout (2*T1), plus two polls for dispatch and
// collection. The EU package currently yields 58.4s. This is a recovery budget,
// not a promise that an unavailable or incorrectly configured conduit recovers.
func suiteCCBudget(path string) (time.Duration, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var config struct {
		Timers struct {
			T0 int `json:"t0_s"`
			T1 int `json:"t1_s"`
		} `json:"timers"`
		Reconnect int `json:"reconnect_interval_ms"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return 0, err
	}
	if config.Timers.T0 <= 0 || config.Timers.T1 <= 0 || config.Reconnect <= 0 {
		return 0, fmt.Errorf("CC preparation requires positive T0, T1 and reconnect timers")
	}
	return time.Duration(config.Reconnect)*time.Millisecond +
		(time.Duration(config.Timers.T0)+3*time.Duration(config.Timers.T1)+1)*time.Second +
		2*suiteCCPoll, nil
}

// executeSuiteCheck is deliberately separate from interactive Execute. Only
// positive EU link expectations need operator preparation; all original
// expectations still go through the ordinary strict checker afterward.
func (s *Server) executeSuiteCheck(ctx context.Context, gen *lifecycle.Generation, expect map[string]any) []StepActionResult {
	if fresh, _ := expect["cc_link_fresh"].(bool); gen.Package.ID == euPackageID && fresh {
		path := filepath.Join(s.cfg.LabDefinitionsPath, "packages", gen.Package.ID, "protocols", "points.json")
		budget, err := suiteCCBudget(path)
		if err != nil {
			return []StepActionResult{{Action: "cc_link_fresh", Detail: "CC preparation timers: " + err.Error()}}
		}
		prepared := prepareSuiteCC(ctx, gen, budget)
		if !prepared.Success {
			return []StepActionResult{prepared}
		}
	}
	return s.executeCheck(ctx, gen, expect)
}

func prepareSuiteCC(parent context.Context, gen *lifecycle.Generation, budget time.Duration) StepActionResult {
	ctx, cancel := context.WithTimeout(parent, budget)
	defer cancel()
	ticker := time.NewTicker(suiteCCPoll)
	defer ticker.Stop()

	result := StepActionResult{Action: "cc_link_fresh", Detail: "CC evidence not collected"}
	requested := false
	var requestedEpoch string
	var requestedAt time.Time
	var after uint64
	var lastStrict string
	var attemptedEpoch string
	var attemptedConnection uint64
	for ctx.Err() == nil {
		state, journal, err := euCC(ctx, gen)
		if err != nil {
			// Preserve the last strict predicate if the deadline interrupts a read.
			if ctx.Err() == nil {
				result.Detail = err.Error()
				if lastStrict != "" {
					result.Detail += "; last strict predicate: " + lastStrict
				}
			}
		} else {
			strict := euLink(state, journal, gen.Package.StationPoints, gen.StartedAt, time.Now().UTC())
			lastStrict = strict.Detail
			result.Detail = strict.Detail
			if !requested && state.Link.State == "started" && state.Link.ConnectionSeq != 0 &&
				(journal.CacheEpoch != attemptedEpoch || state.Link.ConnectionSeq != attemptedConnection) {
				endpoint, _ := controlCentreURL(gen) // euCC already resolved it.
				attemptedEpoch, attemptedConnection = journal.CacheEpoch, state.Link.ConnectionSeq
				requestedAt = time.Now().UTC()
				status, err := requestSuiteGI(ctx, endpoint)
				if err != nil {
					// A lost response may already have queued GI. Never replay it.
					result.Detail += "; request GI: " + err.Error()
					return result
				}
				switch status {
				case http.StatusAccepted, http.StatusConflict:
					// 409 means GI is pending/running: observe, do not requeue.
					requested = true
					requestedEpoch, after = journal.CacheEpoch, journal.LatestSeq
				case http.StatusServiceUnavailable:
					// The link changed between reads and POST. No GI was queued.
					// Wait for a new connection, not another POST every poll.
				default:
					result.Detail += fmt.Sprintf("; request GI returned %d", status)
					return result
				}
			} else if requested {
				// A previously retained GI may already satisfy euLink while the
				// requested GI is still queued. Acceptance is not completion.
				completed := false
				for _, entry := range journal.Entries {
					if entry.Kind == "gi_complete" && entry.Detail.ConnectionSeq != nil &&
						*entry.Detail.ConnectionSeq == state.Link.ConnectionSeq &&
						(journal.CacheEpoch != requestedEpoch || entry.Seq > after) &&
						entry.Seq >= journal.OldestSeq && insideEvidenceWindow(entry.At, requestedAt, time.Now().UTC()) {
						completed = true
					}
				}
				if strict.Status == "pass" && completed && ctx.Err() == nil {
					result.Success = true
					return result
				}
				if !completed {
					result.Detail = "waiting for requested/pending GI completion; last strict predicate: " + result.Detail
				}
			} else {
				result.Detail = "GI not accepted on this connection; last strict predicate: " + result.Detail
			}
		}
		select {
		case <-ctx.Done():
		case <-ticker.C:
		}
	}
	result.Detail = fmt.Sprintf("CC preparation ended (%v; budget %s): %s", ctx.Err(), budget, result.Detail)
	return result
}

// Keep the suite's one-shot POST on its declared endpoint, including on a
// redirect. Do not change the shared operator client or interactive routes.
func requestSuiteGI(parent context.Context, endpoint string) (int, error) {
	ctx, cancel := context.WithTimeout(parent, upstreamReadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint+"/api/gi", nil)
	if err != nil {
		return 0, err
	}
	client := *upstreamClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusConflict && resp.StatusCode != http.StatusServiceUnavailable {
		return resp.StatusCode, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxUpstreamBody+1))
	if err == nil && len(body) > maxUpstreamBody {
		err = fmt.Errorf("CC GI response exceeds %d bytes", maxUpstreamBody)
	}
	if err == nil {
		_, err = jsonObject(controlCentreService, body)
	}
	return resp.StatusCode, err
}
