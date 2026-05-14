package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

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

type fakeTicketStore struct {
	createdTickets      []db.CreateTicketParams
	createdDependencies []db.CreateTicketDependencyParams
	createdEvents       []db.CreateTicketEventParams
	listParams          []db.ListTicketsParams
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
