package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUsesDefaults(t *testing.T) {
	t.Setenv("FORGE_DATABASE_URL", "")
	t.Setenv("FORGE_HTTP_ADDR", "")
	t.Setenv("FORGE_LOG_LEVEL", "")
	t.Setenv("FORGE_WORKER_CONCURRENCY", "")

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
}

func TestLoadMergesConfigFileAndEnvOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "forge.json")
	err := os.WriteFile(path, []byte(`{
		"database_url": "postgres://file",
		"http_addr": "127.0.0.1:4000",
		"log_level": "debug",
		"worker_concurrency": 2,
		"admin_token": "file-secret"
	}`), 0o600)
	if err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("FORGE_DATABASE_URL", "postgres://env")
	t.Setenv("FORGE_HTTP_ADDR", "")
	t.Setenv("FORGE_LOG_LEVEL", "")
	t.Setenv("FORGE_WORKER_CONCURRENCY", "4")
	t.Setenv("FORGE_ADMIN_TOKEN", "env-secret")

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
