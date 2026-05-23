package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

const (
	ActorHuman  = "human"
	ActorAgent  = "agent"
	ActorSystem = "system"

	TicketTypeFeature       = "feature"
	TicketTypeTask          = "task"
	TicketTypeBug           = "bug"
	TicketTypeDocumentation = "documentation"
	TicketTypeResearch      = "research"
	TicketTypeAnalysis      = "analysis"
	TicketTypePlanning      = "planning"
	TicketTypeReview        = "review"
	TicketTypeIntegration   = "integration"
	TicketTypeInvestigation = "investigation"
	TicketTypeCleanup       = "cleanup"
	TicketTypeFollowUp      = "follow_up"
	TicketTypeCustom        = "custom"

	TicketStatusBacklog     = "backlog"
	TicketStatusTodo        = "todo"
	TicketStatusInProgress  = "in_progress"
	TicketStatusBlocked     = "blocked"
	TicketStatusNeedsReview = "needs_review"
	TicketStatusDone        = "done"
	TicketStatusFailed      = "failed"
	TicketStatusArchived    = "archived"

	EventTicketCreated         = "created"
	EventTicketProposed        = "proposed"
	EventTicketUpdated         = "updated"
	EventTicketReady           = "ready"
	EventTicketReopened        = "reopened"
	EventTicketUnblocked       = "unblocked"
	EventTicketReviewRequested = "review_requested"
	EventTicketReviewed        = "reviewed"
	EventTicketArchived        = "archived"

	ReviewDecisionApprove = "approve"
	ReviewDecisionReject  = "reject"
)

var ErrEnqueuePermissionRequired = errors.New("enqueue permission required")
var ErrTicketNotFound = errors.New("ticket not found")
var ErrTicketIsNotProposed = errors.New("ticket is not proposed work")
var ErrTicketTransitionNotAllowed = errors.New("ticket transition is not allowed")

var (
	defaultJSONObject  = []byte("{}")
	defaultJSONArray   = []byte("[]")
	defaultRetryPolicy = []byte(`{"max_attempts":3,"on_failure":"return_to_todo","requires_review_on_success":false}`)
)

