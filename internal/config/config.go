package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

	artifactRootExplicit := false
	artifactRootFromEnv := false
	configPath := firstNonEmpty(opts.ConfigPath, os.Getenv("FORGE_CONFIG"))
	if configPath != "" {
		metadata, err := loadFile(configPath, &cfg)
		if err != nil {
			return Config{}, err
		}
		artifactRootExplicit = metadata.ArtifactRootSet
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
		artifactRootExplicit = true
		artifactRootFromEnv = true
	}
	artifactRoot, err := normalizeArtifactRoot(cfg.ArtifactRoot, configPath, artifactRootExplicit, artifactRootFromEnv)
	if err != nil {
		return Config{}, err
	}
	cfg.ArtifactRoot = artifactRoot

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

type fileConfigMetadata struct {
	ArtifactRootSet bool
}

func loadFile(path string, cfg *Config) (fileConfigMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return fileConfigMetadata{}, fmt.Errorf("open config file: %w", err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return fileConfigMetadata{}, fmt.Errorf("decode config file: %w", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fileConfigMetadata{}, fmt.Errorf("decode config file metadata: %w", err)
	}
	_, artifactRootSet := raw["artifact_root"]
	return fileConfigMetadata{ArtifactRootSet: artifactRootSet}, nil
}

func normalizeArtifactRoot(root string, configPath string, explicit bool, fromEnv bool) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = defaultArtifactRoot
		explicit = false
	}
	if filepath.IsAbs(root) {
		return filepath.Clean(root), nil
	}
	base := ""
	switch {
	case explicit && !fromEnv && strings.TrimSpace(configPath) != "":
		absoluteConfigPath, err := filepath.Abs(configPath)
		if err != nil {
			return "", fmt.Errorf("resolve config path: %w", err)
		}
		base = filepath.Dir(absoluteConfigPath)
	case !explicit && root == defaultArtifactRoot:
		home, err := os.UserHomeDir()
		if err == nil && home != "" {
			base = home
			break
		}
		fallthrough
	default:
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("resolve working directory: %w", err)
		}
		base = wd
	}
	return filepath.Clean(filepath.Join(base, root)), nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
