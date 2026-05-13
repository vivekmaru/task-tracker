package runtime

import (
	"testing"

	"github.com/vivek/agent-task-tracker/internal/db"
)

func TestNewComposesQueriesServicesAndWorkers(t *testing.T) {
	queries := db.New(nil)

	rt := New(queries)

	if rt.Queries != queries {
		t.Fatalf("expected runtime to keep queries")
	}
	if rt.Tickets == nil {
		t.Fatal("expected ticket service")
	}
	if rt.Claims == nil {
		t.Fatal("expected claim service")
	}
	if rt.Attempts == nil {
		t.Fatal("expected attempt service")
	}
	if rt.Maintenance == nil {
		t.Fatal("expected maintenance worker")
	}
}
