//go:build integration

package integration_test

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vivek/agent-task-tracker/internal/cli"
	"github.com/vivek/agent-task-tracker/internal/config"
	"github.com/vivek/agent-task-tracker/internal/testsupport"
	"github.com/vivek/agent-task-tracker/sql/migrations"
)

func TestCancellationMigrationAppliesAfterPreviousSchema(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	rootURL, err := testsupport.TestDatabaseURL()
	if err != nil {
		t.Fatal(err)
	}
	database, err := testsupport.CreateDatabase(ctx, rootURL)
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := database.Close(cleanupCtx); err != nil {
			t.Errorf("drop test database %q: %v", database.Name, err)
		}
	})

	previousDir := copyMigrationsBeforeCancellation(t)
	if _, err := cli.ApplyMigrations(ctx, config.Config{DatabaseURL: database.URL}, previousDir); err != nil {
		t.Fatalf("apply previous migration set: %v", err)
	}
	result, err := database.ApplyMigrations(ctx)
	if err != nil {
		t.Fatalf("apply cancellation migration: %v", err)
	}
	if got := strings.Join(result.Applied, ","); got != "0010_allow_cancelled_ticket_event" {
		t.Fatalf("expected only cancellation migration to apply, got %#v", result)
	}
}

func TestMigrationChecksumsAdoptLegacyHistoryAndRejectChanges(t *testing.T) {
	fixture := newFixture(t)
	var checksum string
	if err := fixture.runtime.Pool.QueryRow(fixture.context, "SELECT checksum FROM forge_schema_migrations WHERE id = '0001_initial_schema'").Scan(&checksum); err != nil || checksum == "" {
		t.Fatalf("expected recorded migration checksum, got %q err=%v", checksum, err)
	}
	if _, err := fixture.runtime.Pool.Exec(fixture.context, "UPDATE forge_schema_migrations SET checksum = NULL WHERE id = '0001_initial_schema'"); err != nil {
		t.Fatalf("clear legacy checksum: %v", err)
	}
	if _, err := cli.ApplyMigrations(fixture.context, config.Config{DatabaseURL: fixture.database.URL}, ""); err != nil {
		t.Fatalf("adopt legacy checksum with embedded migrations: %v", err)
	}
	if err := fixture.runtime.Pool.QueryRow(fixture.context, "SELECT checksum FROM forge_schema_migrations WHERE id = '0001_initial_schema'").Scan(&checksum); err != nil || checksum == "" {
		t.Fatalf("expected adopted checksum, got %q err=%v", checksum, err)
	}

	dir := copyAllMigrations(t)
	path := filepath.Join(dir, "0001_initial_schema.sql")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read copied migration: %v", err)
	}
	if err := os.WriteFile(path, append(data, []byte("\n-- changed\n")...), 0o600); err != nil {
		t.Fatalf("change copied migration: %v", err)
	}
	if _, err := cli.ApplyMigrations(fixture.context, config.Config{DatabaseURL: fixture.database.URL}, dir); err == nil || !strings.Contains(err.Error(), "checksum does not match") {
		t.Fatalf("expected migration checksum rejection, got %v", err)
	}
}

func TestScopeMigrationRejectsExistingMismatches(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	rootURL, err := testsupport.TestDatabaseURL()
	if err != nil {
		t.Fatal(err)
	}
	database, err := testsupport.CreateDatabase(ctx, rootURL)
	if err != nil {
		t.Fatalf("create test database: %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if err := database.Close(cleanupCtx); err != nil {
			t.Errorf("drop test database %q: %v", database.Name, err)
		}
	})
	if _, err := cli.ApplyMigrations(ctx, config.Config{DatabaseURL: database.URL}, copyMigrationsBeforeScopeIntegrity(t)); err != nil {
		t.Fatalf("apply migrations before scope integrity: %v", err)
	}
	pool, err := pgxpool.New(ctx, database.URL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `
INSERT INTO workspaces (name) VALUES ('scope-a'), ('scope-b');
INSERT INTO projects (workspace_id, name) VALUES
    ((SELECT id FROM workspaces WHERE name = 'scope-a'), 'project-a'),
    ((SELECT id FROM workspaces WHERE name = 'scope-b'), 'project-b');
INSERT INTO tickets (workspace_id, project_id, title, type, created_by)
VALUES (
    (SELECT id FROM workspaces WHERE name = 'scope-a'),
    (SELECT id FROM projects WHERE name = 'project-b'),
    'mismatched scope', 'task', 'human'
);`); err != nil {
		t.Fatalf("insert deliberate mismatch: %v", err)
	}
	if _, err := database.ApplyMigrations(ctx); err == nil || !strings.Contains(err.Error(), "scope integrity preflight failed") {
		t.Fatalf("expected scope preflight failure, got %v", err)
	}
}

