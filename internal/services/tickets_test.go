package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

func TestCreateTicketCreatesHumanTodoTicketAndEvent(t *testing.T) {
	store := &fakeTicketStore{}
	service := NewTicketService(store)
	workspaceID := testUUID(1)
	projectID := testUUID(2)

	ticket, err := service.CreateTicket(context.Background(), CreateTicketRequest{
		WorkspaceID:          workspaceID,
		ProjectID:            projectID,
		Title:                "Fix failing auth tests",
		Description:          "Investigate the current auth test failures and land the smallest durable fix.",
		Type:                 TicketTypeBug,
		Priority:             int32Ptr(1),
		Tags:                 []string{"backend", "tests"},
		AcceptanceCriteria:   []string{"Auth test suite passes locally"},
		VerificationCommands: []string{"go test ./..."},
		CreatedBy:            ActorHuman,
		CreatedByID:          "vivek",
	})
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	if ticket.Status != TicketStatusTodo {
		t.Fatalf("expected todo status, got %q", ticket.Status)
	}
	if ticket.CreatedBy != ActorHuman {
		t.Fatalf("expected human creator, got %q", ticket.CreatedBy)
	}
	if len(store.createdEvents) != 1 {
		t.Fatalf("expected one ticket event, got %d", len(store.createdEvents))
	}
	if store.createdEvents[0].Type != EventTicketCreated {
		t.Fatalf("expected created event, got %q", store.createdEvents[0].Type)
	}
	assertJSONStrings(t, store.createdTickets[0].VerificationCommands, []string{"go test ./..."})
}

func TestCreateTicketDefaultsAgentCreatedWorkToBacklogWithoutEnqueue(t *testing.T) {
	store := &fakeTicketStore{}
	service := NewTicketService(store)

	ticket, err := service.CreateTicket(context.Background(), CreateTicketRequest{
		WorkspaceID:          testUUID(1),
		ProjectID:            testUUID(2),
		Title:                "Add regression coverage for retry expiry",
		Description:          "Codex found retry expiry behavior without a regression test while working nearby.",
		Type:                 TicketTypeBug,
		AcceptanceCriteria:   []string{"Regression test covers expired retry behavior"},
		VerificationCommands: []string{"go test ./internal/services"},
		CreatedBy:            ActorAgent,
		CreatedByID:          "codex",
		CreationReason:       "Follow-up discovered while implementing the ticket service",
	})
	if err != nil {
		t.Fatalf("create ticket: %v", err)
	}

	if ticket.Status != TicketStatusBacklog {
		t.Fatalf("expected agent-created ticket to default to backlog, got %q", ticket.Status)
	}
}

func TestCreateTicketRejectsAgentEnqueueWithoutPermission(t *testing.T) {
	service := NewTicketService(&fakeTicketStore{})

	_, err := service.CreateTicket(context.Background(), CreateTicketRequest{
		WorkspaceID:          testUUID(1),
		ProjectID:            testUUID(2),
		Title:                "Add flaky test diagnostics",
		Description:          "The agent found a follow-up that should not become claimable without permission.",
		Type:                 TicketTypeFeature,
		AcceptanceCriteria:   []string{"Diagnostics are captured in failing test output"},
		VerificationCommands: []string{"go test ./..."},
		CreatedBy:            ActorAgent,
		CreatedByID:          "codex",
		CreationReason:       "Follow-up from test failure investigation",
		Enqueue:              true,
	})
	if !errors.Is(err, ErrEnqueuePermissionRequired) {
		t.Fatalf("expected enqueue permission error, got %v", err)
	}
}

func TestProposeTicketCreatesBacklogProposedEventWithSourceAttribution(t *testing.T) {
	store := &fakeTicketStore{}
	service := NewTicketService(store)
	sourceAttemptID := testUUID(9)

	ticket, err := service.ProposeTicket(context.Background(), CreateTicketRequest{
		WorkspaceID:          testUUID(1),
		ProjectID:            testUUID(2),
		SourceAttemptID:      sourceAttemptID,
		Title:                "Stabilize flaky auth refresh test",
		Description:          "Codex observed intermittent auth refresh failures while fixing nearby auth tests.",
		Type:                 TicketTypeBug,
		AcceptanceCriteria:   []string{"Auth refresh test passes 10 consecutive runs"},
		VerificationCommands: []string{"pnpm test auth-refresh --repeat 10"},
		RequiredCapabilities: []string{"testing"},
		CreatedBy:            ActorAgent,
		CreatedByID:          "codex",
		CreationReason:       "Follow-up discovered during attempt",
		Enqueue:              true,
		CanEnqueue:           true,
	})
	if err != nil {
		t.Fatalf("propose ticket: %v", err)
	}

	if ticket.Status != TicketStatusBacklog {
		t.Fatalf("expected proposed ticket to stay in backlog, got %q", ticket.Status)
	}
	if store.createdTickets[0].SourceAttemptID != sourceAttemptID {
		t.Fatalf("expected source attempt attribution")
	}
	if len(store.createdEvents) != 1 || store.createdEvents[0].Type != EventTicketProposed {
		t.Fatalf("expected one proposed event, got %#v", store.createdEvents)
	}
}

