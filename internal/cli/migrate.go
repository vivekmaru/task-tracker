package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vivek/agent-task-tracker/internal/config"
)

const migrationTableSQL = `
CREATE TABLE IF NOT EXISTS forge_schema_migrations (
    id text PRIMARY KEY,
    filename text NOT NULL,
    checksum text,
    applied_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE forge_schema_migrations ADD COLUMN IF NOT EXISTS checksum text`

type MigrationResult struct {
	Applied   []string `json:"applied"`
	Skipped   []string `json:"skipped"`
	Baselined []string `json:"baselined"`
}

type MigrationOptions struct {
	BaselineExisting bool
}

func runMigrateCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	flags := newFlagSet("migrate", stderr)
	var opts commandOptions
	opts.bind(flags)
	var dir string
	var baselineExisting bool
	flags.StringVar(&dir, "dir", "sql/migrations", "migration directory")
	flags.BoolVar(&baselineExisting, "baseline-existing", false, "record existing Forge schema migrations before applying new ones")
	if !parseFlags(flags, args) {
		return 2
	}

	cfg, err := config.Load(config.Options{ConfigPath: opts.ConfigPath})
	if err != nil {
		fmt.Fprintf(stderr, "migrate configuration error: %v\n", err)
		return 2
	}
	if strings.TrimSpace(cfg.DatabaseURL) == "" {
		fmt.Fprintln(stderr, "migrate configuration error: database_url is required")
		return 2
	}
	if deps.RunMigrate == nil {
		deps.RunMigrate = ApplyMigrationsWithOptions
	}

	result, err := deps.RunMigrate(ctx, cfg, dir, MigrationOptions{BaselineExisting: baselineExisting})
	if err != nil {
		fmt.Fprintf(stderr, "migrate error: %v\n", err)
		return 1
	}
	if opts.JSON {
		return writeJSON(stdout, stderr, result)
	}
	for _, id := range result.Baselined {
		fmt.Fprintf(stdout, "baselined %s\n", id)
	}
	for _, id := range result.Applied {
		fmt.Fprintf(stdout, "applied %s\n", id)
	}
	for _, id := range result.Skipped {
		fmt.Fprintf(stdout, "skipped %s\n", id)
	}
	if len(result.Applied) == 0 && len(result.Skipped) == 0 && len(result.Baselined) == 0 {
		fmt.Fprintln(stdout, "no migrations found")
	}
	return 0
}

func ApplyMigrations(ctx context.Context, cfg config.Config, dir string) (MigrationResult, error) {
	return ApplyMigrationsWithOptions(ctx, cfg, dir, MigrationOptions{})
}

func ApplyMigrationsWithOptions(ctx context.Context, cfg config.Config, dir string, opts MigrationOptions) (MigrationResult, error) {
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return MigrationResult{}, fmt.Errorf("create postgres pool: %w", err)
	}
	defer pool.Close()

	files, err := migrationFiles(dir)
	if err != nil {
		return MigrationResult{}, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return MigrationResult{}, fmt.Errorf("begin migration transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(ctx, migrationTableSQL); err != nil {
		return MigrationResult{}, fmt.Errorf("ensure migration table: %w", err)
	}
	if _, err := tx.Exec(ctx, "LOCK TABLE forge_schema_migrations IN EXCLUSIVE MODE"); err != nil {
		return MigrationResult{}, fmt.Errorf("lock migration table: %w", err)
	}

	applied, err := loadAppliedMigrations(ctx, tx)
	if err != nil {
		return MigrationResult{}, err
	}

	result := MigrationResult{
		Applied:   []string{},
		Skipped:   []string{},
		Baselined: []string{},
	}
	baselinedThisRun := map[string]bool{}
	if opts.BaselineExisting && len(applied) == 0 {
		baseline, err := detectBaselineMigrations(ctx, tx, files)
		if err != nil {
			return MigrationResult{}, err
		}
		if len(baseline) == 0 {
			return MigrationResult{}, errors.New("baseline-existing requires an existing Forge schema with recognized migration artifacts")
		}
		for _, path := range files {
			id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			if !baseline[id] {
				continue
			}
			if err := recordMigration(ctx, tx, id, path); err != nil {
				return MigrationResult{}, fmt.Errorf("record baseline migration %s: %w", id, err)
			}
			applied[id] = true
			baselinedThisRun[id] = true
			result.Baselined = append(result.Baselined, id)
		}
	}
	if err := verifyMigrationChecksums(ctx, tx, files); err != nil {
		return MigrationResult{}, err
	}
	for _, path := range files {
		id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if applied[id] {
			if !baselinedThisRun[id] {
				result.Skipped = append(result.Skipped, id)
			}
			continue
		}
		sql, err := readMigrationUp(path)
		if err != nil {
			return MigrationResult{}, err
		}
		if _, err := tx.Exec(ctx, sql); err != nil {
			return MigrationResult{}, fmt.Errorf("apply migration %s: %w", id, err)
		}
		if err := recordMigration(ctx, tx, id, path); err != nil {
			return MigrationResult{}, fmt.Errorf("record migration %s: %w", id, err)
		}
		result.Applied = append(result.Applied, id)
	}

	if err := tx.Commit(ctx); err != nil {
		return MigrationResult{}, fmt.Errorf("commit migrations: %w", err)
	}
	return result, nil
}

