package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

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

func TestRunMigrateDefaultsToEmbeddedMigrationsFromEmptyDirectory(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "forge.json")
	if err := os.WriteFile(configPath, []byte(`{"database_url":"postgres://db"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())

	called := false
	var stdout, stderr strings.Builder
	code := RunWithDependencies([]string{"migrate", "--config", configPath, "--json"}, &stdout, &stderr, Dependencies{
		RunMigrate: func(_ context.Context, _ config.Config, dir string, _ MigrationOptions) (MigrationResult, error) {
			called = true
			if dir != "" {
				t.Fatalf("default must select embedded migrations, got directory %q", dir)
			}
			source := migrationFS(dir)
			files, err := migrationFiles(source)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"0001_initial_schema.sql", "0003_add_attempt_checkpoints_ticket_index.sql", "0003_full_text_search.sql"} {
				if !slices.Contains(files, name) {
					t.Fatalf("missing embedded migration %s: %v", name, files)
				}
			}
			for _, name := range files {
				if _, err := readMigrationUp(source, name); err != nil {
					t.Fatal(err)
				}
				if _, err := migrationChecksum(source, name); err != nil {
					t.Fatal(err)
				}
			}
			return MigrationResult{}, nil
		},
	})
	if code != 0 || !called {
		t.Fatalf("default migrate failed: code=%d called=%v stderr=%s", code, called, stderr.String())
	}
}

func TestEmbeddedMigrationsMatchDisk(t *testing.T) {
	dir := filepath.Join("..", "..", "sql", "migrations")
	disk := migrationFS(dir)
	embedded := migrationFS("")
	files, err := migrationFiles(embedded)
	if err != nil {
		t.Fatal(err)
	}
	diskFiles, err := migrationFiles(disk)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 || !slices.Equal(files, diskFiles) {
		t.Fatalf("migration inventory differs: embedded=%v disk=%v", files, diskFiles)
	}
	for _, name := range files {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(data)
			checksum, err := migrationChecksum(embedded, name)
			if err != nil || checksum != hex.EncodeToString(sum[:]) {
				t.Fatalf("embedded checksum differs from original bytes: %q, %v", checksum, err)
			}
			up, err := readMigrationUp(embedded, name)
			if err != nil {
				t.Fatal(err)
			}
			diskUp, err := readMigrationUp(disk, name)
			if err != nil || up != diskUp {
				t.Fatalf("embedded Up SQL differs from disk: %v", err)
			}
		})
	}
}

func TestMigrationFilesPreserveOrderingAndDuplicatePrefixes(t *testing.T) {
	source := fstest.MapFS{
		"0003_full_text_search.sql":                     {Data: []byte("search")},
		"0003_add_attempt_checkpoints_ticket_index.sql": {Data: []byte("index")},
		"0001_initial_schema.sql":                       {Data: []byte("schema")},
		"embed.go":                                      {Data: []byte("package migrations")},
		"nested/0000_ignored.sql":                       {Data: []byte("nested")},
	}
	files, err := migrationFiles(source)
	want := []string{"0001_initial_schema.sql", "0003_add_attempt_checkpoints_ticket_index.sql", "0003_full_text_search.sql"}
	if err != nil || !slices.Equal(files, want) {
		t.Fatalf("expected filename ordering %v, got %v, %v", want, files, err)
	}
}

func TestMigrationDirectoryOverridesEmbedded(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "custom migrations")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	const name = "0042_custom.sql"
	const contents = "-- +goose Up\nSELECT 42;\n-- +goose Down\nSELECT 0;\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(root)
	for _, path := range []string{dir, "custom migrations"} {
		source := migrationFS(path)
		files, err := migrationFiles(source)
		if err != nil || !slices.Equal(files, []string{name}) {
			t.Fatalf("explicit directory %q must not include embedded files: %v, %v", path, files, err)
		}
		up, err := readMigrationUp(source, name)
		if err != nil || up != "SELECT 42;" {
			t.Fatalf("read custom Up SQL: %q, %v", up, err)
		}
		sum := sha256.Sum256([]byte(contents))
		checksum, err := migrationChecksum(source, name)
		if err != nil || checksum != hex.EncodeToString(sum[:]) {
			t.Fatalf("checksum must include full custom file: %q, %v", checksum, err)
		}
	}
	files, err := migrationFiles(migrationFS(t.TempDir()))
	if err != nil || len(files) != 0 {
		t.Fatalf("empty explicit directory must not fall back to embedded files: %v, %v", files, err)
	}
}

func TestReadMigrationUpFSErrors(t *testing.T) {
	source := fstest.MapFS{
		"missing-marker.sql": {Data: []byte("SELECT 1;")},
		"empty-up.sql":       {Data: []byte("-- +goose Up\n-- +goose Down\nSELECT 1;")},
	}
	for name, want := range map[string]string{
		"missing.sql":        "read migration missing.sql",
		"missing-marker.sql": "parse migration missing-marker.sql: missing -- +goose Up marker",
		"empty-up.sql":       "parse migration empty-up.sql: empty up migration",
	} {
		if _, err := readMigrationUp(source, name); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("expected %q, got %v", want, err)
		}
	}
	if _, err := migrationChecksum(source, "missing.sql"); err == nil || !strings.Contains(err.Error(), "read migration missing.sql") {
		t.Fatalf("expected checksum read error, got %v", err)
	}
}