func TestTicketTemplatesCoverAgentCreatedWorkTypes(t *testing.T) {
	templates := TicketTemplates()
	seen := map[string]TicketTemplate{}
	for _, template := range templates {
		seen[template.Kind] = template
	}

	for _, kind := range []string{
		TemplateBug,
		TemplateFeature,
		TemplateDocumentation,
		TemplateReview,
		TemplateInvestigation,
		TemplateCleanup,
		TemplateFollowUp,
	} {
		template, ok := seen[kind]
		if !ok {
			t.Fatalf("missing template %q", kind)
		}
		if len(template.RequiredFields) == 0 {
			t.Fatalf("template %q should declare required fields", kind)
		}
		if len(template.AcceptanceHints) == 0 {
			t.Fatalf("template %q should include acceptance hints", kind)
		}
	}
}

func TestUpdateTicketPatchesMutableFieldsAndCreatesEvent(t *testing.T) {
	ticketID := testUUID(3)
	tags := []string{"backend", "mcp"}
	verificationCommands := []string{"go test ./internal/mcp"}
	store := &fakeTicketStore{}
	service := NewTicketService(store)

	ticket, err := service.UpdateTicket(context.Background(), UpdateTicketRequest{
		TicketID:             ticketID,
		Title:                stringPtr("  New title  "),
		Tags:                 &tags,
		VerificationCommands: &verificationCommands,
		ActorType:            ActorAgent,
		ActorID:              "codex",
	})
	if err != nil {
		t.Fatalf("update ticket: %v", err)
	}

	if ticket.Title != "New title" {
		t.Fatalf("expected patched title, got %q", ticket.Title)
	}
	if store.updatedTickets[0].Description.Valid {
		t.Fatalf("expected untouched description to be left to SQL patch, got %#v", store.updatedTickets[0])
	}
	if !store.updatedTickets[0].UpdateTags || store.updatedTickets[0].UpdateAcceptanceCriteria {
		t.Fatalf("expected only provided slice fields to be marked for update, got %#v", store.updatedTickets[0])
	}
	assertJSONStrings(t, store.updatedTickets[0].VerificationCommands, []string{"go test ./internal/mcp"})
	if len(store.createdEvents) != 1 || store.createdEvents[0].Type != EventTicketUpdated {
		t.Fatalf("expected updated event, got %#v", store.createdEvents)
	}
}

func TestUpdateTicketPreservesExplicitEmptyArrays(t *testing.T) {
	store := &fakeTicketStore{}
	service := NewTicketService(store)
	tags := []string{}
	acceptanceCriteria := []string{"still required"}
	verificationCommands := []string{}
	relevantPaths := []string{}

	_, err := service.UpdateTicket(context.Background(), UpdateTicketRequest{
		TicketID:             testUUID(3),
		Tags:                 &tags,
		AcceptanceCriteria:   &acceptanceCriteria,
		VerificationCommands: &verificationCommands,
		RelevantPaths:        &relevantPaths,
	})
	if err != nil {
		t.Fatalf("update ticket: %v", err)
	}

	params := store.updatedTickets[0]
	if params.Tags == nil || len(params.Tags) != 0 {
		t.Fatalf("expected explicit empty tags slice, got %#v", params.Tags)
	}
	if params.RelevantPaths == nil || len(params.RelevantPaths) != 0 {
		t.Fatalf("expected explicit empty relevant paths slice, got %#v", params.RelevantPaths)
	}
	assertJSONStrings(t, params.VerificationCommands, []string{})
}

