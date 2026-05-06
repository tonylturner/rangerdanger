package config

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spf13/viper"
)

// Config stores runtime configuration for the backend service.
type Config struct {
	HTTPPort           int
	DBPath             string
	AllowedOrigins     []string
	LabDefinitionsPath string
	ContaindAPIURL     string
	ContaindConfigPath string
}

// envPrefix is the Viper env var prefix. Configuration is read from
// RANGERDANGER_* environment variables. The legacy OTLAB_* prefix is
// also accepted for backwards compatibility and emits a deprecation
// warning at startup; the alias will be removed in a future release.
const (
	envPrefix       = "rangerdanger"
	legacyEnvPrefix = "otlab"
)

// promoteLegacyEnv copies any legacy OTLAB_* environment variables to
// their RANGERDANGER_* equivalents at process start, so Viper sees the
// expected names while still honoring an old deployment's env file.
// New-style values always win — we only fill in when nothing's set.
func promoteLegacyEnv() {
	const legacy = "OTLAB_"
	const modern = "RANGERDANGER_"
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, legacy) {
			continue
		}
		eq := strings.IndexByte(kv, '=')
		if eq <= 0 {
			continue
		}
		oldKey := kv[:eq]
		val := kv[eq+1:]
		newKey := modern + strings.TrimPrefix(oldKey, legacy)
		if _, alreadySet := os.LookupEnv(newKey); alreadySet {
			continue
		}
		_ = os.Setenv(newKey, val)
		log.Printf("config: deprecated env var %s — use %s instead", oldKey, newKey)
	}
}

// Load reads configuration from environment variables and optional
// config files. Honors both RANGERDANGER_* (preferred) and OTLAB_*
// (deprecated) prefixes; warns on the latter at startup.
func Load() (*Config, error) {
	promoteLegacyEnv()

	v := viper.New()
	v.SetEnvPrefix(envPrefix)
	_ = legacyEnvPrefix // referenced by promoteLegacyEnv via constant
	v.AutomaticEnv()

	v.SetDefault("http_port", 8080)
	v.SetDefault("db_path", "backend/data/rangerdanger.db")
	v.SetDefault("allowed_origins", []string{"*"})
	v.SetDefault("lab_definitions_path", "lab-definitions")
	v.SetDefault("containd_api_url", "http://firewall:8080")
	v.SetDefault("containd_config_path", "lab-definitions/firewall/substation-weak.json")

	if err := v.ReadInConfig(); err != nil {
		// Config file is optional; ignore if not found.
	}

	cfg := &Config{
		HTTPPort:           v.GetInt("http_port"),
		DBPath:             v.GetString("db_path"),
		LabDefinitionsPath: v.GetString("lab_definitions_path"),
		ContaindAPIURL:     v.GetString("containd_api_url"),
		ContaindConfigPath: v.GetString("containd_config_path"),
	}

	if origins := v.GetStringSlice("allowed_origins"); len(origins) > 0 {
		cfg.AllowedOrigins = origins
	} else {
		cfg.AllowedOrigins = []string{"*"}
	}

	if cfg.DBPath == "" {
		return nil, fmt.Errorf("db_path must be set")
	}

	if cfg.LabDefinitionsPath == "" {
		cfg.LabDefinitionsPath = "lab-definitions"
	}

	return cfg, nil
}
