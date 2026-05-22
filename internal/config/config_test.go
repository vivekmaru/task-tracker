package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadUsesDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("FORGE_DATABASE_URL", "")
	t.Setenv("FORGE_HTTP_ADDR", "")
	t.Setenv("FORGE_LOG_LEVEL", "")
	t.Setenv("FORGE_WORKER_CONCURRENCY", "")
	t.Setenv("FORGE_AUTH_COOKIE_SECURE", "")
	t.Setenv("FORGE_ARTIFACT_ROOT", "")

	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("expected defaults to load, got %v", err)
	}

	if cfg.HTTPAddr != "127.0.0.1:3017" {
		t.Fatalf("unexpected HTTPAddr: %q", cfg.HTTPAddr)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("unexpected LogLevel: %q", cfg.LogLevel)
	}
	if cfg.WorkerConcurrency != 1 {
		t.Fatalf("unexpected WorkerConcurrency: %d", cfg.WorkerConcurrency)
	}
	if cfg.AuthCookieSecure {
		t.Fatal("expected auth cookies to default to non-secure for local HTTP")
	}
	expectedArtifactRoot := filepath.Join(home, ".forge", "artifacts")
	if cfg.ArtifactRoot != expectedArtifactRoot {
		t.Fatalf("unexpected ArtifactRoot: %q", cfg.ArtifactRoot)
	}
}

func TestLoadMergesConfigFileAndEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "forge.json")
	err := os.WriteFile(path, []byte(`{
		"database_url": "postgres://file",
		"http_addr": "127.0.0.1:4000",
		"log_level": "debug",
		"worker_concurrency": 2,
		"admin_token": "file-secret",
		"auth_cookie_secure": false,
		"artifact_root": "/tmp/file-artifacts"
	}`), 0o600)
	if err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("FORGE_DATABASE_URL", "postgres://env")
	t.Setenv("FORGE_HTTP_ADDR", "")
	t.Setenv("FORGE_LOG_LEVEL", "")
	t.Setenv("FORGE_WORKER_CONCURRENCY", "4")
	t.Setenv("FORGE_ADMIN_TOKEN", "env-secret")
	t.Setenv("FORGE_AUTH_COOKIE_SECURE", "true")
	t.Setenv("FORGE_ARTIFACT_ROOT", "/tmp/env-artifacts")

	cfg, err := Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatalf("expected config to load, got %v", err)
	}

	if cfg.DatabaseURL != "postgres://env" {
		t.Fatalf("expected env database override, got %q", cfg.DatabaseURL)
	}
	if cfg.HTTPAddr != "127.0.0.1:4000" {
		t.Fatalf("expected file http addr, got %q", cfg.HTTPAddr)
	}
	if cfg.WorkerConcurrency != 4 {
		t.Fatalf("expected env worker override, got %d", cfg.WorkerConcurrency)
	}
	if cfg.AdminToken != "env-secret" {
		t.Fatalf("expected env admin token override, got %q", cfg.AdminToken)
	}
	if !cfg.AuthCookieSecure {
		t.Fatal("expected env auth cookie secure override")
	}
	if cfg.ArtifactRoot != "/tmp/env-artifacts" {
		t.Fatalf("expected env artifact root override, got %q", cfg.ArtifactRoot)
	}
}

func TestLoadResolvesRelativeConfigArtifactRootFromConfigDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "forge.json")
	if err := os.WriteFile(path, []byte(`{"artifact_root":"artifacts"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("FORGE_ARTIFACT_ROOT", "")

	cfg, err := Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatalf("expected config to load, got %v", err)
	}

	expectedArtifactRoot := filepath.Join(dir, "artifacts")
	if cfg.ArtifactRoot != expectedArtifactRoot {
		t.Fatalf("expected config-relative artifact root %q, got %q", expectedArtifactRoot, cfg.ArtifactRoot)
	}
}

func TestLoadRejectsInvalidAuthCookieSecureEnv(t *testing.T) {
	t.Setenv("FORGE_AUTH_COOKIE_SECURE", "sometimes")

	_, err := Load(Options{})

	if err == nil {
		t.Fatal("expected invalid boolean env error")
	}
	if got, want := err.Error(), "FORGE_AUTH_COOKIE_SECURE must be a boolean"; !strings.Contains(got, want) {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestValidateServerRequiresDatabaseURL(t *testing.T) {
	cfg := Config{HTTPAddr: "127.0.0.1:3017", WorkerConcurrency: 1, AdminToken: "secret"}

	err := cfg.ValidateServer()

	if err == nil {
		t.Fatal("expected validation error")
	}
	if got, want := err.Error(), "database_url is required"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestValidateServerRequiresAdminToken(t *testing.T) {
	cfg := Config{DatabaseURL: "postgres://db", HTTPAddr: "127.0.0.1:3017", WorkerConcurrency: 1}

	err := cfg.ValidateServer()

	if err == nil {
		t.Fatal("expected validation error")
	}
	if got, want := err.Error(), "admin_token is required"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestValidateServerRejectsWhitespaceAdminToken(t *testing.T) {
	cfg := Config{DatabaseURL: "postgres://db", HTTPAddr: "127.0.0.1:3017", WorkerConcurrency: 1, AdminToken: "   "}

	err := cfg.ValidateServer()

	if err == nil {
		t.Fatal("expected validation error")
	}
	if got, want := err.Error(), "admin_token is required"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestValidateWorkerRequiresPositiveConcurrency(t *testing.T) {
	cfg := Config{DatabaseURL: "postgres://db", WorkerConcurrency: 0}

	err := cfg.ValidateWorker()

	if err == nil {
		t.Fatal("expected validation error")
	}
	if got, want := err.Error(), "worker_concurrency must be greater than zero"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}
