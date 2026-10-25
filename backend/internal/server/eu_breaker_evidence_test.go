package server

import (
	"strings"
	"testing"
	"time"

	dockertypes "github.com/docker/docker/api/types"
)

func TestEURTUPeerEvidenceStreamsPastEightMiB(t *testing.T) {
	const noise = "set-gateway: default gateway set to 10.30.30.2\n"
	prefix := strings.Repeat(noise, (8<<20)/len(noise)+1)
	log := prefix + "2026-10-09T12:00:00Z rtu listening 0.0.0.0:2404\n"
	if len(log) <= 8<<20 {
		t.Fatalf("test log is only %d bytes", len(log))
	}
	if err := euRTUPeersReader(strings.NewReader(log), "10.60.60.20", time.Time{}, time.Time{}, at("12:00:30"), at("12:05:00")); err != nil {
		t.Fatalf("large retained log failed evidence parsing: %v", err)
	}
}

func TestEURTUPeerEvidenceScopesCommandsToTheActiveGeneration(t *testing.T) {
	log := "2026-10-09T11:59:00Z rtu listening 0.0.0.0:2404\n" +
		"2026-10-09T12:00:10Z rtu command ioa=3001 peer=10.10.10.10:1234 phase=execute result=accepted\n"
	err := euRTUPeersReader(strings.NewReader(log), "10.60.60.20", at("11:59:00"), at("12:00:20"), at("12:00:30"), at("12:05:00"))
	if err != nil {
		t.Fatalf("pre-generation command affected current evidence: %v", err)
	}
}

func TestEURTUContainerIdentityRequiresAnUnchangedIncarnation(t *testing.T) {
	hardeningSince := at("12:00:30")
	inspect := dockertypes.ContainerJSON{
		ContainerJSONBase: &dockertypes.ContainerJSONBase{
			ID: "rtu-container",
			State: &dockertypes.ContainerState{
				Running:   true,
				StartedAt: at("11:59:00").Format(time.RFC3339Nano),
			},
		},
	}
	identity, err := euRTUIdentityFromInspect(inspect, hardeningSince)
	if err != nil {
		t.Fatalf("valid container identity rejected: %v", err)
	}
	if identity.id != "rtu-container" || !identity.startedAt.Equal(at("11:59:00")) || identity.restartCount != 0 {
		t.Fatalf("identity = %+v", identity)
	}

	tests := []struct {
		name   string
		mutate func(*dockertypes.ContainerJSON)
	}{
		{"not running", func(i *dockertypes.ContainerJSON) { i.State.Running = false }},
		{"restarting", func(i *dockertypes.ContainerJSON) { i.State.Restarting = true }},
		{"missing ID", func(i *dockertypes.ContainerJSON) { i.ID = "" }},
		{"after hardening", func(i *dockertypes.ContainerJSON) {
			i.State.StartedAt = hardeningSince.Add(time.Second).Format(time.RFC3339Nano)
		}},
		{"restart count", func(i *dockertypes.ContainerJSON) { i.RestartCount = 1 }},
		{"missing start time", func(i *dockertypes.ContainerJSON) { i.State.StartedAt = "not-a-time" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			candidate := inspect
			base := *inspect.ContainerJSONBase
			state := *inspect.State
			candidate.ContainerJSONBase = &base
			candidate.State = &state
			tc.mutate(&candidate)
			if _, err := euRTUIdentityFromInspect(candidate, hardeningSince); err == nil {
				t.Fatal("invalid container identity accepted")
			}
		})
	}
}

func TestEURTUPeerEvidenceRejectsOutOfOrderRecords(t *testing.T) {
	log := "2026-10-09T12:00:00Z rtu listening 0.0.0.0:2404\n" +
		"2026-10-09T12:00:40Z rtu command ioa=3001 peer=10.60.60.20:1234 phase=execute result=accepted\n" +
		"2026-10-09T12:00:35Z rtu command ioa=3001 peer=10.60.60.20:1234 phase=execute result=accepted\n"
	if err := euRTUPeers(log, "10.60.60.20", at("12:00:30"), at("12:05:00")); err == nil || !strings.Contains(err.Error(), "discontinuous") {
		t.Fatalf("out-of-order log error = %v", err)
	}
}
