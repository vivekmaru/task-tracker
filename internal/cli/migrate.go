package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vivek/agent-task-tracker/internal/config"
)

const migrationTableSQL = `
CREATE TABLE IF NOT EXISTS forge_schema_migrations (
    id text PRIMARY KEY,
    filename text NOT NULL,
    applied_at timestamptz NOT NULL DEFAULT now()
)`

type MigrationResult struct {
	Applied []string `json:"applied"`
	Skipped []string `json:"skipped"`
}

func runMigrateCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	flags := newFlagSet("migrate", stderr)
	var opts commandOptions
	opts.bind(flags)
	var dir string
	flags.StringVar(&dir, "dir", "sql/migrations", "migration directory")
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
		deps.RunMigrate = ApplyMigrations
	}

	result, err := deps.RunMigrate(ctx, cfg, dir)
	if err != nil {
		fmt.Fprintf(stderr, "migrate error: %v\n", err)
		return 1
	}
	if opts.JSON {
		return writeJSON(stdout, stderr, result)
	}
	for _, id := range result.Applied {
		fmt.Fprintf(stdout, "applied %s\n", id)
	}
	for _, id := range result.Skipped {
		fmt.Fprintf(stdout, "skipped %s\n", id)
	}
	if len(result.Applied) == 0 && len(result.Skipped) == 0 {
		fmt.Fprintln(stdout, "no migrations found")
	}
	return 0
}

func ApplyMigrations(ctx context.Context, cfg config.Config, dir string) (MigrationResult, error) {
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
		Applied: []string{},
		Skipped: []string{},
	}
	for _, path := range files {
		id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if applied[id] {
			result.Skipped = append(result.Skipped, id)
			continue
		}
		sql, err := readMigrationUp(path)
		if err != nil {
			return MigrationResult{}, err
		}
		if _, err := tx.Exec(ctx, sql); err != nil {
			return MigrationResult{}, fmt.Errorf("apply migration %s: %w", id, err)
		}
		if _, err := tx.Exec(ctx, "INSERT INTO forge_schema_migrations (id, filename) VALUES ($1, $2)", id, filepath.Base(path)); err != nil {
			return MigrationResult{}, fmt.Errorf("record migration %s: %w", id, err)
		}
		result.Applied = append(result.Applied, id)
	}

	if err := tx.Commit(ctx); err != nil {
		return MigrationResult{}, fmt.Errorf("commit migrations: %w", err)
	}
	return result, nil
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