func TestEmbeddedMigrationBinaryFromEmptyDirectory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	binary := filepath.Join(t.TempDir(), "forge")
	build := exec.CommandContext(ctx, "go", "build", "-o", binary, "./cmd/forge")
	build.Dir = filepath.Clean(filepath.Join(testsupport.MigrationsDir(), "..", ".."))
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build forge: %v: %s", err, output)
	}
	files, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil || len(files) == 0 {
		t.Fatalf("list embedded migrations: %v, %v", files, err)
	}
	var ids []string
	for _, name := range files {
		ids = append(ids, strings.TrimSuffix(name, ".sql"))
	}

	for _, baseline := range []bool{false, true} {
		name := "fresh"
		if baseline {
			name = "baseline"
		}
		t.Run(name, func(t *testing.T) {
			rootURL, err := testsupport.TestDatabaseURL()
			if err != nil {
				t.Fatal(err)
			}
			database, err := testsupport.CreateDatabase(ctx, rootURL)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cleanupCancel()
				if err := database.Close(cleanupCtx); err != nil {
					t.Errorf("drop test database: %v", err)
				}
			})
			pool, err := pgxpool.New(ctx, database.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			var baselined []string
			if baseline {
				// Reproduce a pre-ledger schema through the recognized baseline set.
				dir := t.TempDir()
				for _, file := range files {
					if file >= "0009" {
						break
					}
					data, err := migrations.Files.ReadFile(file)
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(dir, file), data, 0o600); err != nil {
						t.Fatal(err)
					}
					baselined = append(baselined, strings.TrimSuffix(file, ".sql"))
				}
				if _, err := cli.ApplyMigrations(ctx, config.Config{DatabaseURL: database.URL}, dir); err != nil {
					t.Fatalf("prepare existing schema: %v", err)
				}
				if _, err := pool.Exec(ctx, "DROP TABLE forge_schema_migrations"); err != nil {
					t.Fatal(err)
				}
			}

			// The subprocess sees only an empty working directory and a database URL.
			// It has neither a --dir argument nor a source checkout in its cwd.
			workDir := t.TempDir()
			t.Setenv("FORGE_DATABASE_URL", database.URL)
			t.Setenv("FORGE_CONFIG", "")
			run := func(extra ...string) cli.MigrationResult {
				t.Helper()
				command := exec.CommandContext(ctx, binary, append([]string{"migrate", "--json"}, extra...)...)
				command.Dir = workDir
				output, err := command.CombinedOutput()
				if err != nil {
					t.Fatalf("migrate from empty directory: %v: %s", err, output)
				}
				var result cli.MigrationResult
				if err := json.Unmarshal(output, &result); err != nil {
					t.Fatalf("decode migrations: %v: %s", err, output)
				}
				return result
			}
			var extra []string
			if baseline {
				extra = append(extra, "--baseline-existing")
			}
			first := run(extra...)
			if !slices.Equal(first.Applied, ids[len(baselined):]) || !slices.Equal(first.Baselined, baselined) || len(first.Skipped) != 0 {
				t.Fatalf("unexpected first migration result: %#v", first)
			}
			repeated := run()
			if len(repeated.Applied) != 0 || len(repeated.Baselined) != 0 || !slices.Equal(repeated.Skipped, ids) {
				t.Fatalf("expected idempotent embedded migrations: %#v", repeated)
			}
			// Disk and embedded sources must accept each other's recorded checksums.
			disk, err := database.ApplyMigrations(ctx)
			if err != nil || len(disk.Applied) != 0 || !slices.Equal(disk.Skipped, ids) {
				t.Fatalf("verify embedded history using disk migrations: %#v, %v", disk, err)
			}
			var count int
			if err := pool.QueryRow(ctx, "SELECT count(*) FROM forge_schema_migrations WHERE checksum IS NOT NULL").Scan(&count); err != nil || count != len(ids) {
				t.Fatalf("expected complete checksummed history, got %d, %v", count, err)
			}
			if _, err := pool.Exec(ctx, "SELECT id FROM tickets LIMIT 0"); err != nil {
				t.Fatalf("migration did not create ticket schema: %v", err)
			}
			if entries, err := os.ReadDir(workDir); err != nil || len(entries) != 0 {
				t.Fatalf("migrations should not require extracted files: %v, %v", entries, err)
			}
		})
	}
}

