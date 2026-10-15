// SPDX-License-Identifier: Apache-2.0
package main

import (
	"testing"

	"github.com/tturner/rangerdanger/services/devices/fielddevice"
)

func env(t *testing.T) {
	t.Helper()
	t.Setenv("DEVICE_KIND", "breaker")
	t.Setenv("DEVICE_ID", "feeder-breaker")
	t.Setenv("DEVICE_RMU_NORMAL", "")
	t.Setenv("DEVICE_MODBUS_LISTEN", "10.40.40.20:502")
	t.Setenv("DEVICE_PROCESS_LISTEN", "10.50.50.30:8080")
	t.Setenv("DEVICE_SIM_LISTEN", "10.99.99.22:8080")
	t.Setenv("DEVICE_ZONES", defaultZones)
}
func TestConfigKindsAndIdentities(t *testing.T) {
	for _, tt := range []struct{ kind, id, normal string }{
		{"breaker", "feeder-breaker", ""}, {"rmu", "rmu-1", "closed"}, {"rmu", "rmu-2", "open"}, {"oltc", "oltc-1", ""},
	} {
		t.Run(tt.id, func(t *testing.T) {
			env(t)
			t.Setenv("DEVICE_KIND", tt.kind)
			t.Setenv("DEVICE_ID", tt.id)
			t.Setenv("DEVICE_RMU_NORMAL", tt.normal)
			cfg, err := loadConfig()
			if err != nil {
				t.Fatal(err)
			}
			m, err := fielddevice.NewModel(fielddevice.Config{Kind: cfg.kind, DeviceID: cfg.deviceID, RMUNormal: cfg.normal})
			if err != nil || string(m.Truth().Kind) != tt.kind || m.Truth().DeviceID != tt.id {
				t.Fatalf("config=%+v err=%v", cfg, err)
			}
		})
	}
}
func TestConfigRejectsMissingKindAndInvalidListeners(t *testing.T) {
	for _, tt := range []struct{ key, value string }{
		{"DEVICE_KIND", ""}, {"DEVICE_KIND", "legacy"}, {"DEVICE_ID", ""},
		{"DEVICE_MODBUS_LISTEN", ":502"}, {"DEVICE_PROCESS_LISTEN", "0.0.0.0:8080"}, {"DEVICE_SIM_LISTEN", "localhost:8080"},
		{"DEVICE_ZONES", "field=invalid"},
	} {
		t.Run(tt.key+tt.value, func(t *testing.T) {
			env(t)
			t.Setenv(tt.key, tt.value)
			if _, err := loadConfig(); err == nil {
				t.Fatal("bad configuration accepted")
			}
		})
	}
	for _, normal := range []string{"", "invalid"} {
		env(t)
		t.Setenv("DEVICE_KIND", "rmu")
		t.Setenv("DEVICE_ID", "rmu-1")
		t.Setenv("DEVICE_RMU_NORMAL", normal)
		if _, err := loadConfig(); err == nil {
			t.Fatal("RMU without a valid normal accepted")
		}
	}
}
