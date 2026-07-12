// Package testsupport provides reusable support for Forge integration tests.
package testsupport

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/vivek/agent-task-tracker/internal/cli"
	"github.com/vivek/agent-task-tracker/internal/config"
)

const TestDatabaseURLEnv = "FORGE_TEST_DATABASE_URL"

var testDatabaseNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Database is an isolated PostgreSQL database created for one integration test.
type Database struct {
	URL      string
	Name     string
	adminURL string
}

// TestDatabaseURL returns the guarded connection URL used to provision test databases.
func TestDatabaseURL() (string, error) {
	raw := strings.TrimSpace(os.Getenv(TestDatabaseURLEnv))
	if raw == "" {
		return "", fmt.Errorf("%s is required for PostgreSQL integration tests", TestDatabaseURLEnv)
	}
	if _, _, err := parseTestDatabaseURL(raw); err != nil {
		return "", err
	}
	return raw, nil
}

// CreateDatabase creates a disposable database with a unique, test-only name.
func CreateDatabase(ctx context.Context, rootURL string) (*Database, error) {
	parsed, baseName, err := parseTestDatabaseURL(rootURL)
	if err != nil {
		return nil, err
	}

	name, err := uniqueDatabaseName(baseName)
	if err != nil {
		return nil, err
	}
	adminURL := databaseURL(parsed, "postgres")
	testURL := databaseURL(parsed, name)

	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		return nil, fmt.Errorf("connect to PostgreSQL maintenance database: %w", err)
	}
	defer admin.Close(ctx)

	if _, err := admin.Exec(ctx, "CREATE DATABASE "+pgx.Identifier{name}.Sanitize()); err != nil {
		return nil, fmt.Errorf("create isolated PostgreSQL database: %w", err)
	}

	return &Database{URL: testURL, Name: name, adminURL: adminURL}, nil
}

// ApplyMigrations applies Forge migrations using the production migration runner.
func (d *Database) ApplyMigrations(ctx context.Context) (cli.MigrationResult, error) {
	result, err := cli.ApplyMigrations(ctx, config.Config{DatabaseURL: d.URL}, migrationsDir())
	if err != nil {
		return cli.MigrationResult{}, fmt.Errorf("apply Forge migrations: %w", err)
	}
	return result, nil
}

// Close terminates remaining connections and drops the disposable database.
func (d *Database) Close(ctx context.Context) error {
	if d == nil {
		return nil
	}
	admin, err := pgx.Connect(ctx, d.adminURL)
	if err != nil {
		return fmt.Errorf("connect to PostgreSQL maintenance database for cleanup: %w", err)
	}
	defer admin.Close(ctx)

	if _, err := admin.Exec(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`, d.Name); err != nil {
		return fmt.Errorf("terminate test database connections: %w", err)
	}
	if _, err := admin.Exec(ctx, "DROP DATABASE IF EXISTS "+pgx.Identifier{d.Name}.Sanitize()); err != nil {
		return fmt.Errorf("drop isolated PostgreSQL database: %w", err)
	}
	return nil
}

func parseTestDatabaseURL(raw string) (*url.URL, string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, "", fmt.Errorf("parse %s: %w", TestDatabaseURLEnv, err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return nil, "", fmt.Errorf("%s must use a PostgreSQL URL", TestDatabaseURLEnv)
	}
	baseName := strings.Trim(parsed.Path, "/")
	if !testDatabaseNamePattern.MatchString(baseName) || !strings.HasPrefix(baseName, "forge_test") {
		return nil, "", fmt.Errorf("%s database name must start with forge_test", TestDatabaseURLEnv)
	}
	return parsed, baseName, nil
}

func databaseURL(parsed *url.URL, databaseName string) string {
	copy := *parsed
	copy.Path = "/" + databaseName
	return copy.String()
}

func uniqueDatabaseName(baseName string) (string, error) {
	var random [8]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", fmt.Errorf("generate test database suffix: %w", err)
	}
	suffix := hex.EncodeToString(random[:])
	maxBaseLength := 63 - len(suffix) - 1
	if len(baseName) > maxBaseLength {
		baseName = baseName[:maxBaseLength]
	}
	return baseName + "_" + suffix, nil
}

func migrationsDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("resolve testsupport source directory")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "sql", "migrations")
}
