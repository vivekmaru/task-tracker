package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type initResult struct {
	Path         string `json:"path"`
	ArtifactRoot string `json:"artifact_root"`
	Overwrote    bool   `json:"overwrote"`
}

type initConfigFile struct {
	DatabaseURL       string `json:"database_url"`
	HTTPAddr          string `json:"http_addr"`
	WorkerConcurrency int    `json:"worker_concurrency"`
	AdminToken        string `json:"admin_token"`
	AuthCookieSecure  bool   `json:"auth_cookie_secure"`
	ArtifactRoot      string `json:"artifact_root"`
	ArtifactBackend   string `json:"artifact_backend"`
}

func runInitCommand(args []string, stdout, stderr io.Writer) int {
	flags := newFlagSet("init", stderr)
	var path, databaseURL, httpAddr, adminToken, artifactRoot, artifactBackend string
	var workerConcurrency int
	var authCookieSecure, force, jsonOut bool
	flags.StringVar(&path, "path", "forge.local.json", "config file path")
	flags.StringVar(&databaseURL, "database-url", firstNonEmpty(os.Getenv("FORGE_DATABASE_URL"), "postgres://localhost:5432/forge?sslmode=disable"), "database URL")
	flags.StringVar(&httpAddr, "http-addr", firstNonEmpty(os.Getenv("FORGE_HTTP_ADDR"), "127.0.0.1:3017"), "HTTP listen address")
	flags.IntVar(&workerConcurrency, "worker-concurrency", defaultIntEnv("FORGE_WORKER_CONCURRENCY", 1), "worker concurrency")
	flags.StringVar(&adminToken, "admin-token", firstNonEmpty(os.Getenv("FORGE_ADMIN_TOKEN"), "change-me-local-admin-token"), "human admin token")
	flags.BoolVar(&authCookieSecure, "auth-cookie-secure", defaultBoolEnv("FORGE_AUTH_COOKIE_SECURE", false), "set secure auth cookie")
	flags.StringVar(&artifactRoot, "artifact-root", firstNonEmpty(os.Getenv("FORGE_ARTIFACT_ROOT"), ".forge/artifacts"), "local artifact root")
	flags.StringVar(&artifactBackend, "artifact-backend", firstNonEmpty(os.Getenv("FORGE_ARTIFACT_BACKEND"), "local"), "artifact backend")
	flags.BoolVar(&force, "force", false, "overwrite an existing config file")
	flags.BoolVar(&jsonOut, "json", false, "write JSON output")
	if !parseFlags(flags, args) {
		return 2
	}
	if err := validateInitOptions(path, databaseURL, httpAddr, workerConcurrency, adminToken, artifactRoot, artifactBackend); err != nil {
		fmt.Fprintf(stderr, "init argument error: %v\n", err)
		return 2
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		fmt.Fprintf(stderr, "init error: resolve config path: %v\n", err)
		return 1
	}
	overwrote := false
	if _, err := os.Stat(absolutePath); err == nil {
		if !force {
			fmt.Fprintf(stderr, "init argument error: %s already exists; pass --force to overwrite\n", absolutePath)
			return 2
		}
		overwrote = true
	} else if !os.IsNotExist(err) {
		fmt.Fprintf(stderr, "init error: inspect config file: %v\n", err)
		return 1
	}

	cfg := initConfigFile{
		DatabaseURL:       strings.TrimSpace(databaseURL),
		HTTPAddr:          strings.TrimSpace(httpAddr),
		WorkerConcurrency: workerConcurrency,
		AdminToken:        strings.TrimSpace(adminToken),
		AuthCookieSecure:  authCookieSecure,
		ArtifactRoot:      strings.TrimSpace(artifactRoot),
		ArtifactBackend:   strings.ToLower(strings.TrimSpace(artifactBackend)),
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "init error: encode config: %v\n", err)
		return 1
	}
	data = append(data, '\n')

	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
		fmt.Fprintf(stderr, "init error: create config directory: %v\n", err)
		return 1
	}
	if err := os.WriteFile(absolutePath, data, 0o600); err != nil {
		fmt.Fprintf(stderr, "init error: write config file: %v\n", err)
		return 1
	}

	resolvedArtifactRoot := resolveInitArtifactRoot(cfg.ArtifactRoot, absolutePath)
	if cfg.ArtifactBackend == "local" {
		if err := os.MkdirAll(resolvedArtifactRoot, 0o755); err != nil {
			fmt.Fprintf(stderr, "init error: create artifact root: %v\n", err)
			return 1
		}
	}

	result := initResult{Path: absolutePath, ArtifactRoot: resolvedArtifactRoot, Overwrote: overwrote}
	if jsonOut {
		return writeJSON(stdout, stderr, result)
	}
	if overwrote {
		fmt.Fprintf(stdout, "overwrote %s\n", absolutePath)
	} else {
		fmt.Fprintf(stdout, "wrote %s\n", absolutePath)
	}
	if cfg.ArtifactBackend == "local" {
		fmt.Fprintf(stdout, "created artifact root %s\n", resolvedArtifactRoot)
	}
	return 0
}

func validateInitOptions(path, databaseURL, httpAddr string, workerConcurrency int, adminToken, artifactRoot, artifactBackend string) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("--path is required")
	}
	if strings.TrimSpace(databaseURL) == "" {
		return fmt.Errorf("--database-url is required")
	}
	if strings.TrimSpace(httpAddr) == "" {
		return fmt.Errorf("--http-addr is required")
	}
	if workerConcurrency <= 0 {
		return fmt.Errorf("--worker-concurrency must be greater than zero")
	}
	if strings.TrimSpace(adminToken) == "" {
		return fmt.Errorf("--admin-token is required")
	}
	if strings.TrimSpace(artifactRoot) == "" {
		return fmt.Errorf("--artifact-root is required")
	}
	switch strings.ToLower(strings.TrimSpace(artifactBackend)) {
	case "local", "s3":
		return nil
	default:
		return fmt.Errorf("--artifact-backend must be local or s3")
	}
}

func resolveInitArtifactRoot(root string, configPath string) string {
	root = strings.TrimSpace(root)
	if filepath.IsAbs(root) {
		return filepath.Clean(root)
	}
	return filepath.Clean(filepath.Join(filepath.Dir(configPath), root))
}

func defaultIntEnv(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func defaultBoolEnv(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
