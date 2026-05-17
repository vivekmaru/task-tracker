package tui

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

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
		"forge get --id 00000000-0000-0000-0000-000000000011",
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

func TestTicketDetailModelUsesNewestAttemptWhenNoNewestActiveAttemptExists(t *testing.T) {
	ticket := detailTicketFixture()
	attempts := []db.Attempt{
		{ID: testUUID(61), TicketID: ticket.ID, Status: services.AttemptStatusSucceeded, AgentID: "codex", Harness: "codex", Model: "gpt-5"},
		{ID: testUUID(62), TicketID: ticket.ID, Status: services.AttemptStatusBlocked, AgentID: "opencode", Harness: "opencode", Model: "sonnet"},
	}

	view := NewTicketDetailModel(ticket, attempts).View()

	current := strings.Index(view, "Current attempt")
	succeeded := strings.Index(view, "succeeded codex/gpt-5")
	prior := strings.Index(view, "Prior attempts")
	blocked := strings.Index(view, "blocked opencode/sonnet")
	if current == -1 || succeeded == -1 || prior == -1 || blocked == -1 {
		t.Fatalf("expected current and prior attempt sections, got:\n%s", view)
	}
	if !(current < succeeded && succeeded < prior && prior < blocked) {
		t.Fatalf("expected newest terminal attempt as current and older blocked attempt as prior, got:\n%s", view)
	}
}

func TestTicketDetailModelRendersTimelineSectionsInOrderAndStates(t *testing.T) {
	ticket := detailTicketFixture()
	timeline := TicketTimeline{
		Attempts: []db.Attempt{
			{
				ID:              testUUID(91),
				TicketID:        ticket.ID,
				Status:          services.AttemptStatusRunning,
				AgentID:         "codex",
				Harness:         "codex",
				Model:           "gpt-5",
				ProgressPercent: 60,
			},
			{
				ID:        testUUID(92),
				TicketID:  ticket.ID,
				Status:    services.AttemptStatusBlocked,
				AgentID:   "opencode",
				Harness:   "opencode",
				Model:     "sonnet",
				Blocker:   []byte(`{"reason":"waiting on staging secrets"}`),
				StartedAt: testTimestamp(9),
			},
			{
				ID:          testUUID(93),
				TicketID:    ticket.ID,
				Status:      services.AttemptStatusSucceeded,
				AgentID:     "verifier",
				Harness:     "codex",
				Model:       "gpt-5",
				CompletedAt: testTimestamp(10),
			},
		},
		Checkpoints: []db.AttemptCheckpoint{
			{ID: testUUID(101), TicketID: ticket.ID, AttemptID: testUUID(91), Summary: "first checkpoint", CommandsRun: []string{"go test ./internal/tui"}, FilesTouched: []string{"internal/tui/detail.go"}, CreatedAt: testTimestamp(1)},
			{ID: testUUID(102), TicketID: ticket.ID, AttemptID: testUUID(91), Summary: "second checkpoint", NextStep: pgtype.Text{String: "inspect artifact", Valid: true}, Risk: pgtype.Text{String: "low", Valid: true}, CreatedAt: testTimestamp(2)},
		},
		Events: []db.TicketEvent{
			{ID: testUUID(111), TicketID: ticket.ID, AttemptID: testUUID(91), Type: "ticket.created", ActorType: services.ActorAgent, ActorID: pgtype.Text{String: "codex", Valid: true}, CreatedAt: testTimestamp(3)},
			{ID: testUUID(112), TicketID: ticket.ID, AttemptID: testUUID(91), Type: "attempt.blocked", ActorType: services.ActorAgent, ActorID: pgtype.Text{String: "opencode", Valid: true}, Data: []byte(`{"reason":"waiting on staging secrets"}`), CreatedAt: testTimestamp(4)},
		},
		Artifacts: []db.Artifact{
			{ID: testUUID(121), TicketID: ticket.ID, AttemptID: testUUID(91), Type: "log", Role: "proof", Name: "test-output.txt", Url: "file:///tmp/test-output.txt", CreatedAt: testTimestamp(5)},
			{ID: testUUID(122), TicketID: ticket.ID, AttemptID: testUUID(91), Type: "trace", Role: "debug", Name: "trace.json", CreatedAt: testTimestamp(6)},
		},
	}

	view := NewTicketDetailModelWithTimeline(ticket, timeline).View()

	for _, want := range []string{
		"Attempts timeline",
		"active running codex/gpt-5 60%",
		"blocked opencode/sonnet",
		"Blocker: waiting on staging secrets",
		"terminal succeeded verifier/gpt-5",
		"Checkpoints timeline",
		"first checkpoint",
		"Commands: go test ./internal/tui",
		"Files: internal/tui/detail.go",
		"second checkpoint",
		"Next: inspect artifact",
		"Risk: low",
		"Events timeline",
		"ticket.created by agent/codex",
		"attempt.blocked by agent/opencode",
		"waiting on staging secrets",
		"Proof artifacts",
		"proof log test-output.txt",
		"file:///tmp/test-output.txt",
		"debug trace trace.json",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected timeline view to contain %q, got:\n%s", want, view)
		}
	}
	assertOrdered(t, view, "first checkpoint", "second checkpoint")
	assertOrdered(t, view, "ticket.created", "attempt.blocked")
	assertOrdered(t, view, "test-output.txt", "trace.json")
}

