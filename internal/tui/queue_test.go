package tui

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/services"
)

func TestQueueModelRendersSummaryListAndPreview(t *testing.T) {
	model := NewQueueModel([]db.Ticket{
		{ID: testUUID(9), Title: "Fix auth", Type: services.TicketTypeBug, Status: services.TicketStatusTodo, Priority: 1},
		{Title: "Write docs", Type: services.TicketTypeDocumentation, Status: services.TicketStatusBlocked, Priority: 2},
	})

	view := model.View()

	for _, want := range []string{
		"Forge Queue",
		"todo 1",
		"blocked 1",
		"> P1 todo bug Fix auth",
		"  P2 blocked documentation Write docs",
		"Selected",
		"Fix auth",
		"Link: /tickets/00000000-0000-0000-0000-000000000009",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected queue view to contain %q, got:\n%s", want, view)
		}
	}
}

func TestQueueModelRendersProposedWorkSignals(t *testing.T) {
	ticketID := testUUID(10)
	model := NewQueueModel([]db.Ticket{
		{
			ID:             ticketID,
			Title:          "Follow-up from smoke",
			Type:           services.TicketTypeFollowUp,
			Status:         services.TicketStatusBacklog,
			Priority:       2,
			CreatedBy:      services.ActorAgent,
			CreatedByID:    pgtype.Text{String: "codex", Valid: true},
			CreationReason: pgtype.Text{String: "missing retry coverage", Valid: true},
		},
	})

	view := model.View()

	for _, want := range []string{
		"proposed 1",
		"> P2 backlog follow_up [proposed] Follow-up from smoke",
		"Proposed by: codex",
		"Reason: missing retry coverage",
		"Triage: /proposed/" + uuidText(ticketID),
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected proposed queue view to contain %q, got:\n%s", want, view)
		}
	}
}

func TestQueueModelMovesSelectionWithinBounds(t *testing.T) {
	model := NewQueueModel([]db.Ticket{
		{Title: "First", Status: services.TicketStatusTodo},
		{Title: "Second", Status: services.TicketStatusTodo},
	})

	model = model.MoveDown()
	model = model.MoveDown()
	if model.SelectedIndex() != 1 {
		t.Fatalf("selection should stop at last item, got %d", model.SelectedIndex())
	}

	model = model.MoveUp()
	model = model.MoveUp()
	if model.SelectedIndex() != 0 {
		t.Fatalf("selection should stop at first item, got %d", model.SelectedIndex())
	}
}

func TestQueueModelRendersEmptyState(t *testing.T) {
	view := NewQueueModel(nil).View()

	for _, want := range []string{"Forge Queue", "No tickets match this queue", "adjust filters"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected empty view to contain %q, got:\n%s", want, view)
		}
	}
}

func TestQueueModelRendersErrorState(t *testing.T) {
	view := NewQueueModelWithError(errors.New("database unavailable")).View()

	for _, want := range []string{"Forge Queue", "Unable to load queue", "database unavailable"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected error view to contain %q, got:\n%s", want, view)
		}
	}
}

func TestLoadQueueUsesSharedTicketListRequest(t *testing.T) {
	lister := &fakeTicketLister{
		tickets: []db.Ticket{{Title: "Fix auth", Status: services.TicketStatusTodo}},
	}

	model, err := LoadQueue(context.Background(), lister, Options{
		WorkspaceID: testUUID(2),
		ProjectID:   testUUID(3),
		Status:      services.TicketStatusTodo,
		Type:        services.TicketTypeBug,
		Limit:       25,
	})
	if err != nil {
		t.Fatalf("load queue: %v", err)
	}

	if lister.req.WorkspaceID != testUUID(2) || lister.req.ProjectID != testUUID(3) {
		t.Fatalf("unexpected scope: %#v", lister.req)
	}
	if lister.req.Status != services.TicketStatusTodo || lister.req.Type != services.TicketTypeBug || lister.req.Limit != 25 {
		t.Fatalf("unexpected filters: %#v", lister.req)
	}
	if !strings.Contains(model.View(), "Fix auth") {
		t.Fatalf("expected loaded ticket in view, got:\n%s", model.View())
	}
}

func TestRunRendersQueueErrorState(t *testing.T) {
	var output bytes.Buffer

	err := Run(context.Background(), &output, &fakeTicketLister{
		err: errors.New("database unavailable"),
	}, Options{})

	if err == nil {
		t.Fatal("expected load error")
	}
	for _, want := range []string{"Forge Queue", "Unable to load queue", "database unavailable"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("expected TUI output to contain %q, got:\n%s", want, output.String())
		}
	}
}

type fakeTicketLister struct {
	req     services.ListTicketsRequest
	tickets []db.Ticket
	err     error
}

func (f *fakeTicketLister) ListTickets(_ context.Context, req services.ListTicketsRequest) ([]db.Ticket, error) {
	f.req = req
	return f.tickets, f.err
}

func testUUID(seed byte) pgtype.UUID {
	var bytes [16]byte
	bytes[15] = seed
	return pgtype.UUID{Bytes: bytes, Valid: true}
}
