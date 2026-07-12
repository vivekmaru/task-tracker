package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	DeprecatedDevelopmentAdminToken = "change-me-local-admin-token"
	defaultHTTPAddr                 = "127.0.0.1:3017"
	defaultLogLevel                 = "info"
	defaultWorkerConcurrency        = 1
	defaultArtifactRoot             = ".forge/artifacts"
	defaultArtifactBackend          = "local"
)

// Config contains process configuration shared by Forge command modes.
type Config struct {
	DatabaseURL           string   `json:"database_url"`
	HTTPAddr              string   `json:"http_addr"`
	LogLevel              string   `json:"log_level"`
	WorkerConcurrency     int      `json:"worker_concurrency"`
	AdminToken            string   `json:"admin_token"`
	AuthCookieSecure      bool     `json:"auth_cookie_secure"`
	WebhookAllowedHosts   []string `json:"webhook_allowed_hosts"`
	WebhookAllowedCIDRs   []string `json:"webhook_allowed_cidrs"`
	WebhookRetentionHours int      `json:"webhook_retention_hours"`
	ArtifactRoot          string   `json:"artifact_root"`
	ArtifactBackend       string   `json:"artifact_backend"`
	S3Endpoint            string   `json:"s3_endpoint"`
	S3Region              string   `json:"s3_region"`
	S3Bucket              string   `json:"s3_bucket"`
	S3Prefix              string   `json:"s3_prefix"`
	S3AccessKeyID         string   `json:"s3_access_key_id"`
	S3SecretAccessKey     string   `json:"s3_secret_access_key"`
	S3SessionToken        string   `json:"s3_session_token"`
	S3UsePathStyle        bool     `json:"s3_use_path_style"`
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
		ArtifactBackend:   defaultArtifactBackend,
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
	if value := os.Getenv("FORGE_WEBHOOK_ALLOWED_HOSTS"); value != "" {
		cfg.WebhookAllowedHosts = splitCSV(value)
	}
	if value := os.Getenv("FORGE_WEBHOOK_ALLOWED_CIDRS"); value != "" {
		cfg.WebhookAllowedCIDRs = splitCSV(value)
	}
	if value := os.Getenv("FORGE_WEBHOOK_RETENTION_HOURS"); value != "" {
		hours, err := strconv.Atoi(value)
		if err != nil {
			return Config{}, fmt.Errorf("FORGE_WEBHOOK_RETENTION_HOURS must be an integer: %w", err)
		}
		cfg.WebhookRetentionHours = hours
	}
	if value := os.Getenv("FORGE_ARTIFACT_ROOT"); value != "" {
		cfg.ArtifactRoot = value
		artifactRootExplicit = true
		artifactRootFromEnv = true
	}
	if value := os.Getenv("FORGE_ARTIFACT_BACKEND"); value != "" {
		cfg.ArtifactBackend = value
	}
	if value := os.Getenv("FORGE_S3_ENDPOINT"); value != "" {
		cfg.S3Endpoint = value
	}
	if value := os.Getenv("FORGE_S3_REGION"); value != "" {
		cfg.S3Region = value
	}
	if value := os.Getenv("FORGE_S3_BUCKET"); value != "" {
		cfg.S3Bucket = value
	}
	if value := os.Getenv("FORGE_S3_PREFIX"); value != "" {
		cfg.S3Prefix = value
	}
	if value := os.Getenv("FORGE_S3_ACCESS_KEY_ID"); value != "" {
		cfg.S3AccessKeyID = value
	}
	if value := os.Getenv("FORGE_S3_SECRET_ACCESS_KEY"); value != "" {
		cfg.S3SecretAccessKey = value
	}
	if value := os.Getenv("FORGE_S3_SESSION_TOKEN"); value != "" {
		cfg.S3SessionToken = value
	}
	if value := os.Getenv("FORGE_S3_USE_PATH_STYLE"); value != "" {
		usePathStyle, err := strconv.ParseBool(value)
		if err != nil {
			return Config{}, fmt.Errorf("FORGE_S3_USE_PATH_STYLE must be a boolean: %w", err)
		}
		cfg.S3UsePathStyle = usePathStyle
	}
	artifactRoot, err := normalizeArtifactRoot(cfg.ArtifactRoot, configPath, artifactRootExplicit, artifactRootFromEnv)
	if err != nil {
		return Config{}, err
	}
	cfg.ArtifactRoot = artifactRoot
	cfg.ArtifactBackend = strings.ToLower(strings.TrimSpace(cfg.ArtifactBackend))
	if cfg.ArtifactBackend == "" {
		cfg.ArtifactBackend = defaultArtifactBackend
	}
	cfg.S3Endpoint = strings.TrimSpace(cfg.S3Endpoint)
	cfg.S3Region = strings.TrimSpace(cfg.S3Region)
	cfg.S3Bucket = strings.TrimSpace(cfg.S3Bucket)
	cfg.S3Prefix = strings.TrimSpace(cfg.S3Prefix)
	cfg.S3AccessKeyID = strings.TrimSpace(cfg.S3AccessKeyID)
	cfg.S3SecretAccessKey = strings.TrimSpace(cfg.S3SecretAccessKey)
	cfg.S3SessionToken = strings.TrimSpace(cfg.S3SessionToken)
	cfg.WebhookAllowedHosts = compactConfigStrings(cfg.WebhookAllowedHosts)
	cfg.WebhookAllowedCIDRs = compactConfigStrings(cfg.WebhookAllowedCIDRs)

	return cfg, nil
}