func TestCreateTicketFromAttemptDefaultsToProposedBacklogWork(t *testing.T) {
	store := &fakeTicketStore{}
	service := NewTicketService(store)
	sourceAttemptID := testUUID(9)

	ticket, err := service.CreateTicketFromAttempt(context.Background(), CreateTicketFromAttemptRequest{
		WorkspaceID:          testUUID(1),
		ProjectID:            testUUID(2),
		SourceAttemptID:      sourceAttemptID,
		TemplateKind:         TemplateBug,
		Title:                "Stabilize failing migration smoke test",
		Description:          "The migration smoke test failed while Codex was validating the current task.",
		AcceptanceCriteria:   []string{"Migration smoke test passes locally"},
		VerificationCommands: []string{"go test ./internal/db"},
		RelevantPaths:        []string{"sql/migrations/0001_initial_schema.sql"},
		CreatedByID:          "codex",
		CreationReason:       "Follow-up from blocked attempt evidence",
	})
	if err != nil {
		t.Fatalf("create ticket from attempt: %v", err)
	}

	if ticket.Status != TicketStatusBacklog {
		t.Fatalf("expected backlog proposed work, got %q", ticket.Status)
	}
	if store.createdTickets[0].Type != TicketTypeBug {
		t.Fatalf("expected bug template type, got %q", store.createdTickets[0].Type)
	}
	if store.createdTickets[0].SourceAttemptID != sourceAttemptID {
		t.Fatalf("expected source attempt attribution")
	}
	if store.createdTickets[0].CreatedBy != ActorAgent {
		t.Fatalf("expected agent-created ticket, got %q", store.createdTickets[0].CreatedBy)
	}
	if len(store.createdEvents) != 1 || store.createdEvents[0].Type != EventTicketProposed {
		t.Fatalf("expected proposed event, got %#v", store.createdEvents)
	}
}

func TestCreateTicketFromAttemptCanCreateClaimableWorkWhenAllowed(t *testing.T) {
	store := &fakeTicketStore{}
	service := NewTicketService(store)

	ticket, err := service.CreateTicketFromAttempt(context.Background(), CreateTicketFromAttemptRequest{
		WorkspaceID:          testUUID(1),
		ProjectID:            testUUID(2),
		SourceAttemptID:      testUUID(9),
		TemplateKind:         TemplateCleanup,
		Title:                "Remove stale generated files",
		Description:          "Generated files were left stale after regeneration.",
		AcceptanceCriteria:   []string{"Generated files are refreshed"},
		VerificationCommands: []string{"go test ./..."},
		CreatedByID:          "codex",
		CreationReason:       "Cleanup discovered while validating generated code",
		Enqueue:              true,
		CanEnqueue:           true,
	})
	if err != nil {
		t.Fatalf("create claimable ticket from attempt: %v", err)
	}

	if ticket.Status != TicketStatusTodo {
		t.Fatalf("expected todo work when enqueue is allowed, got %q", ticket.Status)
	}
	if store.createdTickets[0].Type != TicketTypeCleanup {
		t.Fatalf("expected cleanup template type, got %q", store.createdTickets[0].Type)
	}
	if len(store.createdEvents) != 1 || store.createdEvents[0].Type != EventTicketCreated {
		t.Fatalf("expected created event, got %#v", store.createdEvents)
	}
}

