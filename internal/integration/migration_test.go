//go:build integration

package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivek/agent-task-tracker/internal/cli"
	"github.com/vivek/agent-task-tracker/internal/config"
	"github.com/vivek/agent-task-tracker/internal/testsupport"
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
