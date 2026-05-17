package tui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/services"
)

func TestTicketDetailModelRendersDenseInspectionSurface(t *testing.T) {
	ticket := detailTicketFixture()
	attempts := []db.Attempt{
		{
			ID:              testUUID(31),
			TicketID:        ticket.ID,
			Status:          services.AttemptStatusRunning,
			AgentID:         "codex",
			Harness:         "codex",
			Model:           "gpt-5",
			ProgressPercent: 40,
			CurrentSummary:  pgtype.Text{String: "debugging login", Valid: true},
			NextStep:        pgtype.Text{String: "run auth tests", Valid: true},
		},
		{
			ID:              testUUID(32),
			TicketID:        ticket.ID,
			Status:          services.AttemptStatusFailed,
			AgentID:         "opencode",
			Harness:         "opencode",
			Model:           "sonnet",
			FailureReason:   pgtype.Text{String: "timeout", Valid: true},
			FailureCategory: pgtype.Text{String: "infra", Valid: true},
		},
	}

	view := NewTicketDetailModel(ticket, attempts).View()

	for _, want := range []string{
		"Ticket Detail",
		"Fix auth refresh",
		"todo bug P1",
		"Tags: auth, security",
		"Source: agent codex",
		"Source attempt: 00000000-0000-0000-0000-000000000021",
		"Acceptance",
		"- Auth tests pass",
		"Verification",
		"$ go test ./internal/auth",
		"Paths",
		"internal/auth/session.go",
		"Expected artifacts",
		"test output",
		"Required tools",
		"go",
		"Permissions",
		"network",
		"Capabilities",
		"tests",
		"Harnesses",
		"codex",
		"Current attempt",
		"running codex/gpt-5 40%",
		"debugging login",
		"Next: run auth tests",
		"Prior attempts",
		"failed opencode/sonnet",
		"Failure: timeout",
		"Copy",
		"Ticket ID: 00000000-0000-0000-0000-000000000011",
		"forge get --ticket-id 00000000-0000-0000-0000-000000000011",
		"b back",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected detail view to contain %q, got:\n%s", want, view)
		}
	}
	for _, forbidden := range []string{"Sprint", "Board", "Drag"} {
		if strings.Contains(view, forbidden) {
			t.Fatalf("detail view should avoid board-management language %q, got:\n%s", forbidden, view)
		}
	}
}

func TestQueueModelEnterLoadsSelectedTicketDetail(t *testing.T) {
	first := db.Ticket{ID: testUUID(41), Title: "First", Status: services.TicketStatusTodo}
	second := db.Ticket{ID: testUUID(42), Title: "Second", Status: services.TicketStatusTodo}
	loader := &fakeDetailLoader{
		attempts: map[pgtype.UUID][]db.Attempt{
			second.ID: {
				{ID: testUUID(43), TicketID: second.ID, Status: services.AttemptStatusRunning, AgentID: "codex", Harness: "codex", Model: "gpt-5"},
			},
		},
	}
	model := NewQueueModel([]db.Ticket{first, second}).WithDetailLoader(context.Background(), loader).MoveDown()

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected enter to start detail load")
	}
	msg := cmd()
	updated, _ = updated.Update(msg)
	view := updated.View()

	if loader.requested != second.ID {
		t.Fatalf("expected detail load for selected ticket, got %#v", loader.requested)
	}
	for _, want := range []string{"Ticket Detail", "Second", "running codex/gpt-5"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected detail view to contain %q, got:\n%s", want, view)
		}
	}

	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if view := updated.View(); !strings.Contains(view, "Forge Queue") || !strings.Contains(view, "> P0 todo  Second") {
		t.Fatalf("expected back to return to selected queue row, got:\n%s", view)
	}
}

func TestQueueModelDetailLoadErrorRendersCalmState(t *testing.T) {
	ticket := db.Ticket{ID: testUUID(51), Title: "Broken", Status: services.TicketStatusTodo}
	model := NewQueueModel([]db.Ticket{ticket}).WithDetailLoader(context.Background(), &fakeDetailLoader{
		err: errors.New("attempt query failed"),
	})

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected enter to start detail load")
	}
	updated, _ = updated.Update(cmd())
	view := updated.View()

	for _, want := range []string{"Ticket Detail", "Broken", "Unable to load attempts", "attempt query failed", "b back"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected error detail view to contain %q, got:\n%s", want, view)
		}
	}
}

type fakeDetailLoader struct {
	requested pgtype.UUID
	attempts  map[pgtype.UUID][]db.Attempt
	err       error
}

func (f *fakeDetailLoader) ListAttemptsByTicket(_ context.Context, ticketID pgtype.UUID) ([]db.Attempt, error) {
	f.requested = ticketID
	return f.attempts[ticketID], f.err
}

func detailTicketFixture() db.Ticket {
	return db.Ticket{
		ID:                   testUUID(17),
		SourceAttemptID:      testUUID(33),
		Title:                "Fix auth refresh",
		Description:          "Refresh tokens expire too early.",
		Type:                 services.TicketTypeBug,
		Status:               services.TicketStatusTodo,
		Priority:             1,
		Tags:                 []string{"auth", "security"},
		AcceptanceCriteria:   []string{"Auth tests pass", "Refresh works after restart"},
		VerificationCommands: mustJSONStrings([]string{"go test ./internal/auth"}),
		ExpectedArtifacts:    []string{"test output"},
		RelevantPaths:        []string{"internal/auth/session.go"},
		RequiredTools:        []string{"go"},
		RequiredPermissions:  []string{"network"},
		RequiredCapabilities: []string{"tests"},
		AllowedHarnesses:     []string{"codex"},
		CreatedBy:            services.ActorAgent,
		CreatedByID:          pgtype.Text{String: "codex", Valid: true},
		CreationReason:       pgtype.Text{String: "found during auth run", Valid: true},
	}
}

func mustJSONStrings(values []string) []byte {
	data, err := json.Marshal(values)
	if err != nil {
		panic(err)
	}
	return data
}