func TestCreateTicketFromAttemptRejectsWeakAgentTicket(t *testing.T) {
	service := NewTicketService(&fakeTicketStore{})

	_, err := service.CreateTicketFromAttempt(context.Background(), CreateTicketFromAttemptRequest{
		WorkspaceID:    testUUID(1),
		ProjectID:      testUUID(2),
		TemplateKind:   TemplateBug,
		Title:          "Follow up",
		CreatedByID:    "codex",
		CreationReason: "",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	var validationErr ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	got := strings.Join(validationErr.Problems, "\n")
	for _, want := range []string{
		"source_attempt_id is required",
		"acceptance_criteria is required",
		"useful context is required",
		"creation_reason is required",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected validation problem %q in:\n%s", want, got)
		}
	}
}

func TestDecomposeTicketProposesChildrenAndDependencies(t *testing.T) {
	store := &fakeTicketStore{}
	service := NewTicketService(store)
	parentID := testUUID(8)

	result, err := service.DecomposeTicket(context.Background(), DecomposeTicketRequest{
		WorkspaceID:    testUUID(1),
		ProjectID:      testUUID(2),
		ParentID:       parentID,
		Mode:           DecomposeModePropose,
		CreatedBy:      ActorAgent,
		CreatedByID:    "planner",
		CreationReason: "Planner decomposition from Phase 2 task",
		Children: []DecomposeChildRequest{
			{
				Key:                  "contracts",
				Title:                "Define contract structs",
				Description:          "Create the shared contract structs first.",
				Type:                 TicketTypeFeature,
				AcceptanceCriteria:   []string{"Contract structs exist"},
				VerificationCommands: []string{"go test ./internal/contracts"},
				ExpectedArtifacts:    []string{"test output"},
			},
			{
				Key:                  "docs",
				Title:                "Document contract usage",
				Description:          "Explain how adapters should consume contract structs.",
				Type:                 TicketTypeDocumentation,
				AcceptanceCriteria:   []string{"Docs explain adapter usage"},
				VerificationCommands: []string{"go test ./..."},
				DependsOn:            []string{"contracts"},
			},
		},
	})
	if err != nil {
		t.Fatalf("decompose ticket: %v", err)
	}

	if len(result.Children) != 2 {
		t.Fatalf("expected two child tickets, got %#v", result.Children)
	}
	if store.createdTickets[0].ParentID != parentID || store.createdTickets[1].ParentID != parentID {
		t.Fatalf("expected parent relationship on children")
	}
	if store.createdTickets[0].RootID != parentID || store.createdTickets[1].RootID != parentID {
		t.Fatalf("expected parent to become root when root is omitted")
	}
	if store.createdTickets[0].ExpectedArtifacts[0] != "test output" {
		t.Fatalf("expected proof expectations to be preserved, got %#v", store.createdTickets[0].ExpectedArtifacts)
	}
	if len(store.createdDependencies) != 1 {
		t.Fatalf("expected one child dependency, got %#v", store.createdDependencies)
	}
	if store.createdDependencies[0].TicketID != result.Children[1].ID || store.createdDependencies[0].DependsOnTicketID != result.Children[0].ID {
		t.Fatalf("unexpected dependency: %#v children=%#v", store.createdDependencies[0], result.Children)
	}
	if len(store.createdEvents) != 2 || store.createdEvents[0].Type != EventTicketProposed || store.createdEvents[1].Type != EventTicketProposed {
		t.Fatalf("expected proposed events, got %#v", store.createdEvents)
	}
}

func TestDecomposeTicketCanCreateClaimableChildrenWhenAllowed(t *testing.T) {
	store := &fakeTicketStore{}
	service := NewTicketService(store)

	result, err := service.DecomposeTicket(context.Background(), DecomposeTicketRequest{
		WorkspaceID:    testUUID(1),
		ProjectID:      testUUID(2),
		ParentID:       testUUID(8),
		Mode:           DecomposeModeCreate,
		CanEnqueue:     true,
		CreatedBy:      ActorHuman,
		CreatedByID:    "vivek",
		CreationReason: "Manual phase planning",
		Children: []DecomposeChildRequest{
			{
				Key:                "impl",
				Title:              "Implement capability lookup",
				Description:        "Add lookup behavior.",
				Type:               TicketTypeFeature,
				AcceptanceCriteria: []string{"Lookup tests pass"},
				RelevantPaths:      []string{"internal/services"},
			},
		},
	})
	if err != nil {
		t.Fatalf("decompose ticket: %v", err)
	}
	if len(result.Children) != 1 {
		t.Fatalf("expected one child, got %#v", result.Children)
	}
	if result.Children[0].Status != TicketStatusTodo {
		t.Fatalf("expected created child to be todo, got %q", result.Children[0].Status)
	}
	if len(store.createdEvents) != 1 || store.createdEvents[0].Type != EventTicketCreated {
		t.Fatalf("expected created event, got %#v", store.createdEvents)
	}
}

func TestDecomposeTicketRejectsUnknownChildDependency(t *testing.T) {
	service := NewTicketService(&fakeTicketStore{})

	_, err := service.DecomposeTicket(context.Background(), DecomposeTicketRequest{
		WorkspaceID:    testUUID(1),
		ProjectID:      testUUID(2),
		ParentID:       testUUID(8),
		Mode:           DecomposeModePropose,
		CreatedBy:      ActorAgent,
		CreatedByID:    "planner",
		CreationReason: "Planner decomposition",
		Children: []DecomposeChildRequest{
			{
				Key:                "docs",
				Title:              "Document flow",
				Description:        "Write docs.",
				Type:               TicketTypeDocumentation,
				AcceptanceCriteria: []string{"Docs exist"},
				RelevantPaths:      []string{"docs"},
				DependsOn:          []string{"missing"},
			},
		},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	var validationErr ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if !strings.Contains(strings.Join(validationErr.Problems, "\n"), `children[0].depends_on references unknown child "missing"`) {
		t.Fatalf("unexpected validation problems: %#v", validationErr.Problems)
	}
}

func TestDecomposeTicketRejectsCyclicChildDependencies(t *testing.T) {
	service := NewTicketService(&fakeTicketStore{})

	_, err := service.DecomposeTicket(context.Background(), DecomposeTicketRequest{
		WorkspaceID:    testUUID(1),
		ProjectID:      testUUID(2),
		ParentID:       testUUID(8),
		Mode:           DecomposeModePropose,
		CreatedBy:      ActorAgent,
		CreatedByID:    "planner",
		CreationReason: "Planner decomposition",
		Children: []DecomposeChildRequest{
			{
				Key:                "api",
				Title:              "Add API surface",
				Description:        "Expose the API.",
				Type:               TicketTypeFeature,
				AcceptanceCriteria: []string{"API tests pass"},
				RelevantPaths:      []string{"internal/api"},
				DependsOn:          []string{"docs"},
			},
			{
				Key:                "docs",
				Title:              "Document API surface",
				Description:        "Document the API.",
				Type:               TicketTypeDocumentation,
				AcceptanceCriteria: []string{"Docs exist"},
				RelevantPaths:      []string{"docs"},
				DependsOn:          []string{"api"},
			},
		},
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	var validationErr ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if !strings.Contains(strings.Join(validationErr.Problems, "\n"), "children dependencies contain a cycle") {
		t.Fatalf("unexpected validation problems: %#v", validationErr.Problems)
	}
}

func TestValidateTicketRequiresQualityFields(t *testing.T) {
	service := NewTicketService(&fakeTicketStore{})

	_, err := service.CreateTicket(context.Background(), CreateTicketRequest{
		WorkspaceID: testUUID(1),
		CreatedBy:   ActorAgent,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	var validationErr ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}

	got := strings.Join(validationErr.Problems, "\n")
	for _, want := range []string{
		"title is required",
		"project_id is required",
		"type is required",
		"acceptance_criteria is required",
		"useful context is required",
		"creation_reason is required for agent-created tickets",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected validation problem %q in:\n%s", want, got)
		}
	}
}

func TestListTicketsDefaultsPaginationAndFilters(t *testing.T) {
	store := &fakeTicketStore{}
	service := NewTicketService(store)

	_, err := service.ListTickets(context.Background(), ListTicketsRequest{
		WorkspaceID: testUUID(1),
		ProjectID:   testUUID(2),
		Status:      TicketStatusBacklog,
		Type:        TicketTypeBug,
	})
	if err != nil {
		t.Fatalf("list tickets: %v", err)
	}

	params := store.listParams[0]
	if params.Limit != 50 {
		t.Fatalf("expected default limit 50, got %d", params.Limit)
	}
	if params.Offset != 0 {
		t.Fatalf("expected default offset 0, got %d", params.Offset)
	}
	if params.Status.String != TicketStatusBacklog || !params.Status.Valid {
		t.Fatalf("expected backlog status filter, got %#v", params.Status)
	}
	if params.Type.String != TicketTypeBug || !params.Type.Valid {
		t.Fatalf("expected bug type filter, got %#v", params.Type)
	}
}

func TestHumanTicketTransitionsUpdateStatusAndWriteEvents(t *testing.T) {
	ticketID := testUUID(30)
	tests := []struct {
		name           string
		run            func(*TicketService) (db.Ticket, error)
		wantStatus     string
		wantAllowed    []string
		wantEventType  string
		wantDataFields map[string]any
	}{
		{
			name: "ready backlog work",
			run: func(service *TicketService) (db.Ticket, error) {
				return service.MarkReady(context.Background(), TicketTransitionRequest{
					TicketID:  ticketID,
					ActorType: ActorHuman,
					ActorID:   "vivek",
					Reason:    "triaged for the next agent",
				})
			},
			wantStatus:    TicketStatusTodo,
			wantAllowed:   []string{TicketStatusBacklog},
			wantEventType: EventTicketReady,
			wantDataFields: map[string]any{
				"operation": "ready",
				"reason":    "triaged for the next agent",
			},
		},
		{
			name: "reopen terminal work",
			run: func(service *TicketService) (db.Ticket, error) {
				return service.Reopen(context.Background(), TicketTransitionRequest{
					TicketID:  ticketID,
					ActorType: ActorHuman,
					ActorID:   "vivek",
					Reason:    "verification needs another attempt",
				})
			},
			wantStatus:    TicketStatusTodo,
			wantAllowed:   []string{TicketStatusDone, TicketStatusFailed},
			wantEventType: EventTicketReopened,
			wantDataFields: map[string]any{
				"operation": "reopen",
				"reason":    "verification needs another attempt",
			},
		},
		{
			name: "unblock work",
			run: func(service *TicketService) (db.Ticket, error) {
				return service.Unblock(context.Background(), TicketTransitionRequest{
					TicketID:  ticketID,
					ActorType: ActorHuman,
					ActorID:   "vivek",
					Reason:    "staging secret was added",
				})
			},
			wantStatus:    TicketStatusTodo,
			wantAllowed:   []string{TicketStatusBlocked},
			wantEventType: EventTicketUnblocked,
			wantDataFields: map[string]any{
				"operation": "unblock",
				"reason":    "staging secret was added",
			},
		},
		{
			name: "request review",
			run: func(service *TicketService) (db.Ticket, error) {
				return service.RequestReview(context.Background(), TicketTransitionRequest{
					TicketID:  ticketID,
					ActorType: ActorHuman,
					ActorID:   "vivek",
					Reason:    "human decision required",
				})
			},
			wantStatus:    TicketStatusNeedsReview,
			wantAllowed:   []string{TicketStatusBlocked, TicketStatusTodo, TicketStatusFailed, TicketStatusDone},
			wantEventType: EventTicketReviewRequested,
			wantDataFields: map[string]any{
				"operation": "request_review",
				"reason":    "human decision required",
			},
		},
		{
			name: "archive active work",
			run: func(service *TicketService) (db.Ticket, error) {
				return service.Archive(context.Background(), TicketTransitionRequest{
					TicketID:  ticketID,
					ActorType: ActorHuman,
					ActorID:   "vivek",
					Reason:    "superseded by another ticket",
				})
			},
			wantStatus:    TicketStatusArchived,
			wantAllowed:   []string{TicketStatusBacklog, TicketStatusTodo, TicketStatusBlocked, TicketStatusNeedsReview, TicketStatusDone, TicketStatusFailed},
			wantEventType: EventTicketArchived,
			wantDataFields: map[string]any{
				"operation": "archive",
				"reason":    "superseded by another ticket",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeTicketStore{}
			service := NewTicketService(store)

			ticket, err := tt.run(service)
			if err != nil {
				t.Fatalf("transition ticket: %v", err)
			}

			if ticket.Status != tt.wantStatus {
				t.Fatalf("expected ticket status %q, got %q", tt.wantStatus, ticket.Status)
			}
			if len(store.transitions) != 1 {
				t.Fatalf("expected one ticket transition, got %d", len(store.transitions))
			}
			transition := store.transitions[0]
			if transition.ID != ticketID || transition.Status != tt.wantStatus {
				t.Fatalf("unexpected ticket transition: %#v", transition)
			}
			assertStringSlicesEqual(t, transition.AllowedStatuses, tt.wantAllowed)

			if len(store.createdEvents) != 1 {
				t.Fatalf("expected one transition event, got %d", len(store.createdEvents))
			}
			event := store.createdEvents[0]
			if event.Type != tt.wantEventType || event.ActorType != ActorHuman || event.ActorID.String != "vivek" || !event.ActorID.Valid {
				t.Fatalf("unexpected transition event: %#v", event)
			}
			assertJSONFields(t, event.Data, tt.wantDataFields)
			assertJSONFields(t, event.Data, map[string]any{"status": tt.wantStatus})
		})
	}
}

func TestReviewTicketApprovesOrRejectsNeedsReviewWork(t *testing.T) {
	tests := []struct {
		decision   string
		wantStatus string
	}{
		{decision: ReviewDecisionApprove, wantStatus: TicketStatusDone},
		{decision: ReviewDecisionReject, wantStatus: TicketStatusTodo},
	}

	for _, tt := range tests {
		t.Run(tt.decision, func(t *testing.T) {
			store := &fakeTicketStore{}
			service := NewTicketService(store)
			ticketID := testUUID(40)

			ticket, err := service.Review(context.Background(), ReviewTicketRequest{
				TicketID:  ticketID,
				Decision:  tt.decision,
				ActorType: ActorHuman,
				ActorID:   "reviewer",
				Reason:    "review decision",
			})
			if err != nil {
				t.Fatalf("review ticket: %v", err)
			}

			if ticket.Status != tt.wantStatus {
				t.Fatalf("expected ticket status %q, got %q", tt.wantStatus, ticket.Status)
			}
			if len(store.transitions) != 1 {
				t.Fatalf("expected one ticket transition, got %d", len(store.transitions))
			}
			assertStringSlicesEqual(t, store.transitions[0].AllowedStatuses, []string{TicketStatusNeedsReview})
			if len(store.createdEvents) != 1 {
				t.Fatalf("expected one reviewed event, got %d", len(store.createdEvents))
			}
			assertJSONFields(t, store.createdEvents[0].Data, map[string]any{
				"operation": "review",
				"decision":  tt.decision,
				"status":    tt.wantStatus,
				"reason":    "review decision",
			})
		})
	}
}

func TestHumanTicketTransitionsRejectInvalidTransitions(t *testing.T) {
	store := &fakeTicketStore{transitionErr: pgx.ErrNoRows}
	service := NewTicketService(store)

	_, err := service.MarkReady(context.Background(), TicketTransitionRequest{
		TicketID:  testUUID(50),
		ActorType: ActorHuman,
		ActorID:   "vivek",
	})
	if !errors.Is(err, ErrTicketTransitionNotAllowed) {
		t.Fatalf("expected ErrTicketTransitionNotAllowed, got %v", err)
	}
	if len(store.createdEvents) != 0 {
		t.Fatalf("expected no event for rejected transition, got %d", len(store.createdEvents))
	}
}

func TestArchiveDoesNotAllowInProgressTickets(t *testing.T) {
	store := &fakeTicketStore{}
	service := NewTicketService(store)

	_, err := service.Archive(context.Background(), TicketTransitionRequest{
		TicketID:  testUUID(55),
		ActorType: ActorHuman,
	})
	if err != nil {
		t.Fatalf("archive ticket: %v", err)
	}

	for _, allowed := range store.transitions[0].AllowedStatuses {
		if allowed == TicketStatusInProgress {
			t.Fatalf("archive should not allow %q tickets while attempts can still complete: %#v", TicketStatusInProgress, store.transitions[0].AllowedStatuses)
		}
	}
}

func TestReviewTicketRejectsUnknownDecision(t *testing.T) {
	service := NewTicketService(&fakeTicketStore{})

	_, err := service.Review(context.Background(), ReviewTicketRequest{
		TicketID:  testUUID(60),
		Decision:  "maybe",
		ActorType: ActorHuman,
	})

	var validationErr ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if !strings.Contains(strings.Join(validationErr.Problems, "\n"), "decision must be approve or reject") {
		t.Fatalf("expected decision validation problem, got %#v", validationErr.Problems)
	}
}

type fakeTicketStore struct {
	createdTickets      []db.CreateTicketParams
	updatedTickets      []db.UpdateTicketParams
	transitions         []db.TransitionTicketParams
	createdDependencies []db.CreateTicketDependencyParams
	createdEvents       []db.CreateTicketEventParams
	listParams          []db.ListTicketsParams
	transitionErr       error
}

func (s *fakeTicketStore) CreateTicket(_ context.Context, params db.CreateTicketParams) (db.Ticket, error) {
	s.createdTickets = append(s.createdTickets, params)
	return db.Ticket{
		ID:                   testUUID(byte(90 + len(s.createdTickets))),
		WorkspaceID:          params.WorkspaceID,
		ProjectID:            params.ProjectID,
		ParentID:             params.ParentID,
		RootID:               params.RootID,
		SourceAttemptID:      params.SourceAttemptID,
		SourceArtifactID:     params.SourceArtifactID,
		Title:                params.Title,
		Description:          params.Description,
		Type:                 params.Type,
		Status:               params.Status,
		Priority:             params.Priority,
		Tags:                 params.Tags,
		AcceptanceCriteria:   params.AcceptanceCriteria,
		VerificationCommands: params.VerificationCommands,
		ExpectedArtifacts:    params.ExpectedArtifacts,
		RelevantPaths:        params.RelevantPaths,
		RequiredTools:        params.RequiredTools,
		RequiredPermissions:  params.RequiredPermissions,
		Environment:          params.Environment,
		Input:                params.Input,
		InputSchema:          params.InputSchema,
		RequiredCapabilities: params.RequiredCapabilities,
		AllowedHarnesses:     params.AllowedHarnesses,
		RetryPolicy:          params.RetryPolicy,
		CreatedBy:            params.CreatedBy,
		CreatedByID:          params.CreatedByID,
		CreationReason:       params.CreationReason,
	}, nil
}

func (s *fakeTicketStore) UpdateTicket(_ context.Context, params db.UpdateTicketParams) (db.Ticket, error) {
	s.updatedTickets = append(s.updatedTickets, params)
	return db.Ticket{
		ID:                   params.ID,
		WorkspaceID:          testUUID(1),
		ProjectID:            testUUID(2),
		Title:                params.Title.String,
		Description:          params.Description.String,
		Type:                 TicketTypeFeature,
		Status:               TicketStatusTodo,
		Tags:                 params.Tags,
		AcceptanceCriteria:   params.AcceptanceCriteria,
		VerificationCommands: params.VerificationCommands,
		RelevantPaths:        params.RelevantPaths,
	}, nil
}

func (s *fakeTicketStore) TransitionTicket(_ context.Context, params db.TransitionTicketParams) (db.TransitionTicketRow, error) {
	if s.transitionErr != nil {
		return db.TransitionTicketRow{}, s.transitionErr
	}
	s.transitions = append(s.transitions, params)
	s.createdEvents = append(s.createdEvents, db.CreateTicketEventParams{
		WorkspaceID: testUUID(1),
		ProjectID:   testUUID(2),
		TicketID:    params.ID,
		Type:        params.Type,
		ActorType:   params.ActorType,
		ActorID:     params.ActorID,
		Data:        params.Data,
	})
	return db.TransitionTicketRow{
		ID:          params.ID,
		WorkspaceID: testUUID(1),
		ProjectID:   testUUID(2),
		Title:       "Transition me",
		Type:        TicketTypeFeature,
		Status:      params.Status,
	}, nil
}

func (s *fakeTicketStore) CreateTicketDependency(_ context.Context, params db.CreateTicketDependencyParams) (db.TicketDependency, error) {
	s.createdDependencies = append(s.createdDependencies, params)
	return db.TicketDependency{
		TicketID:          params.TicketID,
		DependsOnTicketID: params.DependsOnTicketID,
		WorkspaceID:       params.WorkspaceID,
		ProjectID:         params.ProjectID,
	}, nil
}

func (s *fakeTicketStore) CreateTicketEvent(_ context.Context, params db.CreateTicketEventParams) (db.TicketEvent, error) {
	s.createdEvents = append(s.createdEvents, params)
	return db.TicketEvent{
		ID:          testUUID(100),
		WorkspaceID: params.WorkspaceID,
		ProjectID:   params.ProjectID,
		TicketID:    params.TicketID,
		Type:        params.Type,
		ActorType:   params.ActorType,
		ActorID:     params.ActorID,
		Data:        params.Data,
	}, nil
}

func (s *fakeTicketStore) ListTickets(_ context.Context, params db.ListTicketsParams) ([]db.Ticket, error) {
	s.listParams = append(s.listParams, params)
	return nil, nil
}

func testUUID(seed byte) pgtype.UUID {
	var bytes [16]byte
	bytes[15] = seed
	return pgtype.UUID{Bytes: bytes, Valid: true}
}

func int32Ptr(value int32) *int32 {
	return &value
}

func stringPtr(value string) *string {
	return &value
}

func assertJSONStrings(t *testing.T, raw []byte, want []string) {
	t.Helper()

	var got []string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal json strings: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d strings, got %d: %#v", len(want), len(got), got)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("expected %q at index %d, got %q", want[i], i, got[i])
		}
	}
}

func assertStringSlicesEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("expected %#v, got %#v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %#v, got %#v", want, got)
		}
	}
}

func assertJSONFields(t *testing.T, raw []byte, want map[string]any) {
	t.Helper()
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal json object: %v", err)
	}
	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Fatalf("expected JSON field %q=%#v, got %#v in %#v", key, wantValue, got[key], got)
		}
	}
}
