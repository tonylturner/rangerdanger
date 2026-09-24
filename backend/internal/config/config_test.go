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
	"RANGERDANGER_CONTAIND_API_URL",
	"RANGERDANGER_CONTAIND_CONFIG_PATH",
	"OTLAB_HTTP_PORT",
	"OTLAB_DB_PATH",
	"OTLAB_ALLOWED_ORIGINS",
	"OTLAB_LAB_DEFINITIONS_PATH",
	"OTLAB_CONTAIND_API_URL",
	"OTLAB_CONTAIND_CONFIG_PATH",
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
		"RANGERDANGER_CONTAIND_API_URL":     "http://firewall.example:9000",
		"RANGERDANGER_CONTAIND_CONFIG_PATH": "fixtures/weak.json",
		"OTLAB_HTTP_PORT":                   "9999",
		"OTLAB_DB_PATH":                     "legacy.sqlite",
		"OTLAB_ALLOWED_ORIGINS":             "https://legacy.example",
		"OTLAB_LAB_DEFINITIONS_PATH":        "legacy-labs",
		"OTLAB_CONTAIND_API_URL":            "http://legacy-firewall:8080",
		"OTLAB_CONTAIND_CONFIG_PATH":        "legacy-weak.json",
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
		ContaindAPIURL:     "http://firewall.example:9000",
		ContaindConfigPath: "fixtures/weak.json",
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("Load() = %#v, want %#v", cfg, want)
	}
}

func TestLoadDefaults(t *testing.T) {
	isolateConfigEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	want := &Config{
		HTTPPort:           8080,
		DBPath:             "backend/data/rangerdanger.db",
		AllowedOrigins:     []string{"*"},
		LabDefinitionsPath: "lab-definitions",
		ContaindAPIURL:     "http://firewall:8080",
		ContaindConfigPath: "lab-definitions/firewall/substation-weak.json",
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Errorf("Load() = %#v, want defaults %#v", cfg, want)
	}
}

func TestLoadPromotesLegacyHTTPPort(t *testing.T) {
	isolateConfigEnv(t)
	t.Setenv("OTLAB_HTTP_PORT", "8181")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPPort != 8181 {
		t.Errorf("HTTPPort = %d, want legacy value 8181", cfg.HTTPPort)
	}
}