func splitCSV(value string) []string { return strings.Split(value, ",") }
func compactConfigStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
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
	if !isLoopbackAddress(c.HTTPAddr) {
		if strings.TrimSpace(c.AdminToken) == DeprecatedDevelopmentAdminToken {
			return errors.New("admin_token must not use the development placeholder outside loopback")
		}
		if !c.AuthCookieSecure {
			return errors.New("auth_cookie_secure must be true outside loopback")
		}
	}
	if err := c.ValidateArtifactStorage(); err != nil {
		return err
	}
	return nil
}

func isLoopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	host = strings.Trim(strings.TrimSpace(host), "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// ValidateWorker checks the minimum configuration needed to start worker mode.
func (c Config) ValidateWorker() error {
	if c.DatabaseURL == "" {
		return errors.New("database_url is required")
	}
	if c.WorkerConcurrency <= 0 {
		return errors.New("worker_concurrency must be greater than zero")
	}
	if err := c.ValidateArtifactStorage(); err != nil {
		return err
	}
	return nil
}

func (c Config) ValidateRuntime() error {
	if c.DatabaseURL == "" {
		return errors.New("database_url is required")
	}
	if c.WebhookRetentionHours < 0 {
		return errors.New("webhook_retention_hours must not be negative")
	}
	if err := c.ValidateArtifactStorage(); err != nil {
		return err
	}
	return nil
}

func (c Config) ValidateArtifactStorage() error {
	switch strings.ToLower(strings.TrimSpace(c.ArtifactBackend)) {
	case "", "local":
		return nil
	case "s3":
		if strings.TrimSpace(c.S3Bucket) == "" {
			return errors.New("s3_bucket is required when artifact_backend is s3")
		}
		if strings.TrimSpace(c.S3SessionToken) != "" && (strings.TrimSpace(c.S3AccessKeyID) == "" || strings.TrimSpace(c.S3SecretAccessKey) == "") {
			return errors.New("s3_access_key_id and s3_secret_access_key are required when s3_session_token is provided")
		}
		if (strings.TrimSpace(c.S3AccessKeyID) == "") != (strings.TrimSpace(c.S3SecretAccessKey) == "") {
			return errors.New("s3_access_key_id and s3_secret_access_key must be provided together")
		}
		return nil
	default:
		return errors.New("artifact_backend must be local or s3")
	}
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
