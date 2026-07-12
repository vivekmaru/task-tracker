//go:build integration

package integration_test

import (
	"context"
	"testing"
	"time"

	"github.com/vivek/agent-task-tracker/internal/config"
	"github.com/vivek/agent-task-tracker/internal/runtime"
	"github.com/vivek/agent-task-tracker/internal/testsupport"
)

type fixture struct {
	context  context.Context
	database *testsupport.Database
	runtime  *runtime.Runtime
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
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

	result, err := database.ApplyMigrations(ctx)
	if err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	if len(result.Applied) == 0 {
		t.Fatal("expected fresh test database migrations to be applied")
	}

	rt, err := runtime.Open(ctx, config.Config{
		DatabaseURL:  database.URL,
		ArtifactRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("open runtime: %v", err)
	}
	t.Cleanup(rt.Close)

	return &fixture{context: ctx, database: database, runtime: rt}
}