type TicketStore interface {
	CreateTicket(context.Context, db.CreateTicketParams) (db.Ticket, error)
	GetTicket(context.Context, pgtype.UUID) (db.Ticket, error)
	UpdateTicket(context.Context, db.UpdateTicketParams) (db.Ticket, error)
	TransitionTicket(context.Context, db.TransitionTicketParams) (db.TransitionTicketRow, error)
	CreateTicketDependency(context.Context, db.CreateTicketDependencyParams) (db.TicketDependency, error)
	CreateTicketEvent(context.Context, db.CreateTicketEventParams) (db.TicketEvent, error)
	ListTickets(context.Context, db.ListTicketsParams) ([]db.Ticket, error)
	ListProposedTickets(context.Context, db.ListProposedTicketsParams) ([]db.Ticket, error)
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

type UpdateTicketRequest struct {
	TicketID pgtype.UUID

	Title                *string
	Description          *string
	Tags                 *[]string
	AcceptanceCriteria   *[]string
	VerificationCommands *[]string
	RelevantPaths        *[]string

	ActorType string
	ActorID   string
	eventData map[string]any
}

type TicketTransitionRequest struct {
	TicketID  pgtype.UUID
	ActorType string
	ActorID   string
	Reason    string
}

type ReviewTicketRequest struct {
	TicketID  pgtype.UUID
	Decision  string
	ActorType string
	ActorID   string
	Reason    string
}

type ListProposedTicketsRequest struct {
	WorkspaceID pgtype.UUID
	ProjectID   pgtype.UUID
	Type        string
	Offset      int32
	Limit       int32
}

type ProposedTicketTriageItem struct {
	Ticket               db.Ticket
	SourceAttemptID      pgtype.UUID
	SourceArtifactID     pgtype.UUID
	CreatedByID          string
	CreationReason       string
	AcceptanceCriteria   []string
	VerificationCommands []string
	RelevantPaths        []string
}

type ProposedTicketTriageRequest struct {
	TicketID   pgtype.UUID
	ActorType  string
	ActorID    string
	Reason     string
	CanEnqueue bool
}

type MergeProposedTicketRequest struct {
	TicketID       pgtype.UUID
	TargetTicketID pgtype.UUID
	ActorType      string
	ActorID        string
	Reason         string
}

type RefineProposedTicketRequest struct {
	TicketID pgtype.UUID

	Title                *string
	Description          *string
	Tags                 *[]string
	AcceptanceCriteria   *[]string
	VerificationCommands *[]string
	RelevantPaths        *[]string

	ActorType string
	ActorID   string
	Reason    string
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

func (s *TicketService) ListProposedTickets(ctx context.Context, req ListProposedTicketsRequest) ([]ProposedTicketTriageItem, error) {
	req.Type = strings.TrimSpace(req.Type)
	if problems := validateListProposedTicketsRequest(req); len(problems) > 0 {
		return nil, ValidationError{Problems: problems}
	}
	if req.Limit <= 0 {
		req.Limit = 50
	}
	if req.Limit > 200 {
		req.Limit = 200
	}
	tickets, err := s.store.ListProposedTickets(ctx, db.ListProposedTicketsParams{
		WorkspaceID: req.WorkspaceID,
		ProjectID:   req.ProjectID,
		Type:        optionalText(req.Type),
		Offset:      req.Offset,
		Limit:       req.Limit,
	})
	if err != nil {
		return nil, err
	}

	items := make([]ProposedTicketTriageItem, 0, len(tickets))
	for _, ticket := range tickets {
		if ticket.CreatedBy != ActorAgent {
			continue
		}
		verificationCommands, err := decodeJSONArray(ticket.VerificationCommands)
		if err != nil {
			return nil, fmt.Errorf("decode proposed ticket verification commands: %w", err)
		}
		items = append(items, ProposedTicketTriageItem{
			Ticket:               ticket,
			SourceAttemptID:      ticket.SourceAttemptID,
			SourceArtifactID:     ticket.SourceArtifactID,
			CreatedByID:          textValue(ticket.CreatedByID),
			CreationReason:       textValue(ticket.CreationReason),
			AcceptanceCriteria:   append([]string(nil), ticket.AcceptanceCriteria...),
			VerificationCommands: verificationCommands,
			RelevantPaths:        append([]string(nil), ticket.RelevantPaths...),
		})
	}
	return items, nil
}

func (s *TicketService) UpdateTicket(ctx context.Context, req UpdateTicketRequest) (db.Ticket, error) {
	req = trimUpdateTicketRequest(req)
	if req.ActorType == "" {
		req.ActorType = ActorAgent
	}
	if problems := validateUpdateTicketRequest(req); len(problems) > 0 {
		return db.Ticket{}, ValidationError{Problems: problems}
	}

	var changedFields []string
	params := db.UpdateTicketParams{ID: req.TicketID}
	if req.Title != nil {
		params.Title = requiredText(*req.Title)
		changedFields = append(changedFields, "title")
	}
	if req.Description != nil {
		params.Description = requiredText(*req.Description)
		changedFields = append(changedFields, "description")
	}
	if req.Tags != nil {
		params.UpdateTags = true
		params.Tags = *req.Tags
		changedFields = append(changedFields, "tags")
	}
	if req.AcceptanceCriteria != nil {
		params.UpdateAcceptanceCriteria = true
		params.AcceptanceCriteria = *req.AcceptanceCriteria
		changedFields = append(changedFields, "acceptance_criteria")
	}
	if req.VerificationCommands != nil {
		verificationRaw, err := encodeJSONArray(*req.VerificationCommands)
		if err != nil {
			return db.Ticket{}, fmt.Errorf("marshal verification commands: %w", err)
		}
		params.UpdateVerificationCommands = true
		params.VerificationCommands = verificationRaw
		changedFields = append(changedFields, "verification_commands")
	}
	if req.RelevantPaths != nil {
		params.UpdateRelevantPaths = true
		params.RelevantPaths = *req.RelevantPaths
		changedFields = append(changedFields, "relevant_paths")
	}

	ticket, err := s.store.UpdateTicket(ctx, params)
	if err != nil {
		return db.Ticket{}, fmt.Errorf("update ticket: %w", err)
	}

	data := map[string]any{
		"changed_fields": changedFields,
	}
	for key, value := range req.eventData {
		if value != "" {
			data[key] = value
		}
	}
	eventData, err := json.Marshal(data)
	if err != nil {
		return db.Ticket{}, fmt.Errorf("marshal ticket event data: %w", err)
	}
	_, err = s.store.CreateTicketEvent(ctx, db.CreateTicketEventParams{
		WorkspaceID: ticket.WorkspaceID,
		ProjectID:   ticket.ProjectID,
		TicketID:    ticket.ID,
		Type:        EventTicketUpdated,
		ActorType:   req.ActorType,
		ActorID:     optionalText(req.ActorID),
		Data:        eventData,
	})
	if err != nil {
		return db.Ticket{}, fmt.Errorf("create ticket event: %w", err)
	}

	return ticket, nil
}

func (s *TicketService) ReadyProposedTicket(ctx context.Context, req ProposedTicketTriageRequest) (db.Ticket, error) {
	return s.transitionProposedTicket(ctx, req, "ready_proposed", EventTicketReady, TicketStatusTodo)
}

func (s *TicketService) EnqueueProposedTicket(ctx context.Context, req ProposedTicketTriageRequest) (db.Ticket, error) {
	req = trimProposedTicketTriageRequest(req)
	if req.ActorType == "" {
		req.ActorType = ActorHuman
	}
	if req.ActorType == ActorAgent && !req.CanEnqueue {
		return db.Ticket{}, ErrEnqueuePermissionRequired
	}
	return s.transitionProposedTicket(ctx, req, "enqueue_proposed", EventTicketReady, TicketStatusTodo)
}

func (s *TicketService) RejectProposedTicket(ctx context.Context, req ProposedTicketTriageRequest) (db.Ticket, error) {
	return s.transitionProposedTicket(ctx, req, "reject_proposed", EventTicketArchived, TicketStatusArchived)
}

func (s *TicketService) ArchiveProposedTicket(ctx context.Context, req ProposedTicketTriageRequest) (db.Ticket, error) {
	return s.transitionProposedTicket(ctx, req, "archive_proposed", EventTicketArchived, TicketStatusArchived)
}

func (s *TicketService) MergeProposedTicket(ctx context.Context, req MergeProposedTicketRequest) (db.Ticket, error) {
	req = trimMergeProposedTicketRequest(req)
	if req.ActorType == "" {
		req.ActorType = ActorHuman
	}
	if problems := validateMergeProposedTicketRequest(req); len(problems) > 0 {
		return db.Ticket{}, ValidationError{Problems: problems}
	}
	proposed, err := s.requireProposedTicket(ctx, req.TicketID)
	if err != nil {
		return db.Ticket{}, err
	}
	target, err := s.store.GetTicket(ctx, req.TargetTicketID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Ticket{}, ErrTicketNotFound
		}
		return db.Ticket{}, fmt.Errorf("get proposed ticket merge target: %w", err)
	}
	if target.WorkspaceID != proposed.WorkspaceID || target.ProjectID != proposed.ProjectID {
		return db.Ticket{}, ValidationError{Problems: []string{"target_ticket_id must belong to the same workspace and project"}}
	}
	return s.setTicketStatus(ctx, setTicketStatusRequest{
		ticketID:        req.TicketID,
		status:          TicketStatusArchived,
		allowedStatuses: []string{TicketStatusBacklog},
		eventType:       EventTicketArchived,
		actorType:       req.ActorType,
		actorID:         req.ActorID,
		data: map[string]any{
			"operation":             "merge_proposed",
			"reason":                req.Reason,
			"merged_into_ticket_id": ticketUUIDString(req.TargetTicketID),
		},
	})
}

func (s *TicketService) RefineProposedTicket(ctx context.Context, req RefineProposedTicketRequest) (db.Ticket, error) {
	req = trimRefineProposedTicketRequest(req)
	if req.ActorType == "" {
		req.ActorType = ActorHuman
	}
	if problems := validateRefineProposedTicketRequest(req); len(problems) > 0 {
		return db.Ticket{}, ValidationError{Problems: problems}
	}
	if _, err := s.requireProposedTicket(ctx, req.TicketID); err != nil {
		return db.Ticket{}, err
	}
	return s.UpdateTicket(ctx, UpdateTicketRequest{
		TicketID:             req.TicketID,
		Title:                req.Title,
		Description:          req.Description,
		Tags:                 req.Tags,
		AcceptanceCriteria:   req.AcceptanceCriteria,
		VerificationCommands: req.VerificationCommands,
		RelevantPaths:        req.RelevantPaths,
		ActorType:            req.ActorType,
		ActorID:              req.ActorID,
		eventData: map[string]any{
			"operation": "refine_proposed",
			"reason":    req.Reason,
		},
	})
}

func (s *TicketService) MarkReady(ctx context.Context, req TicketTransitionRequest) (db.Ticket, error) {
	return s.transitionTicket(ctx, req, ticketTransitionSpec{
		operation:       "ready",
		eventType:       EventTicketReady,
		targetStatus:    TicketStatusTodo,
		allowedStatuses: []string{TicketStatusBacklog},
	})
}

func (s *TicketService) Reopen(ctx context.Context, req TicketTransitionRequest) (db.Ticket, error) {
	return s.transitionTicket(ctx, req, ticketTransitionSpec{
		operation:       "reopen",
		eventType:       EventTicketReopened,
		targetStatus:    TicketStatusTodo,
		allowedStatuses: []string{TicketStatusDone, TicketStatusFailed},
	})
}

func (s *TicketService) Unblock(ctx context.Context, req TicketTransitionRequest) (db.Ticket, error) {
	return s.transitionTicket(ctx, req, ticketTransitionSpec{
		operation:       "unblock",
		eventType:       EventTicketUnblocked,
		targetStatus:    TicketStatusTodo,
		allowedStatuses: []string{TicketStatusBlocked},
	})
}

func (s *TicketService) RequestReview(ctx context.Context, req TicketTransitionRequest) (db.Ticket, error) {
	return s.transitionTicket(ctx, req, ticketTransitionSpec{
		operation:       "request_review",
		eventType:       EventTicketReviewRequested,
		targetStatus:    TicketStatusNeedsReview,
		allowedStatuses: []string{TicketStatusBlocked, TicketStatusTodo, TicketStatusFailed, TicketStatusDone},
	})
}

func (s *TicketService) Archive(ctx context.Context, req TicketTransitionRequest) (db.Ticket, error) {
	return s.transitionTicket(ctx, req, ticketTransitionSpec{
		operation:    "archive",
		eventType:    EventTicketArchived,
		targetStatus: TicketStatusArchived,
		allowedStatuses: []string{
			TicketStatusBacklog,
			TicketStatusTodo,
			TicketStatusBlocked,
			TicketStatusNeedsReview,
			TicketStatusDone,
			TicketStatusFailed,
		},
	})
}

func (s *TicketService) Review(ctx context.Context, req ReviewTicketRequest) (db.Ticket, error) {
	req = trimReviewTicketRequest(req)
	if req.ActorType == "" {
		req.ActorType = ActorHuman
	}
	if problems := validateReviewTicketRequest(req); len(problems) > 0 {
		return db.Ticket{}, ValidationError{Problems: problems}
	}

	targetStatus := TicketStatusDone
	if req.Decision == ReviewDecisionReject {
		targetStatus = TicketStatusTodo
	}
	return s.setTicketStatus(ctx, setTicketStatusRequest{
		ticketID:        req.TicketID,
		status:          targetStatus,
		allowedStatuses: []string{TicketStatusNeedsReview},
		eventType:       EventTicketReviewed,
		actorType:       req.ActorType,
		actorID:         req.ActorID,
		data: map[string]any{
			"operation": "review",
			"decision":  req.Decision,
			"reason":    req.Reason,
		},
	})
}

type ticketTransitionSpec struct {
	operation       string
	eventType       string
	targetStatus    string
	allowedStatuses []string
}

type setTicketStatusRequest struct {
	ticketID        pgtype.UUID
	status          string
	allowedStatuses []string
	eventType       string
	actorType       string
	actorID         string
	data            map[string]any
}

func (s *TicketService) transitionTicket(ctx context.Context, req TicketTransitionRequest, spec ticketTransitionSpec) (db.Ticket, error) {
	req = trimTicketTransitionRequest(req)
	if req.ActorType == "" {
		req.ActorType = ActorHuman
	}
	if problems := validateTicketTransitionRequest(req); len(problems) > 0 {
		return db.Ticket{}, ValidationError{Problems: problems}
	}
	return s.setTicketStatus(ctx, setTicketStatusRequest{
		ticketID:        req.TicketID,
		status:          spec.targetStatus,
		allowedStatuses: spec.allowedStatuses,
		eventType:       spec.eventType,
		actorType:       req.ActorType,
		actorID:         req.ActorID,
		data: map[string]any{
			"operation": spec.operation,
			"reason":    req.Reason,
		},
	})
}

func (s *TicketService) transitionProposedTicket(ctx context.Context, req ProposedTicketTriageRequest, operation string, eventType string, targetStatus string) (db.Ticket, error) {
	req = trimProposedTicketTriageRequest(req)
	if req.ActorType == "" {
		req.ActorType = ActorHuman
	}
	if problems := validateProposedTicketTriageRequest(req); len(problems) > 0 {
		return db.Ticket{}, ValidationError{Problems: problems}
	}
	if _, err := s.requireProposedTicket(ctx, req.TicketID); err != nil {
		return db.Ticket{}, err
	}
	return s.setTicketStatus(ctx, setTicketStatusRequest{
		ticketID:        req.TicketID,
		status:          targetStatus,
		allowedStatuses: []string{TicketStatusBacklog},
		eventType:       eventType,
		actorType:       req.ActorType,
		actorID:         req.ActorID,
		data: map[string]any{
			"operation": operation,
			"reason":    req.Reason,
		},
	})
}

func (s *TicketService) requireProposedTicket(ctx context.Context, ticketID pgtype.UUID) (db.Ticket, error) {
	ticket, err := s.store.GetTicket(ctx, ticketID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return db.Ticket{}, ErrTicketNotFound
		}
		return db.Ticket{}, fmt.Errorf("get proposed ticket: %w", err)
	}
	if ticket.Status != TicketStatusBacklog || ticket.CreatedBy != ActorAgent {
		return db.Ticket{}, ErrTicketIsNotProposed
	}
	return ticket, nil
}