type migrationRecorder interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func recordMigration(ctx context.Context, q migrationRecorder, id string, path string) error {
	checksum, err := migrationChecksum(path)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, "INSERT INTO forge_schema_migrations (id, filename, checksum) VALUES ($1, $2, $3)", id, filepath.Base(path), checksum)
	return err
}

type migrationChecksumQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func verifyMigrationChecksums(ctx context.Context, q migrationChecksumQuerier, files []string) error {
	byID := make(map[string]string, len(files))
	for _, path := range files {
		byID[strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))] = path
	}
	rows, err := q.Query(ctx, "SELECT id, filename, checksum FROM forge_schema_migrations")
	if err != nil {
		return fmt.Errorf("list migration checksums: %w", err)
	}
	defer rows.Close()
	type adoption struct{ id, checksum string }
	adoptions := []adoption{}
	for rows.Next() {
		var id, filename string
		var checksum pgtype.Text
		if err := rows.Scan(&id, &filename, &checksum); err != nil {
			return fmt.Errorf("scan migration checksum: %w", err)
		}
		path, ok := byID[id]
		if !ok {
			return fmt.Errorf("applied migration %s is missing", id)
		}
		if filename != filepath.Base(path) {
			return fmt.Errorf("applied migration %s filename changed from %s to %s", id, filename, filepath.Base(path))
		}
		current, err := migrationChecksum(path)
		if err != nil {
			return err
		}
		if !checksum.Valid || checksum.String == "" {
			adoptions = append(adoptions, adoption{id: id, checksum: current})
		} else if checksum.String != current {
			return fmt.Errorf("applied migration %s checksum does not match file", id)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read migration checksums: %w", err)
	}
	rows.Close()
	for _, adoption := range adoptions {
		if _, err := q.Exec(ctx, "UPDATE forge_schema_migrations SET checksum = $2 WHERE id = $1 AND checksum IS NULL", adoption.id, adoption.checksum); err != nil {
			return fmt.Errorf("adopt checksum for migration %s: %w", adoption.id, err)
		}
	}
	return nil
}

