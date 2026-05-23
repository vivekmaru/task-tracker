package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivek/agent-task-tracker/internal/config"
)

func TestRunMigrateUsesConfigAndDirectory(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "forge.json")
	if err := os.WriteFile(configPath, []byte(`{"database_url":"postgres://db"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var gotConfig config.Config
	var gotDir string

	var stdout, stderr strings.Builder
	code := RunWithDependencies([]string{
		"migrate",
		"--config", configPath,
		"--dir", "custom/migrations",
		"--baseline-existing",
		"--json",
	}, &stdout, &stderr, Dependencies{
		RunMigrate: func(_ context.Context, cfg config.Config, dir string, opts MigrationOptions) (MigrationResult, error) {
			gotConfig = cfg
			gotDir = dir
			if !opts.BaselineExisting {
				t.Fatal("expected baseline existing option")
			}
			return MigrationResult{Applied: []string{"0002_more"}, Skipped: []string{"0003_more"}, Baselined: []string{"0001_initial_schema"}}, nil
		},
	})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	if gotConfig.DatabaseURL != "postgres://db" {
		t.Fatalf("expected database URL from config, got %#v", gotConfig.DatabaseURL)
	}
	if gotDir != "custom/migrations" {
		t.Fatalf("expected migration directory, got %q", gotDir)
	}
	var result MigrationResult
	if err := json.Unmarshal([]byte(stdout.String()), &result); err != nil {
		t.Fatalf("decode migration result: %v; stdout=%s", err, stdout.String())
	}
	if len(result.Applied) != 1 || result.Applied[0] != "0002_more" {
		t.Fatalf("unexpected applied migrations: %#v", result)
	}
	if len(result.Skipped) != 1 || result.Skipped[0] != "0003_more" {
		t.Fatalf("unexpected skipped migrations: %#v", result)
	}
	if len(result.Baselined) != 1 || result.Baselined[0] != "0001_initial_schema" {
		t.Fatalf("unexpected baselined migrations: %#v", result)
	}
}

func TestRunMigratePrintsBaselinedMigrations(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "forge.json")
	if err := os.WriteFile(configPath, []byte(`{"database_url":"postgres://db"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var stdout, stderr strings.Builder
	code := RunWithDependencies([]string{
		"migrate",
		"--config", configPath,
	}, &stdout, &stderr, Dependencies{
		RunMigrate: func(context.Context, config.Config, string, MigrationOptions) (MigrationResult, error) {
			return MigrationResult{Applied: []string{"0002_more"}, Skipped: []string{"0003_more"}, Baselined: []string{"0001_initial_schema"}}, nil
		},
	})

	if code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr=%q", code, stderr.String())
	}
	for _, want := range []string{
		"baselined 0001_initial_schema",
		"applied 0002_more",
		"skipped 0003_more",
	} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("expected output to contain %q, got %q", want, stdout.String())
		}
	}
}

func TestRunMigrateRequiresDatabaseURL(t *testing.T) {
	var stdout, stderr strings.Builder

	code := RunWithDependencies([]string{"migrate"}, &stdout, &stderr, Dependencies{
		RunMigrate: func(context.Context, config.Config, string, MigrationOptions) (MigrationResult, error) {
			t.Fatal("migration runner should not be called")
			return MigrationResult{}, nil
		},
	})

	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "migrate configuration error: database_url is required") {
		t.Fatalf("expected database URL error, got %q", stderr.String())
	}
}

func TestExtractGooseUp(t *testing.T) {
	up, err := extractGooseUp(`-- +goose Up
CREATE TABLE things (id integer);

-- +goose Down
DROP TABLE things;
`)
	if err != nil {
		t.Fatalf("extract up migration: %v", err)
	}
	if up != "CREATE TABLE things (id integer);" {
		t.Fatalf("unexpected up migration: %q", up)
	}
}

func TestExtractGooseUpRejectsMissingMarker(t *testing.T) {
	_, err := extractGooseUp("CREATE TABLE things (id integer);")
	if err == nil || !strings.Contains(err.Error(), "missing -- +goose Up marker") {
		t.Fatalf("expected missing marker error, got %v", err)
	}
}
