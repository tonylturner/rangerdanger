// SPDX-License-Identifier: Apache-2.0
package fielddevice

import "testing"

func TestAuditRingAndZoneMapping(t *testing.T) {
	m, _ := newTestModel(t, Breaker)
	for range 510 {
		write(t, m, 0, 1)
	}
	entries, latest, oldest := m.AuditAfter(0)
	if len(entries) != AuditCapacity || latest == nil || oldest == nil || *latest != 510 || *oldest != 11 {
		t.Fatalf("audit len=%d latest=%v oldest=%v", len(entries), latest, oldest)
	}
	if after, _, _ := m.AuditAfter(509); len(after) != 1 || after[0].Seq != 510 {
		t.Fatal("bad audit cursor")
	}
	zones, err := ParseZones("field=10.40.40.0/24,station=10.30.30.0/24,process=10.50.50.0/24,mgmt=10.99.99.0/24,specific=10.40.40.16/28")
	if err != nil {
		t.Fatal(err)
	}
	for peer, want := range map[string]string{"10.40.40.20:502": "specific", "10.40.40.1:502": "field", "10.30.30.20:502": "station", "10.50.50.20:8080": "process", "10.99.99.22:8080": "mgmt", "192.0.2.1:1": "unknown", "[::1]:1": "unknown", "invalid": "unknown"} {
		if got := zones.ForPeer(peer); got != want {
			t.Errorf("zone %s=%s want %s", peer, got, want)
		}
	}
	for _, raw := range []string{"", "field=invalid", "field=::1/128", "field=10.0.0.0/8,field=10.1.0.0/16"} {
		if _, err := ParseZones(raw); err == nil {
			t.Errorf("bad zones accepted: %s", raw)
		}
	}
}