func TestTicketDetailModelRendersTimelineEmptyStates(t *testing.T) {
	view := NewTicketDetailModelWithTimeline(detailTicketFixture(), TicketTimeline{}).View()

	for _, want := range []string{
		"Attempts timeline",
		"No attempts recorded yet.",
		"Checkpoints timeline",
		"No checkpoints recorded.",
		"Events timeline",
		"No ticket events recorded.",
		"Proof artifacts",
		"No proof artifacts recorded.",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected empty timeline view to contain %q, got:\n%s", want, view)
		}
	}
}

func TestLoadTicketTimelineUsesSharedTimelineQueries(t *testing.T) {
	ticketID := testUUID(130)
	loader := &fakeDetailLoader{
		attempts: map[pgtype.UUID][]db.Attempt{
			ticketID: {{ID: testUUID(131), TicketID: ticketID, Status: services.AttemptStatusRunning}},
		},
		checkpoints: []db.AttemptCheckpoint{{ID: testUUID(132), TicketID: ticketID, Summary: "checkpoint"}},
		events:      []db.TicketEvent{{ID: testUUID(133), TicketID: ticketID, Type: "ticket.created"}},
		artifacts:   []db.Artifact{{ID: testUUID(134), TicketID: ticketID, Name: "proof.txt"}},
	}

	timeline, err := LoadTicketTimeline(context.Background(), loader, ticketID)
	if err != nil {
		t.Fatalf("load ticket timeline: %v", err)
	}

	if loader.requested != ticketID || loader.checkpointTicketID != ticketID || loader.eventTicketID != ticketID || loader.artifactTicketID != ticketID {
		t.Fatalf("expected all timeline queries to use ticket id %v, got loader %#v", ticketID, loader)
	}
	if len(timeline.Attempts) != 1 || len(timeline.Checkpoints) != 1 || len(timeline.Events) != 1 || len(timeline.Artifacts) != 1 {
		t.Fatalf("unexpected timeline payload: %#v", timeline)
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

func TestQueueModelIgnoresStaleDetailLoadResponses(t *testing.T) {
	first := db.Ticket{ID: testUUID(71), Title: "First", Status: services.TicketStatusTodo}
	second := db.Ticket{ID: testUUID(72), Title: "Second", Status: services.TicketStatusTodo}
	loader := &fakeDetailLoader{
		attempts: map[pgtype.UUID][]db.Attempt{
			first.ID: {
				{ID: testUUID(73), TicketID: first.ID, Status: services.AttemptStatusRunning, AgentID: "first-agent", Harness: "codex", Model: "gpt-5"},
			},
			second.ID: {
				{ID: testUUID(74), TicketID: second.ID, Status: services.AttemptStatusRunning, AgentID: "second-agent", Harness: "codex", Model: "gpt-5"},
			},
		},
	}
	model := NewQueueModel([]db.Ticket{first, second}).WithDetailLoader(context.Background(), loader)

	updated, firstCmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if firstCmd == nil {
		t.Fatal("expected first enter to start detail load")
	}
	queueAfterFirstEnter := updated.(QueueModel).MoveDown()
	updated, secondCmd := queueAfterFirstEnter.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if secondCmd == nil {
		t.Fatal("expected second enter to start detail load")
	}

	updated, _ = updated.Update(secondCmd())
	updated, _ = updated.Update(firstCmd())
	view := updated.View()

	if strings.Contains(view, "First") || strings.Contains(view, "first-agent") {
		t.Fatalf("stale first detail response should not overwrite newer detail, got:\n%s", view)
	}
	for _, want := range []string{"Ticket Detail", "Second", "second-agent/gpt-5"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected latest detail view to contain %q, got:\n%s", want, view)
		}
	}
}

func TestQueueModelIgnoresStaleSameTicketDetailLoadResponses(t *testing.T) {
	ticket := db.Ticket{ID: testUUID(81), Title: "Same", Status: services.TicketStatusTodo}
	model := NewQueueModel([]db.Ticket{ticket}).WithDetailLoader(context.Background(), &fakeDetailLoader{})

	updated, firstCmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if firstCmd == nil {
		t.Fatal("expected first enter to start detail load")
	}
	updated, secondCmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if secondCmd == nil {
		t.Fatal("expected second enter to start detail load")
	}

	updated, _ = updated.Update(detailLoadedMsg{
		requestSeq: 2,
		ticket:     ticket,
		timeline: TicketTimeline{
			Attempts: []db.Attempt{{ID: testUUID(82), TicketID: ticket.ID, Status: services.AttemptStatusRunning, AgentID: "fresh-agent", Harness: "codex", Model: "gpt-5", ProgressPercent: 90}},
		},
	})
	updated, _ = updated.Update(detailLoadedMsg{
		requestSeq: 1,
		ticket:     ticket,
		timeline: TicketTimeline{
			Attempts: []db.Attempt{{ID: testUUID(83), TicketID: ticket.ID, Status: services.AttemptStatusRunning, AgentID: "stale-agent", Harness: "codex", Model: "gpt-5", ProgressPercent: 10}},
		},
	})
	view := updated.View()

	if strings.Contains(view, "stale-agent") || strings.Contains(view, "10%") {
		t.Fatalf("stale same-ticket detail response should not overwrite newer detail, got:\n%s", view)
	}
	for _, want := range []string{"Ticket Detail", "Same", "fresh-agent/gpt-5 90%"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected latest same-ticket detail view to contain %q, got:\n%s", want, view)
		}
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
	requested          pgtype.UUID
	checkpointTicketID pgtype.UUID
	eventTicketID      pgtype.UUID
	artifactTicketID   pgtype.UUID
	attempts           map[pgtype.UUID][]db.Attempt
	checkpoints        []db.AttemptCheckpoint
	events             []db.TicketEvent
	artifacts          []db.Artifact
	err                error
}

func (f *fakeDetailLoader) ListAttemptsByTicket(_ context.Context, ticketID pgtype.UUID) ([]db.Attempt, error) {
	f.requested = ticketID
	return f.attempts[ticketID], f.err
}

func (f *fakeDetailLoader) ListAttemptCheckpointsByTicket(_ context.Context, ticketID pgtype.UUID) ([]db.AttemptCheckpoint, error) {
	f.checkpointTicketID = ticketID
	return f.checkpoints, f.err
}

func (f *fakeDetailLoader) ListTicketEventsByTicket(_ context.Context, ticketID pgtype.UUID) ([]db.TicketEvent, error) {
	f.eventTicketID = ticketID
	return f.events, f.err
}

func (f *fakeDetailLoader) ListArtifactsByTicket(_ context.Context, ticketID pgtype.UUID) ([]db.Artifact, error) {
	f.artifactTicketID = ticketID
	return f.artifacts, f.err
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

func testTimestamp(hour int) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: time.Date(2026, time.May, 17, hour, 0, 0, 0, time.UTC), Valid: true}
}

func assertOrdered(t *testing.T, text, before, after string) {
	t.Helper()
	beforeIndex := strings.Index(text, before)
	afterIndex := strings.Index(text, after)
	if beforeIndex == -1 || afterIndex == -1 {
		t.Fatalf("expected %q and %q in text:\n%s", before, after, text)
	}
	if beforeIndex >= afterIndex {
		t.Fatalf("expected %q before %q in text:\n%s", before, after, text)
	}
}
