package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

const (
	ActorHuman  = "human"
	ActorAgent  = "agent"
	ActorSystem = "system"

	TicketTypeFeature       = "feature"
	TicketTypeBug           = "bug"
	TicketTypeDocumentation = "documentation"
	TicketTypeResearch      = "research"
	TicketTypeAnalysis      = "analysis"
	TicketTypePlanning      = "planning"
	TicketTypeReview        = "review"
	TicketTypeIntegration   = "integration"
	TicketTypeCustom        = "custom"

	TicketStatusBacklog = "backlog"
	TicketStatusTodo    = "todo"

	EventTicketCreated  = "created"
	EventTicketProposed = "proposed"
)

var ErrEnqueuePermissionRequired = errors.New("enqueue permission required")

var (
	defaultJSONObject  = []byte("{}")
	defaultJSONArray   = []byte("[]")
	defaultRetryPolicy = []byte(`{"max_attempts":3,"on_failure":"return_to_todo","requires_review_on_success":false}`)
)

type TicketStore interface {
	CreateTicket(context.Context, db.CreateTicketParams) (db.Ticket, error)
	CreateTicketDependency(context.Context, db.CreateTicketDependencyParams) (db.TicketDependency, error)
	CreateTicketEvent(context.Context, db.CreateTicketEventParams) (db.TicketEvent, error)
	ListTickets(context.Context, db.ListTicketsParams) ([]db.Ticket, error)
}

var _ TicketStore = (*db.Queries)(nil)

type TicketService struct {
	store TicketStore
}

func NewTicketService(store TicketStore) *TicketService {
	return &TicketService{store: store}
}

type CreateTicketRequest struct {
	WorkspaceID pgtype.UUID
	ProjectID   pgtype.UUID

	ParentID         pgtype.UUID
	RootID           pgtype.UUID
	SourceAttemptID  pgtype.UUID
	SourceArtifactID pgtype.UUID

	Title       string
	Description string
	Type        string
	Status      string
	Priority    *int32
	Tags        []string

	AcceptanceCriteria   []string
	VerificationCommands []string
	ExpectedArtifacts    []string
	RelevantPaths        []string
	RequiredTools        []string
	RequiredPermissions  []string
	Environment          map[string]any
	Input                map[string]any
	InputSchema          string
	RequiredCapabilities []string
	AllowedHarnesses     []string
	RetryPolicy          []byte
	Dependencies         []pgtype.UUID

	CreatedBy      string
	CreatedByID    string
	CreationReason string

	Enqueue    bool
	CanEnqueue bool
}

type ListTicketsRequest struct {
	WorkspaceID pgtype.UUID
	ProjectID   pgtype.UUID
	Status      string
	Type        string
	Offset      int32
	Limit       int32
}

type ValidationError struct {
	Problems []string
}

func (e ValidationError) Error() string {
	return "validation failed: " + strings.Join(e.Problems, "; ")
}

func (s *TicketService) CreateTicket(ctx context.Context, req CreateTicketRequest) (db.Ticket, error) {
	return s.createTicket(ctx, req, EventTicketCreated)
}

func (s *TicketService) ProposeTicket(ctx context.Context, req CreateTicketRequest) (db.Ticket, error) {
	req.Status = TicketStatusBacklog
	req.Enqueue = false
	return s.createTicket(ctx, req, EventTicketProposed)
}

func (s *TicketService) ListTickets(ctx context.Context, req ListTicketsRequest) ([]db.Ticket, error) {
	if problems := validateListTicketsRequest(req); len(problems) > 0 {
		return nil, ValidationError{Problems: problems}
	}

	limit := req.Limit
	if limit == 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	return s.store.ListTickets(ctx, db.ListTicketsParams{
		WorkspaceID: req.WorkspaceID,
		ProjectID:   req.ProjectID,
		Status:      optionalText(req.Status),
		Type:        optionalText(req.Type),
		Offset:      req.Offset,
		Limit:       limit,
	})
}