func TestMigrationFailureRollsBackSchemaAndHistory(t *testing.T) {
	fixture := newFixture(t)
	dir := copyAllMigrations(t)
	for name, sql := range map[string]string{
		"9998_transaction_probe.sql":   "-- +goose Up\nCREATE TABLE migration_transaction_probe (id integer);\n",
		"9999_transaction_failure.sql": "-- +goose Up\nSELECT 1 / 0;\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(sql), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := cli.ApplyMigrations(fixture.context, config.Config{DatabaseURL: fixture.database.URL}, dir); err == nil || !strings.Contains(err.Error(), "apply migration 9999_transaction_failure") {
		t.Fatalf("expected failing custom migration, got %v", err)
	}
	var rolledBack bool
	if err := fixture.runtime.Pool.QueryRow(fixture.context, `
SELECT to_regclass('migration_transaction_probe') IS NULL
   AND NOT EXISTS (SELECT 1 FROM forge_schema_migrations WHERE id LIKE '999%')`).Scan(&rolledBack); err != nil || !rolledBack {
		t.Fatalf("schema and migration history must roll back together: %v, %v", rolledBack, err)
	}
}

func copyMigrationsBeforeCancellation(t *testing.T) string {
	t.Helper()
	sourceDir := testsupport.MigrationsDir()
	destinationDir := t.TempDir()
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		t.Fatalf("read migration directory: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "0010_allow_cancelled_ticket_event.sql" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sourceDir, entry.Name()))
		if err != nil {
			t.Fatalf("read migration %s: %v", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(destinationDir, entry.Name()), data, 0o600); err != nil {
			t.Fatalf("copy migration %s: %v", entry.Name(), err)
		}
	}
	return destinationDir
}

func copyAllMigrations(t *testing.T) string {
	t.Helper()
	sourceDir := testsupport.MigrationsDir()
	destinationDir := t.TempDir()
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		t.Fatalf("read migration directory: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sourceDir, entry.Name()))
		if err != nil {
			t.Fatalf("read migration %s: %v", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(destinationDir, entry.Name()), data, 0o600); err != nil {
			t.Fatalf("copy migration %s: %v", entry.Name(), err)
		}
	}
	return destinationDir
}

func copyMigrationsBeforeScopeIntegrity(t *testing.T) string {
	t.Helper()
	sourceDir := testsupport.MigrationsDir()
	destinationDir := t.TempDir()
	entries, err := os.ReadDir(sourceDir)
	if err != nil {
		t.Fatalf("read migration directory: %v", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "0013_scope_integrity.sql" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sourceDir, entry.Name()))
		if err != nil {
			t.Fatalf("read migration %s: %v", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(destinationDir, entry.Name()), data, 0o600); err != nil {
			t.Fatalf("copy migration %s: %v", entry.Name(), err)
		}
	}
	return destinationDir
}
