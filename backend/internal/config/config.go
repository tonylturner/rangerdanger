package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/viper"

	"github.com/tturner/rangerdanger/backend/internal/manifest"
)

// Config stores runtime configuration for the backend service.
type Config struct {
	HTTPPort           int
	DBPath             string
	AllowedOrigins     []string
	LabDefinitionsPath string
	// Package is the package served until a range is recorded
	// (RANGERDANGER_PACKAGE); the runtime record wins after that.
	Package string
	// Root is the installation root H (RANGERDANGER_ROOT): mounted at the
	// same path in the backend, and the Compose project directory of ranges.
	Root string
	// Mode selects each package's Compose file (RANGERDANGER_MODE).
	Mode manifest.Mode
}

// DefaultPackage is the package served when none is configured.
const DefaultPackage = "us-dnp3-substation"

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
	v.SetDefault("package", DefaultPackage)

	if err := v.ReadInConfig(); err != nil {
		// Config file is optional; ignore if not found.
	}

	cfg := &Config{
		HTTPPort:           v.GetInt("http_port"),
		DBPath:             v.GetString("db_path"),
		LabDefinitionsPath: v.GetString("lab_definitions_path"),
		Package:            v.GetString("package"),
		Root:               v.GetString("root"),
		Mode:               manifest.Mode(v.GetString("mode")),
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

	if cfg.Package == "" {
		cfg.Package = DefaultPackage
	}

	// Ranges cannot run without these, and both come from the platform
	// Compose file, so there is no default to fall back to.
	if !filepath.IsAbs(cfg.Root) {
		return nil, fmt.Errorf("root (RANGERDANGER_ROOT) must be an absolute path, got %q", cfg.Root)
	}
	if cfg.Mode != manifest.ModeSource && cfg.Mode != manifest.ModeRelease {
		return nil, fmt.Errorf("mode (RANGERDANGER_MODE) must be %q or %q, got %q", manifest.ModeSource, manifest.ModeRelease, cfg.Mode)
	}

	return cfg, nil
}