func (s *TicketService) createTicket(ctx context.Context, req CreateTicketRequest, eventType string) (db.Ticket, error) {
	normalized, err := normalizeCreateTicketRequest(req, eventType)
	if err != nil {
		return db.Ticket{}, err
	}

	ticket, err := s.store.CreateTicket(ctx, normalized.params)
	if err != nil {
		return db.Ticket{}, fmt.Errorf("create ticket: %w", err)
	}

	for _, dependencyID := range normalized.dependencies {
		_, err := s.store.CreateTicketDependency(ctx, db.CreateTicketDependencyParams{
			TicketID:          ticket.ID,
			DependsOnTicketID: dependencyID,
			WorkspaceID:       ticket.WorkspaceID,
			ProjectID:         ticket.ProjectID,
		})
		if err != nil {
			return db.Ticket{}, fmt.Errorf("create ticket dependency: %w", err)
		}
	}

	eventData, err := json.Marshal(map[string]any{
		"status": ticket.Status,
		"type":   ticket.Type,
	})
	if err != nil {
		return db.Ticket{}, fmt.Errorf("marshal ticket event data: %w", err)
	}
	_, err = s.store.CreateTicketEvent(ctx, db.CreateTicketEventParams{
		WorkspaceID: ticket.WorkspaceID,
		ProjectID:   ticket.ProjectID,
		TicketID:    ticket.ID,
		Type:        eventType,
		ActorType:   ticket.CreatedBy,
		ActorID:     ticket.CreatedByID,
		Data:        eventData,
	})
	if err != nil {
		return db.Ticket{}, fmt.Errorf("create ticket event: %w", err)
	}

	return ticket, nil
}

type normalizedCreateTicketRequest struct {
	params       db.CreateTicketParams
	dependencies []pgtype.UUID
}

func normalizeCreateTicketRequest(req CreateTicketRequest, eventType string) (normalizedCreateTicketRequest, error) {
	req = trimCreateTicketRequest(req)
	if req.CreatedBy == "" {
		req.CreatedBy = ActorHuman
	}
	if eventType == EventTicketProposed {
		req.Status = TicketStatusBacklog
	} else if req.Status == "" {
		req.Status = defaultCreateStatus(req)
	}

	if req.CreatedBy == ActorAgent && (req.Enqueue || req.Status == TicketStatusTodo) && !req.CanEnqueue {
		return normalizedCreateTicketRequest{}, ErrEnqueuePermissionRequired
	}

	if problems := validateCreateTicketRequest(req); len(problems) > 0 {
		return normalizedCreateTicketRequest{}, ValidationError{Problems: problems}
	}

	priority := int32(2)
	if req.Priority != nil {
		priority = *req.Priority
	}
	verificationCommands, err := encodeJSONArray(req.VerificationCommands)
	if err != nil {
		return normalizedCreateTicketRequest{}, fmt.Errorf("marshal verification commands: %w", err)
	}
	environment, err := encodeJSONObject(req.Environment)
	if err != nil {
		return normalizedCreateTicketRequest{}, fmt.Errorf("marshal environment: %w", err)
	}
	input, err := encodeJSONObject(req.Input)
	if err != nil {
		return normalizedCreateTicketRequest{}, fmt.Errorf("marshal input: %w", err)
	}

	retryPolicy := req.RetryPolicy
	if len(retryPolicy) == 0 {
		retryPolicy = defaultRetryPolicy
	}

	return normalizedCreateTicketRequest{
		params: db.CreateTicketParams{
			WorkspaceID:          req.WorkspaceID,
			ProjectID:            req.ProjectID,
			ParentID:             req.ParentID,
			RootID:               req.RootID,
			SourceAttemptID:      req.SourceAttemptID,
			SourceArtifactID:     req.SourceArtifactID,
			Title:                req.Title,
			Description:          req.Description,
			Type:                 req.Type,
			Status:               req.Status,
			Priority:             priority,
			Tags:                 req.Tags,
			AcceptanceCriteria:   req.AcceptanceCriteria,
			VerificationCommands: verificationCommands,
			ExpectedArtifacts:    req.ExpectedArtifacts,
			RelevantPaths:        req.RelevantPaths,
			RequiredTools:        req.RequiredTools,
			RequiredPermissions:  req.RequiredPermissions,
			Environment:          environment,
			Input:                input,
			InputSchema:          optionalText(req.InputSchema),
			RequiredCapabilities: req.RequiredCapabilities,
			AllowedHarnesses:     req.AllowedHarnesses,
			RetryPolicy:          retryPolicy,
			CreatedBy:            req.CreatedBy,
			CreatedByID:          optionalText(req.CreatedByID),
			CreationReason:       optionalText(req.CreationReason),
		},
		dependencies: req.Dependencies,
	}, nil
}

