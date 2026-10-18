// SPDX-License-Identifier: Apache-2.0
package fielddevice

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestConcurrentAdaptersShareAtomicCore(t *testing.T) {
	for _, kind := range []Kind{Breaker, RMU, OLTC} {
		t.Run(string(kind), func(t *testing.T) {
			id := map[Kind]string{Breaker: "feeder-breaker", RMU: "rmu-1", OLTC: "oltc-1"}[kind]
			m, err := NewModel(Config{Kind: kind, DeviceID: id, RMUNormal: PositionClosed, Generation: 42})
			if err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			for worker := range 4 {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for seq := range 25 {
						snapshot := m.Snapshot()
						input := MeasurementInput{Schema: 3, DeviceID: id, Generation: 42, Revision: snapshot.Revision, SolverEpoch: "shared", SolveSeq: uint64(worker*25 + seq + 1), SolvedAt: time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC).Add(time.Duration(worker*25+seq+1) * time.Second).Format(time.RFC3339Nano), CurrentA: 1, VoltageKV: 20, SectionLive: true}
						_, err := m.ApplyMeasurements(context.Background(), input, testActor)
						if err != nil && err != MeasurementReplay && err != MeasurementStaleTruth {
							t.Errorf("unexpected delivery error: %v", err)
						}
						m.WriteHolding(context.Background(), 0, 1, testActor)
						if _, err := m.Action(context.Background(), "reset", labActor); err != nil {
							t.Errorf("reset: %v", err)
						}
						m.AuditAfter(0)
					}
				}()
			}
			wg.Wait()
			final := m.Snapshot()
			if final.Revision < 101 || final.Schema != 3 || final.Generation != 42 {
				t.Fatalf("lost atomic revisions: %+v", final.Truth)
			}
		})
	}
}
