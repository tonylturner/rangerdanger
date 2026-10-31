package config

import (
	"os"
	"reflect"
	"testing"
)

var configEnvKeys = []string{
	"RANGERDANGER_HTTP_PORT",
	"RANGERDANGER_DB_PATH",
	"RANGERDANGER_ALLOWED_ORIGINS",
	"RANGERDANGER_LAB_DEFINITIONS_PATH",
	"RANGERDANGER_PACKAGE",
	"RANGERDANGER_ROOT",
	"RANGERDANGER_MODE",
	"OTLAB_HTTP_PORT",
	"OTLAB_DB_PATH",
	"OTLAB_ALLOWED_ORIGINS",
	"OTLAB_LAB_DEFINITIONS_PATH",
	"OTLAB_PACKAGE",
	"OTLAB_ROOT",
	"OTLAB_MODE",
}

// isolateConfigEnv prevents the developer's shell environment from influencing
// Load while restoring it after any legacy variables have been promoted.
func isolateConfigEnv(t *testing.T) {
	t.Helper()
	original := make(map[string]string, len(configEnvKeys))
	wasSet := make(map[string]bool, len(configEnvKeys))
	for _, key := range configEnvKeys {
		original[key], wasSet[key] = os.LookupEnv(key)
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}
	t.Cleanup(func() {
		for _, key := range configEnvKeys {
			if wasSet[key] {
				_ = os.Setenv(key, original[key])
			} else {
				_ = os.Unsetenv(key)
			}
		}
	})
}

func TestLoadEnvironmentAndLegacyPrecedence(t *testing.T) {
	isolateConfigEnv(t)
	for key, value := range map[string]string{
		"RANGERDANGER_HTTP_PORT":            "9123",
		"RANGERDANGER_DB_PATH":              "var/test.sqlite",
		"RANGERDANGER_ALLOWED_ORIGINS":      "https://portal.example",
		"RANGERDANGER_LAB_DEFINITIONS_PATH": "fixtures/labs",
		"RANGERDANGER_PACKAGE":              "eu-iec104-substation",
		"RANGERDANGER_ROOT":                 "/srv/rangerdanger",
		"RANGERDANGER_MODE":                 "release",
		"OTLAB_HTTP_PORT":                   "9999",
		"OTLAB_DB_PATH":                     "legacy.sqlite",
		"OTLAB_ALLOWED_ORIGINS":             "https://legacy.example",
		"OTLAB_LAB_DEFINITIONS_PATH":        "legacy-labs",
		"OTLAB_PACKAGE":                     "legacy-package",
		"OTLAB_ROOT":                        "/legacy",
		"OTLAB_MODE":                        "source",
	} {
		t.Setenv(key, value)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := &Config{
		HTTPPort:           9123,
		DBPath:             "var/test.sqlite",
		AllowedOrigins:     []string{"https://portal.example"},
		LabDefinitionsPath: "fixtures/labs",
		Package:            "eu-iec104-substation",
		Root:               "/srv/rangerdanger",
		Mode:               "release",
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("Load() = %#v, want %#v", cfg, want)
	}
}

// setRange sets the two variables the platform Compose file always provides.
func setRange(t *testing.T) {
	t.Helper()
	t.Setenv("RANGERDANGER_ROOT", "/srv/rangerdanger")
	t.Setenv("RANGERDANGER_MODE", "source")
}

func TestLoadDefaults(t *testing.T) {
	isolateConfigEnv(t)
	setRange(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := &Config{
		HTTPPort:           8080,
		DBPath:             "backend/data/rangerdanger.db",
		AllowedOrigins:     []string{"*"},
		LabDefinitionsPath: "lab-definitions",
		Package:            DefaultPackage,
		Root:               "/srv/rangerdanger",
		Mode:               "source",
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("Load() = %#v, want defaults %#v", cfg, want)
	}
}

func TestLoadPromotesLegacyHTTPPort(t *testing.T) {
	isolateConfigEnv(t)
	setRange(t)
	t.Setenv("OTLAB_HTTP_PORT", "8181")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPPort != 8181 {
		t.Errorf("HTTPPort = %d, want legacy value 8181", cfg.HTTPPort)
	}
}

func TestLoadRequiresRootAndMode(t *testing.T) {
	tests := map[string]map[string]string{
		"no root":       {"RANGERDANGER_MODE": "source"},
		"relative root": {"RANGERDANGER_ROOT": "rangerdanger", "RANGERDANGER_MODE": "source"},
		"no mode":       {"RANGERDANGER_ROOT": "/srv/rangerdanger"},
		"unknown mode":  {"RANGERDANGER_ROOT": "/srv/rangerdanger", "RANGERDANGER_MODE": "offline"},
	}
	for name, env := range tests {
		t.Run(name, func(t *testing.T) {
			isolateConfigEnv(t)
			for key, value := range env {
				t.Setenv(key, value)
			}
			if cfg, err := Load(); err == nil {
				t.Errorf("Load() = %#v, want an error", cfg)
			}
		})
	}
}
