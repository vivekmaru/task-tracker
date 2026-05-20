package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	defaultHTTPAddr          = "127.0.0.1:3017"
	defaultLogLevel          = "info"
	defaultWorkerConcurrency = 1
	defaultArtifactRoot      = ".forge/artifacts"
)

// Config contains process configuration shared by Forge command modes.
type Config struct {
	DatabaseURL       string `json:"database_url"`
	HTTPAddr          string `json:"http_addr"`
	LogLevel          string `json:"log_level"`
	WorkerConcurrency int    `json:"worker_concurrency"`
	AdminToken        string `json:"admin_token"`
	AuthCookieSecure  bool   `json:"auth_cookie_secure"`
	ArtifactRoot      string `json:"artifact_root"`
}

// Options controls configuration loading.
type Options struct {
	ConfigPath string
}

// Load builds configuration from defaults, an optional JSON file, then env vars.
func Load(opts Options) (Config, error) {
	cfg := Config{
		HTTPAddr:          defaultHTTPAddr,
		LogLevel:          defaultLogLevel,
		WorkerConcurrency: defaultWorkerConcurrency,
		ArtifactRoot:      defaultArtifactRoot,
	}

	path := firstNonEmpty(opts.ConfigPath, os.Getenv("FORGE_CONFIG"))
	if path != "" {
		if err := loadFile(path, &cfg); err != nil {
			return Config{}, err
		}
	}

	if value := os.Getenv("FORGE_DATABASE_URL"); value != "" {
		cfg.DatabaseURL = value
	}
	if value := os.Getenv("FORGE_HTTP_ADDR"); value != "" {
		cfg.HTTPAddr = value
	}
	if value := os.Getenv("FORGE_LOG_LEVEL"); value != "" {
		cfg.LogLevel = value
	}
	if value := os.Getenv("FORGE_WORKER_CONCURRENCY"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("FORGE_WORKER_CONCURRENCY must be an integer: %w", err)
		}
		cfg.WorkerConcurrency = n
	}
	if value := os.Getenv("FORGE_ADMIN_TOKEN"); value != "" {
		cfg.AdminToken = value
	}
	if value := os.Getenv("FORGE_AUTH_COOKIE_SECURE"); value != "" {
		secure, err := strconv.ParseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("FORGE_AUTH_COOKIE_SECURE must be a boolean: %w", err)
		}
		cfg.AuthCookieSecure = secure
	}
	if value := os.Getenv("FORGE_ARTIFACT_ROOT"); value != "" {
		cfg.ArtifactRoot = value
	}

	return cfg, nil
}

// ValidateServer checks the minimum configuration needed to start server mode.
func (c Config) ValidateServer() error {
	if c.DatabaseURL == "" {
		return errors.New("database_url is required")
	}
	if c.HTTPAddr == "" {
		return errors.New("http_addr is required")
	}
	if strings.TrimSpace(c.AdminToken) == "" {
		return errors.New("admin_token is required")
	}
	return nil
}

// ValidateWorker checks the minimum configuration needed to start worker mode.
func (c Config) ValidateWorker() error {
	if c.DatabaseURL == "" {
		return errors.New("database_url is required")
	}
	if c.WorkerConcurrency <= 0 {
		return errors.New("worker_concurrency must be greater than zero")
	}
	return nil
}

func (c Config) ValidateRuntime() error {
	if c.DatabaseURL == "" {
		return errors.New("database_url is required")
	}
	return nil
}

func loadFile(path string, cfg *Config) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open config file: %w", err)
	}
	defer f.Close()

	if err := json.NewDecoder(f).Decode(cfg); err != nil {
		return fmt.Errorf("decode config file: %w", err)
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