func (s *TicketService) setTicketStatus(ctx context.Context, req setTicketStatusRequest) (db.Ticket, error) {
	eventData := make(map[string]any, len(req.data)+1)
	for key, value := range req.data {
		if value != "" {
			eventData[key] = value
		}
	}
	eventData["status"] = req.status
	raw, err := json.Marshal(eventData)
	if err != nil {
		return db.Ticket{}, fmt.Errorf("marshal ticket event data: %w", err)
	}

	row, err := s.store.TransitionTicket(ctx, db.TransitionTicketParams{
		ID:              req.ticketID,
		Status:          req.status,
		AllowedStatuses: req.allowedStatuses,
		Type:            req.eventType,
		ActorType:       req.actorType,
		ActorID:         optionalText(req.actorID),
		Data:            raw,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			if _, getErr := s.store.GetTicket(ctx, req.ticketID); errors.Is(getErr, pgx.ErrNoRows) {
				return db.Ticket{}, ErrTicketNotFound
			} else if getErr != nil {
				return db.Ticket{}, fmt.Errorf("get ticket after transition miss: %w", getErr)
			}
			return db.Ticket{}, ErrTicketTransitionNotAllowed
		}
		return db.Ticket{}, fmt.Errorf("transition ticket: %w", err)
	}

	return ticketFromTransitionRow(row), nil
}

