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
		"artifact_root": "/tmp/file-artifacts",
		"artifact_backend": "s3",
		"s3_endpoint": "http://file-s3.local",
		"s3_region": "us-west-2",
		"s3_bucket": "file-bucket",
		"s3_prefix": "file-prefix",
		"s3_access_key_id": "file-access",
		"s3_secret_access_key": "file-secret-key",
		"s3_session_token": "file-session",
		"s3_use_path_style": false
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
	t.Setenv("FORGE_ARTIFACT_BACKEND", "s3")
	t.Setenv("FORGE_S3_ENDPOINT", "http://env-s3.local")
	t.Setenv("FORGE_S3_REGION", "ap-southeast-2")
	t.Setenv("FORGE_S3_BUCKET", "env-bucket")
	t.Setenv("FORGE_S3_PREFIX", "env-prefix")
	t.Setenv("FORGE_S3_ACCESS_KEY_ID", "env-access")
	t.Setenv("FORGE_S3_SECRET_ACCESS_KEY", "env-secret-key")
	t.Setenv("FORGE_S3_SESSION_TOKEN", "env-session")
	t.Setenv("FORGE_S3_USE_PATH_STYLE", "true")

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
	if cfg.ArtifactBackend != "s3" || cfg.S3Endpoint != "http://env-s3.local" || cfg.S3Region != "ap-southeast-2" || cfg.S3Bucket != "env-bucket" || cfg.S3Prefix != "env-prefix" {
		t.Fatalf("expected env s3 overrides, got %#v", cfg)
	}
	if cfg.S3AccessKeyID != "env-access" || cfg.S3SecretAccessKey != "env-secret-key" || cfg.S3SessionToken != "env-session" || !cfg.S3UsePathStyle {
		t.Fatalf("expected env s3 credential/path-style overrides, got %#v", cfg)
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

func TestLoadResolvesExplicitDefaultArtifactRootFromConfigDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := t.TempDir()
	path := filepath.Join(dir, "forge.json")
	if err := os.WriteFile(path, []byte(`{"artifact_root":".forge/artifacts"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("FORGE_ARTIFACT_ROOT", "")

	cfg, err := Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatalf("expected config to load, got %v", err)
	}

	expectedArtifactRoot := filepath.Join(dir, ".forge", "artifacts")
	if cfg.ArtifactRoot != expectedArtifactRoot {
		t.Fatalf("expected explicit config artifact root %q, got %q", expectedArtifactRoot, cfg.ArtifactRoot)
	}
}

func TestLoadFallsBackToWorkingDirectoryWhenHomeIsUnset(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("FORGE_ARTIFACT_ROOT", "")
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("expected config to load without home directory, got %v", err)
	}

	expectedArtifactRoot := filepath.Join(wd, ".forge", "artifacts")
	if cfg.ArtifactRoot != expectedArtifactRoot {
		t.Fatalf("expected cwd artifact root fallback %q, got %q", expectedArtifactRoot, cfg.ArtifactRoot)
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

func TestLoadRejectsInvalidS3UsePathStyleEnv(t *testing.T) {
	t.Setenv("FORGE_S3_USE_PATH_STYLE", "sometimes")

	_, err := Load(Options{})

	if err == nil {
		t.Fatal("expected invalid boolean env error")
	}
	if got, want := err.Error(), "FORGE_S3_USE_PATH_STYLE must be a boolean"; !strings.Contains(got, want) {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestValidateArtifactStorageRequiresS3Bucket(t *testing.T) {
	cfg := Config{DatabaseURL: "postgres://db", ArtifactBackend: "s3"}

	err := cfg.ValidateRuntime()

	if err == nil {
		t.Fatal("expected validation error")
	}
	if got, want := err.Error(), "s3_bucket is required when artifact_backend is s3"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestValidateArtifactStorageRejectsPartialS3Credentials(t *testing.T) {
	cfg := Config{DatabaseURL: "postgres://db", ArtifactBackend: "s3", S3Bucket: "forge", S3AccessKeyID: "access"}

	err := cfg.ValidateRuntime()

	if err == nil {
		t.Fatal("expected validation error")
	}
	if got, want := err.Error(), "s3_access_key_id and s3_secret_access_key must be provided together"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestValidateArtifactStorageRejectsSessionTokenWithoutStaticCredentials(t *testing.T) {
	cfg := Config{DatabaseURL: "postgres://db", ArtifactBackend: "s3", S3Bucket: "forge", S3SessionToken: "session"}

	err := cfg.ValidateRuntime()

	if err == nil {
		t.Fatal("expected validation error")
	}
	if got, want := err.Error(), "s3_access_key_id and s3_secret_access_key are required when s3_session_token is provided"; got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestValidateArtifactStorageRejectsUnknownBackend(t *testing.T) {
	cfg := Config{DatabaseURL: "postgres://db", ArtifactBackend: "ftp"}

	err := cfg.ValidateRuntime()

	if err == nil {
		t.Fatal("expected validation error")
	}
	if got, want := err.Error(), "artifact_backend must be local or s3"; got != want {
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