func validateCreateTicketRequest(req CreateTicketRequest) []string {
	var problems []string
	if !req.WorkspaceID.Valid {
		problems = append(problems, "workspace_id is required")
	}
	if !req.ProjectID.Valid {
		problems = append(problems, "project_id is required")
	}
	if req.Title == "" {
		problems = append(problems, "title is required")
	}
	if req.Type == "" {
		problems = append(problems, "type is required")
	} else if !isAllowedTicketType(req.Type) {
		problems = append(problems, "type must be one of feature, bug, documentation, research, analysis, planning, review, integration, custom")
	}
	if req.Status == "" {
		problems = append(problems, "status is required")
	} else if req.Status != TicketStatusBacklog && req.Status != TicketStatusTodo {
		problems = append(problems, "status must be backlog or todo when creating tickets")
	}
	if req.Priority != nil && (*req.Priority < 0 || *req.Priority > 4) {
		problems = append(problems, "priority must be between 0 and 4")
	}
	if !isAllowedActor(req.CreatedBy) {
		problems = append(problems, "created_by must be human, agent, or system")
	}
	if len(req.AcceptanceCriteria) == 0 {
		problems = append(problems, "acceptance_criteria is required")
	}
	if !hasUsefulContext(req) {
		problems = append(problems, "useful context is required")
	}
	if req.CreatedBy == ActorAgent && req.CreationReason == "" {
		problems = append(problems, "creation_reason is required for agent-created tickets")
	}
	if len(req.RetryPolicy) > 0 && !json.Valid(req.RetryPolicy) {
		problems = append(problems, "retry_policy must be valid JSON")
	}
	for _, dependencyID := range req.Dependencies {
		if !dependencyID.Valid {
			problems = append(problems, "dependencies must contain valid ticket ids")
			break
		}
	}
	return problems
}

func validateListTicketsRequest(req ListTicketsRequest) []string {
	var problems []string
	if !req.WorkspaceID.Valid {
		problems = append(problems, "workspace_id is required")
	}
	if !req.ProjectID.Valid {
		problems = append(problems, "project_id is required")
	}
	if req.Status != "" && !isAllowedTicketListStatus(req.Status) {
		problems = append(problems, "status filter is not valid")
	}
	if req.Type != "" && !isAllowedTicketType(req.Type) {
		problems = append(problems, "type filter is not valid")
	}
	if req.Offset < 0 {
		problems = append(problems, "offset must be non-negative")
	}
	if req.Limit < 0 {
		problems = append(problems, "limit must be non-negative")
	}
	return problems
}

func defaultCreateStatus(req CreateTicketRequest) string {
	if req.CreatedBy == ActorAgent && !req.Enqueue {
		return TicketStatusBacklog
	}
	return TicketStatusTodo
}

func hasUsefulContext(req CreateTicketRequest) bool {
	return req.Description != "" ||
		len(req.RelevantPaths) > 0 ||
		len(req.VerificationCommands) > 0 ||
		len(req.ExpectedArtifacts) > 0 ||
		len(req.Input) > 0
}

func trimCreateTicketRequest(req CreateTicketRequest) CreateTicketRequest {
	req.Title = strings.TrimSpace(req.Title)
	req.Description = strings.TrimSpace(req.Description)
	req.Type = strings.TrimSpace(req.Type)
	req.Status = strings.TrimSpace(req.Status)
	req.CreatedBy = strings.TrimSpace(req.CreatedBy)
	req.CreatedByID = strings.TrimSpace(req.CreatedByID)
	req.CreationReason = strings.TrimSpace(req.CreationReason)
	req.InputSchema = strings.TrimSpace(req.InputSchema)
	req.Tags = compactStrings(req.Tags)
	req.AcceptanceCriteria = compactStrings(req.AcceptanceCriteria)
	req.VerificationCommands = compactStrings(req.VerificationCommands)
	req.ExpectedArtifacts = compactStrings(req.ExpectedArtifacts)
	req.RelevantPaths = compactStrings(req.RelevantPaths)
	req.RequiredTools = compactStrings(req.RequiredTools)
	req.RequiredPermissions = compactStrings(req.RequiredPermissions)
	req.RequiredCapabilities = compactStrings(req.RequiredCapabilities)
	req.AllowedHarnesses = compactStrings(req.AllowedHarnesses)
	return req
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func optionalText(value string) pgtype.Text {
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

func encodeJSONObject(value map[string]any) ([]byte, error) {
	if len(value) == 0 {
		return defaultJSONObject, nil
	}
	return json.Marshal(value)
}

func encodeJSONArray(value []string) ([]byte, error) {
	if len(value) == 0 {
		return defaultJSONArray, nil
	}
	return json.Marshal(value)
}

func isAllowedActor(value string) bool {
	switch value {
	case ActorHuman, ActorAgent, ActorSystem:
		return true
	default:
		return false
	}
}

func isAllowedTicketType(value string) bool {
	switch value {
	case TicketTypeFeature,
		TicketTypeBug,
		TicketTypeDocumentation,
		TicketTypeResearch,
		TicketTypeAnalysis,
		TicketTypePlanning,
		TicketTypeReview,
		TicketTypeIntegration,
		TicketTypeCustom:
		return true
	default:
		return false
	}
}

func isAllowedTicketListStatus(value string) bool {
	switch value {
	case TicketStatusBacklog,
		TicketStatusTodo,
		"in_progress",
		"blocked",
		"needs_review",
		"done",
		"failed",
		"archived":
		return true
	default:
		return false
	}
}