func ticketFromTransitionRow(row db.TransitionTicketRow) db.Ticket {
	return db.Ticket{
		ID:                   row.ID,
		WorkspaceID:          row.WorkspaceID,
		ProjectID:            row.ProjectID,
		ParentID:             row.ParentID,
		RootID:               row.RootID,
		SourceAttemptID:      row.SourceAttemptID,
		SourceArtifactID:     row.SourceArtifactID,
		Title:                row.Title,
		Description:          row.Description,
		Type:                 row.Type,
		Status:               row.Status,
		Priority:             row.Priority,
		Tags:                 row.Tags,
		AcceptanceCriteria:   row.AcceptanceCriteria,
		VerificationCommands: row.VerificationCommands,
		ExpectedArtifacts:    row.ExpectedArtifacts,
		RelevantPaths:        row.RelevantPaths,
		RequiredTools:        row.RequiredTools,
		RequiredPermissions:  row.RequiredPermissions,
		Environment:          row.Environment,
		Input:                row.Input,
		InputSchema:          row.InputSchema,
		RequiredCapabilities: row.RequiredCapabilities,
		AllowedHarnesses:     row.AllowedHarnesses,
		RetryPolicy:          row.RetryPolicy,
		CreatedBy:            row.CreatedBy,
		CreatedByID:          row.CreatedByID,
		CreationReason:       row.CreationReason,
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
	}
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
		problems = append(problems, "type must be one of feature, task, bug, documentation, research, analysis, planning, review, integration, investigation, cleanup, follow_up, custom")
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

func validateListProposedTicketsRequest(req ListProposedTicketsRequest) []string {
	var problems []string
	if !req.WorkspaceID.Valid {
		problems = append(problems, "workspace_id is required")
	}
	if !req.ProjectID.Valid {
		problems = append(problems, "project_id is required")
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

func validateUpdateTicketRequest(req UpdateTicketRequest) []string {
	var problems []string
	if !req.TicketID.Valid {
		problems = append(problems, "ticket_id is required")
	}
	if !isAllowedActor(req.ActorType) {
		problems = append(problems, "actor_type must be human, agent, or system")
	}
	if req.Title == nil &&
		req.Description == nil &&
		req.Tags == nil &&
		req.AcceptanceCriteria == nil &&
		req.VerificationCommands == nil &&
		req.RelevantPaths == nil {
		problems = append(problems, "patch must include at least one supported field")
	}
	if req.Title != nil && *req.Title == "" {
		problems = append(problems, "title cannot be empty")
	}
	if req.AcceptanceCriteria != nil && len(*req.AcceptanceCriteria) == 0 {
		problems = append(problems, "acceptance_criteria cannot be empty")
	}
	return problems
}

func validateTicketTransitionRequest(req TicketTransitionRequest) []string {
	var problems []string
	if !req.TicketID.Valid {
		problems = append(problems, "ticket_id is required")
	}
	if !isAllowedActor(req.ActorType) {
		problems = append(problems, "actor_type must be human, agent, or system")
	}
	return problems
}

func validateReviewTicketRequest(req ReviewTicketRequest) []string {
	var problems []string
	if !req.TicketID.Valid {
		problems = append(problems, "ticket_id is required")
	}
	if !isAllowedActor(req.ActorType) {
		problems = append(problems, "actor_type must be human, agent, or system")
	}
	if req.Decision != ReviewDecisionApprove && req.Decision != ReviewDecisionReject {
		problems = append(problems, "decision must be approve or reject")
	}
	return problems
}

func validateProposedTicketTriageRequest(req ProposedTicketTriageRequest) []string {
	var problems []string
	if !req.TicketID.Valid {
		problems = append(problems, "ticket_id is required")
	}
	if !isAllowedActor(req.ActorType) {
		problems = append(problems, "actor_type must be human, agent, or system")
	}
	return problems
}

func validateMergeProposedTicketRequest(req MergeProposedTicketRequest) []string {
	var problems []string
	if !req.TicketID.Valid {
		problems = append(problems, "ticket_id is required")
	}
	if !req.TargetTicketID.Valid {
		problems = append(problems, "target_ticket_id is required")
	}
	if req.TicketID == req.TargetTicketID {
		problems = append(problems, "target_ticket_id must differ from ticket_id")
	}
	if !isAllowedActor(req.ActorType) {
		problems = append(problems, "actor_type must be human, agent, or system")
	}
	return problems
}

func validateRefineProposedTicketRequest(req RefineProposedTicketRequest) []string {
	var problems []string
	if !req.TicketID.Valid {
		problems = append(problems, "ticket_id is required")
	}
	if !isAllowedActor(req.ActorType) {
		problems = append(problems, "actor_type must be human, agent, or system")
	}
	if req.Title == nil &&
		req.Description == nil &&
		req.Tags == nil &&
		req.AcceptanceCriteria == nil &&
		req.VerificationCommands == nil &&
		req.RelevantPaths == nil {
		problems = append(problems, "patch must include at least one supported field")
	}
	if req.Title != nil && *req.Title == "" {
		problems = append(problems, "title cannot be empty")
	}
	if req.AcceptanceCriteria != nil && len(*req.AcceptanceCriteria) == 0 {
		problems = append(problems, "acceptance_criteria cannot be empty")
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
	req.Tags = compactStringsPreserveEmpty(req.Tags)
	req.AcceptanceCriteria = compactStringsPreserveEmpty(req.AcceptanceCriteria)
	req.VerificationCommands = compactStringsPreserveEmpty(req.VerificationCommands)
	req.ExpectedArtifacts = compactStringsPreserveEmpty(req.ExpectedArtifacts)
	req.RelevantPaths = compactStringsPreserveEmpty(req.RelevantPaths)
	req.RequiredTools = compactStringsPreserveEmpty(req.RequiredTools)
	req.RequiredPermissions = compactStringsPreserveEmpty(req.RequiredPermissions)
	req.RequiredCapabilities = compactStringsPreserveEmpty(req.RequiredCapabilities)
	req.AllowedHarnesses = compactStringsPreserveEmpty(req.AllowedHarnesses)
	return req
}

func trimUpdateTicketRequest(req UpdateTicketRequest) UpdateTicketRequest {
	if req.Title != nil {
		value := strings.TrimSpace(*req.Title)
		req.Title = &value
	}
	if req.Description != nil {
		value := strings.TrimSpace(*req.Description)
		req.Description = &value
	}
	req.ActorType = strings.TrimSpace(req.ActorType)
	req.ActorID = strings.TrimSpace(req.ActorID)
	if req.Tags != nil {
		values := compactStringsPreserveEmpty(*req.Tags)
		req.Tags = &values
	}
	if req.AcceptanceCriteria != nil {
		values := compactStringsPreserveEmpty(*req.AcceptanceCriteria)
		req.AcceptanceCriteria = &values
	}
	if req.VerificationCommands != nil {
		values := compactStringsPreserveEmpty(*req.VerificationCommands)
		req.VerificationCommands = &values
	}
	if req.RelevantPaths != nil {
		values := compactStringsPreserveEmpty(*req.RelevantPaths)
		req.RelevantPaths = &values
	}
	return req
}

func trimTicketTransitionRequest(req TicketTransitionRequest) TicketTransitionRequest {
	req.ActorType = strings.TrimSpace(req.ActorType)
	req.ActorID = strings.TrimSpace(req.ActorID)
	req.Reason = strings.TrimSpace(req.Reason)
	return req
}

func trimReviewTicketRequest(req ReviewTicketRequest) ReviewTicketRequest {
	req.Decision = strings.TrimSpace(req.Decision)
	req.ActorType = strings.TrimSpace(req.ActorType)
	req.ActorID = strings.TrimSpace(req.ActorID)
	req.Reason = strings.TrimSpace(req.Reason)
	return req
}

func trimProposedTicketTriageRequest(req ProposedTicketTriageRequest) ProposedTicketTriageRequest {
	req.ActorType = strings.TrimSpace(req.ActorType)
	req.ActorID = strings.TrimSpace(req.ActorID)
	req.Reason = strings.TrimSpace(req.Reason)
	return req
}

func trimMergeProposedTicketRequest(req MergeProposedTicketRequest) MergeProposedTicketRequest {
	req.ActorType = strings.TrimSpace(req.ActorType)
	req.ActorID = strings.TrimSpace(req.ActorID)
	req.Reason = strings.TrimSpace(req.Reason)
	return req
}

func trimRefineProposedTicketRequest(req RefineProposedTicketRequest) RefineProposedTicketRequest {
	if req.Title != nil {
		value := strings.TrimSpace(*req.Title)
		req.Title = &value
	}
	if req.Description != nil {
		value := strings.TrimSpace(*req.Description)
		req.Description = &value
	}
	req.ActorType = strings.TrimSpace(req.ActorType)
	req.ActorID = strings.TrimSpace(req.ActorID)
	req.Reason = strings.TrimSpace(req.Reason)
	if req.Tags != nil {
		values := compactStringsPreserveEmpty(*req.Tags)
		req.Tags = &values
	}
	if req.AcceptanceCriteria != nil {
		values := compactStringsPreserveEmpty(*req.AcceptanceCriteria)
		req.AcceptanceCriteria = &values
	}
	if req.VerificationCommands != nil {
		values := compactStringsPreserveEmpty(*req.VerificationCommands)
		req.VerificationCommands = &values
	}
	if req.RelevantPaths != nil {
		values := compactStringsPreserveEmpty(*req.RelevantPaths)
		req.RelevantPaths = &values
	}
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

func compactStringsPreserveEmpty(values []string) []string {
	out := compactStrings(values)
	if out == nil {
		return []string{}
	}
	return out
}

func optionalText(value string) pgtype.Text {
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

func requiredText(value string) pgtype.Text {
	return pgtype.Text{String: value, Valid: true}
}

func textValue(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func ticketUUIDString(id pgtype.UUID) string {
	value, err := id.Value()
	if err != nil {
		return ""
	}
	text, _ := value.(string)
	return text
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

func decodeJSONArray(value []byte) ([]string, error) {
	if len(value) == 0 {
		return nil, nil
	}
	var out []string
	if err := json.Unmarshal(value, &out); err != nil {
		return nil, err
	}
	return out, nil
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
		TicketTypeTask,
		TicketTypeBug,
		TicketTypeDocumentation,
		TicketTypeResearch,
		TicketTypeAnalysis,
		TicketTypePlanning,
		TicketTypeReview,
		TicketTypeIntegration,
		TicketTypeInvestigation,
		TicketTypeCleanup,
		TicketTypeFollowUp,
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
