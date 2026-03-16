package config

import (
    "fmt"

    "github.com/spf13/viper"
)

// Config stores runtime configuration for the backend service.
type Config struct {
    HTTPPort            int
    DBPath              string
    AllowedOrigins      []string
    LabDefinitionsPath  string
    ContaindAPIURL      string
    ContaindConfigPath  string
}

// Load reads configuration from environment variables and optional config files.
func Load() (*Config, error) {
    v := viper.New()
    v.SetEnvPrefix("otlab")
    v.AutomaticEnv()

    v.SetDefault("http_port", 8080)
    v.SetDefault("db_path", "backend/data/otlab.db")
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