func migrationChecksum(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read migration %s: %w", filepath.Base(path), err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

type baselineMigrationQuerier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func detectBaselineMigrations(ctx context.Context, q baselineMigrationQuerier, files []string) (map[string]bool, error) {
	coreSchema, err := hasAllRelations(ctx, q, []string{
		"workspaces",
		"projects",
		"tickets",
		"ticket_dependencies",
		"attempts",
		"attempt_checkpoints",
		"ticket_events",
		"artifacts",
		"idempotency_keys",
		"agent_capabilities",
		"api_keys",
		"attempt_metrics",
	})
	if err != nil {
		return nil, err
	}
	if !coreSchema {
		return map[string]bool{}, nil
	}

	candidates := map[string]bool{
		"0001_initial_schema": true,
	}
	if ok, err := constraintContains(ctx, q, "ticket_events", "ticket_events_type_check", "ready"); err != nil {
		return nil, err
	} else if ok {
		candidates["0002_ticket_transition_event_types"] = true
	}
	if ok, err := relationExists(ctx, q, "idx_attempt_checkpoints_ticket_id"); err != nil {
		return nil, err
	} else if ok {
		candidates["0003_add_attempt_checkpoints_ticket_index"] = true
	}
	if ok, err := hasAllRelations(ctx, q, []string{
		"idx_tickets_search_vector",
		"idx_attempts_search_vector",
		"idx_ticket_events_search_vector",
		"idx_artifacts_search_vector",
	}); err != nil {
		return nil, err
	} else if ok {
		candidates["0003_full_text_search"] = true
	}
	if ok, err := hasAllRelations(ctx, q, []string{"webhook_subscriptions", "webhook_deliveries"}); err != nil {
		return nil, err
	} else if ok {
		candidates["0004_webhook_deliveries"] = true
	}
	if ok, err := relationExists(ctx, q, "workspace_members"); err != nil {
		return nil, err
	} else if ok {
		candidates["0005_workspace_members"] = true
	}
	if ok, err := columnExists(ctx, q, "ticket_events", "event_sequence"); err != nil {
		return nil, err
	} else if ok {
		candidates["0006_ticket_event_sequence"] = true
	}
	if ok, err := functionSourceContains(ctx, q, "enqueue_webhook_deliveries_for_ticket_event", "'metrics'"); err != nil {
		return nil, err
	} else if ok {
		candidates["0007_webhook_observability_snapshots"] = true
	}
	if ok, err := constraintContains(ctx, q, "tickets", "tickets_type_check", "task"); err != nil {
		return nil, err
	} else if ok {
		candidates["0008_ticket_task_type"] = true
	}

	filtered := make(map[string]bool, len(candidates))
	for _, path := range files {
		id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if candidates[id] {
			filtered[id] = true
		}
	}
	return filtered, nil
}

func hasAllRelations(ctx context.Context, q baselineMigrationQuerier, names []string) (bool, error) {
	for _, name := range names {
		exists, err := relationExists(ctx, q, name)
		if err != nil || !exists {
			return exists, err
		}
	}
	return true, nil
}

func relationExists(ctx context.Context, q baselineMigrationQuerier, name string) (bool, error) {
	var exists bool
	if err := q.QueryRow(ctx, "SELECT to_regclass($1) IS NOT NULL", name).Scan(&exists); err != nil {
		return false, fmt.Errorf("inspect relation %s: %w", name, err)
	}
	return exists, nil
}

func columnExists(ctx context.Context, q baselineMigrationQuerier, table string, column string) (bool, error) {
	var exists bool
	err := q.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM information_schema.columns
    WHERE table_schema = current_schema()
      AND table_name = $1
      AND column_name = $2
)`, table, column).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("inspect column %s.%s: %w", table, column, err)
	}
	return exists, nil
}

func constraintContains(ctx context.Context, q baselineMigrationQuerier, table string, constraint string, fragment string) (bool, error) {
	var exists bool
	err := q.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM pg_constraint c
    JOIN pg_class r ON r.oid = c.conrelid
    JOIN pg_namespace n ON n.oid = r.relnamespace
    WHERE n.nspname = current_schema()
      AND r.relname = $1
      AND c.conname = $2
      AND pg_get_constraintdef(c.oid) ILIKE '%' || $3 || '%'
)`, table, constraint, fragment).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("inspect constraint %s.%s: %w", table, constraint, err)
	}
	return exists, nil
}

func functionSourceContains(ctx context.Context, q baselineMigrationQuerier, name string, fragment string) (bool, error) {
	var exists bool
	err := q.QueryRow(ctx, `
SELECT EXISTS (
    SELECT 1
    FROM pg_proc p
    JOIN pg_namespace n ON n.oid = p.pronamespace
    WHERE n.nspname = current_schema()
      AND p.proname = $1
      AND p.prosrc ILIKE '%' || $2 || '%'
)`, name, fragment).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("inspect function %s: %w", name, err)
	}
	return exists, nil
}

type appliedMigrationQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func loadAppliedMigrations(ctx context.Context, q appliedMigrationQuerier) (map[string]bool, error) {
	rows, err := q.Query(ctx, "SELECT id FROM forge_schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("list applied migrations: %w", err)
	}
	defer rows.Close()

	applied := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan applied migration: %w", err)
		}
		applied[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}
	return applied, nil
}

func migrationFiles(dir string) ([]string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, errors.New("migration directory is required")
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

func readMigrationUp(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read migration %s: %w", filepath.Base(path), err)
	}
	up, err := extractGooseUp(string(data))
	if err != nil {
		return "", fmt.Errorf("parse migration %s: %w", filepath.Base(path), err)
	}
	return up, nil
}

func extractGooseUp(contents string) (string, error) {
	lines := strings.Split(contents, "\n")
	start := -1
	end := len(lines)
	for index, line := range lines {
		switch strings.TrimSpace(line) {
		case "-- +goose Up":
			start = index + 1
		case "-- +goose Down":
			if start >= 0 {
				end = index
				goto done
			}
		}
	}

done:
	if start < 0 {
		return "", errors.New("missing -- +goose Up marker")
	}
	up := strings.TrimSpace(strings.Join(lines[start:end], "\n"))
	if up == "" {
		return "", errors.New("empty up migration")
	}
	return up, nil
}
