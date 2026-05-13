package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

const (
	AttemptStatusRunning   = "running"
	AttemptStatusSucceeded = "succeeded"
	AttemptStatusFailed    = "failed"
	AttemptStatusBlocked   = "blocked"
	AttemptStatusExpired   = "expired"
	AttemptStatusCancelled = "cancelled"
)

var ErrNoClaimableTickets = errors.New("no claimable tickets")
var ErrIdempotencyConflict = errors.New("idempotency key reused with a different request")

type ClaimStore interface {
	ClaimNextTicket(context.Context, db.ClaimNextTicketParams) (db.ClaimNextTicketRow, error)
	GetIdempotencyKey(context.Context, db.GetIdempotencyKeyParams) (db.IdempotencyKey, error)
	GetTicket(context.Context, pgtype.UUID) (db.Ticket, error)
	GetAttempt(context.Context, pgtype.UUID) (db.Attempt, error)
	ListAttemptsByTicket(context.Context, pgtype.UUID) ([]db.Attempt, error)
	ListAttemptCheckpointsByTicket(context.Context, pgtype.UUID) ([]db.AttemptCheckpoint, error)
	ListArtifactsByTicket(context.Context, pgtype.UUID) ([]db.Artifact, error)
}

var _ ClaimStore = (*db.Queries)(nil)

type ClaimService struct {
	store ClaimStore
	now   func() time.Time
}

type ClaimOption func(*ClaimService)

func WithClaimClock(clock func() time.Time) ClaimOption {
	return func(service *ClaimService) {
		service.now = clock
	}
}

func NewClaimService(store ClaimStore, opts ...ClaimOption) *ClaimService {
	service := &ClaimService{
		store: store,
		now:   time.Now,
	}
	for _, opt := range opts {
		opt(service)
	}
	return service
}

type ClaimNextRequest struct {
	WorkspaceID  pgtype.UUID
	ProjectID    pgtype.UUID
	Type         string
	Tags         []string
	Harness      string
	Capabilities []string
	AgentID      string
	Model        string
	Lease        time.Duration

	IdempotencyKey string
	IdempotencyTTL time.Duration
}

type ClaimNextResult struct {
	Ticket  db.Ticket
	Attempt db.Attempt
	Context ClaimContextBundle
}

type ClaimContextBundle struct {
	Ticket               db.Ticket
	Attempt              db.Attempt
	AcceptanceCriteria   []string
	VerificationCommands []string
	Environment          map[string]any
	Input                map[string]any
	RelevantPaths        []string
	RequiredTools        []string
	RequiredPermissions  []string
	ExpectedArtifacts    []string
	PriorAttempts        []db.Attempt
	Checkpoints          []db.AttemptCheckpoint
	Artifacts            []db.Artifact
}

func (s *ClaimService) ClaimNext(ctx context.Context, req ClaimNextRequest) (ClaimNextResult, error) {
	req = trimClaimNextRequest(req)
	if problems := validateClaimNextRequest(req); len(problems) > 0 {
		return ClaimNextResult{}, ValidationError{Problems: problems}
	}

	now := s.now().UTC()
	requestHash, err := claimRequestHash(req)
	if err != nil {
		return ClaimNextResult{}, fmt.Errorf("hash claim request: %w", err)
	}
	if req.IdempotencyKey != "" {
		replayed, ok, err := s.replayClaim(ctx, req, requestHash)
		if err != nil {
			return ClaimNextResult{}, err
		}
		if ok {
			return replayed, nil
		}
	}

	idempotencyTTL := req.IdempotencyTTL
	if idempotencyTTL == 0 {
		idempotencyTTL = 24 * time.Hour
	}
	row, err := s.store.ClaimNextTicket(ctx, db.ClaimNextTicketParams{
		WorkspaceID:          req.WorkspaceID,
		ProjectID:            req.ProjectID,
		TicketType:           optionalText(req.Type),
		Tags:                 req.Tags,
		Harness:              req.Harness,
		Capabilities:         req.Capabilities,
		AgentID:              req.AgentID,
		Model:                req.Model,
		LeaseExpiresAt:       timestamptz(now.Add(req.Lease)),
		LastHeartbeatAt:      timestamptz(now),
		IdempotencyKey:       optionalText(req.IdempotencyKey),
		RequestHash:          optionalText(requestHash),
		IdempotencyExpiresAt: timestamptz(now.Add(idempotencyTTL)),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ClaimNextResult{}, ErrNoClaimableTickets
		}
		return ClaimNextResult{}, fmt.Errorf("claim next ticket: %w", err)
	}

	return s.hydrateClaim(ctx, row.TicketID, row.AttemptID)
}

func (s *ClaimService) replayClaim(ctx context.Context, req ClaimNextRequest, requestHash string) (ClaimNextResult, bool, error) {
	record, err := s.store.GetIdempotencyKey(ctx, db.GetIdempotencyKeyParams{
		WorkspaceID: req.WorkspaceID,
		ActorID:     req.AgentID,
		Route:       "claim-next",
		Key:         req.IdempotencyKey,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ClaimNextResult{}, false, nil
		}
		return ClaimNextResult{}, false, fmt.Errorf("get idempotency key: %w", err)
	}
	if record.RequestHash != requestHash {
		return ClaimNextResult{}, false, ErrIdempotencyConflict
	}

	response, err := decodeClaimIdempotencyResponse(record.ResponseBody)
	if err != nil {
		return ClaimNextResult{}, false, fmt.Errorf("decode claim idempotency response: %w", err)
	}
	result, err := s.hydrateClaim(ctx, response.TicketID, response.AttemptID)
	if err != nil {
		return ClaimNextResult{}, false, err
	}
	return result, true, nil
}

func (s *ClaimService) hydrateClaim(ctx context.Context, ticketID, attemptID pgtype.UUID) (ClaimNextResult, error) {
	ticket, err := s.store.GetTicket(ctx, ticketID)
	if err != nil {
		return ClaimNextResult{}, fmt.Errorf("get claimed ticket: %w", err)
	}
	attempt, err := s.store.GetAttempt(ctx, attemptID)
	if err != nil {
		return ClaimNextResult{}, fmt.Errorf("get claimed attempt: %w", err)
	}
	contextBundle, err := s.contextBundle(ctx, ticket, attempt)
	if err != nil {
		return ClaimNextResult{}, err
	}

	return ClaimNextResult{
		Ticket:  ticket,
		Attempt: attempt,
		Context: contextBundle,
	}, nil
}

type claimIdempotencyResponse struct {
	TicketID  pgtype.UUID
	AttemptID pgtype.UUID
}

func decodeClaimIdempotencyResponse(raw []byte) (claimIdempotencyResponse, error) {
	var payload struct {
		TicketID  string `json:"ticket_id"`
		AttemptID string `json:"attempt_id"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return claimIdempotencyResponse{}, err
	}
	ticketID, err := parseUUID(payload.TicketID)
	if err != nil {
		return claimIdempotencyResponse{}, fmt.Errorf("ticket_id: %w", err)
	}
	attemptID, err := parseUUID(payload.AttemptID)
	if err != nil {
		return claimIdempotencyResponse{}, fmt.Errorf("attempt_id: %w", err)
	}
	return claimIdempotencyResponse{TicketID: ticketID, AttemptID: attemptID}, nil
}

func claimRequestHash(req ClaimNextRequest) (string, error) {
	payload := struct {
		WorkspaceID  pgtype.UUID `json:"workspace_id"`
		ProjectID    pgtype.UUID `json:"project_id"`
		Type         string      `json:"type"`
		Tags         []string    `json:"tags"`
		Harness      string      `json:"harness"`
		Capabilities []string    `json:"capabilities"`
		AgentID      string      `json:"agent_id"`
		Model        string      `json:"model"`
		LeaseSeconds int64       `json:"lease_seconds"`
	}{
		WorkspaceID:  req.WorkspaceID,
		ProjectID:    req.ProjectID,
		Type:         req.Type,
		Tags:         req.Tags,
		Harness:      req.Harness,
		Capabilities: req.Capabilities,
		AgentID:      req.AgentID,
		Model:        req.Model,
		LeaseSeconds: int64(req.Lease.Seconds()),
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func parseUUID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if value == "" {
		return id, errors.New("uuid is required")
	}
	if err := id.Scan(value); err != nil {
		return pgtype.UUID{}, err
	}
	return id, nil
}

func (s *ClaimService) contextBundle(ctx context.Context, ticket db.Ticket, attempt db.Attempt) (ClaimContextBundle, error) {
	attempts, err := s.store.ListAttemptsByTicket(ctx, ticket.ID)
	if err != nil {
		return ClaimContextBundle{}, fmt.Errorf("list ticket attempts: %w", err)
	}
	checkpoints, err := s.store.ListAttemptCheckpointsByTicket(ctx, ticket.ID)
	if err != nil {
		return ClaimContextBundle{}, fmt.Errorf("list ticket checkpoints: %w", err)
	}
	artifacts, err := s.store.ListArtifactsByTicket(ctx, ticket.ID)
	if err != nil {
		return ClaimContextBundle{}, fmt.Errorf("list ticket artifacts: %w", err)
	}

	verificationCommands, err := decodeStringArray(ticket.VerificationCommands)
	if err != nil {
		return ClaimContextBundle{}, fmt.Errorf("decode verification commands: %w", err)
	}
	environment, err := decodeObject(ticket.Environment)
	if err != nil {
		return ClaimContextBundle{}, fmt.Errorf("decode environment: %w", err)
	}
	input, err := decodeObject(ticket.Input)
	if err != nil {
		return ClaimContextBundle{}, fmt.Errorf("decode input: %w", err)
	}

	return ClaimContextBundle{
		Ticket:               ticket,
		Attempt:              attempt,
		AcceptanceCriteria:   ticket.AcceptanceCriteria,
		VerificationCommands: verificationCommands,
		Environment:          environment,
		Input:                input,
		RelevantPaths:        ticket.RelevantPaths,
		RequiredTools:        ticket.RequiredTools,
		RequiredPermissions:  ticket.RequiredPermissions,
		ExpectedArtifacts:    ticket.ExpectedArtifacts,
		PriorAttempts:        priorAttempts(attempts, attempt.ID),
		Checkpoints:          checkpoints,
		Artifacts:            artifacts,
	}, nil
}

func validateClaimNextRequest(req ClaimNextRequest) []string {
	var problems []string
	if !req.WorkspaceID.Valid {
		problems = append(problems, "workspace_id is required")
	}
	if !req.ProjectID.Valid {
		problems = append(problems, "project_id is required")
	}
	if req.AgentID == "" {
		problems = append(problems, "agent_id is required")
	}
	if req.Harness == "" {
		problems = append(problems, "harness is required")
	}
	if req.Lease <= 0 {
		problems = append(problems, "lease must be positive")
	}
	if req.IdempotencyTTL < 0 {
		problems = append(problems, "idempotency_ttl must be non-negative")
	}
	if req.Type != "" && !isAllowedTicketType(req.Type) {
		problems = append(problems, "type filter is not valid")
	}
	return problems
}

func trimClaimNextRequest(req ClaimNextRequest) ClaimNextRequest {
	req.Type = strings.TrimSpace(req.Type)
	req.Harness = strings.TrimSpace(req.Harness)
	req.AgentID = strings.TrimSpace(req.AgentID)
	req.Model = strings.TrimSpace(req.Model)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	req.Tags = compactStrings(req.Tags)
	req.Capabilities = compactStrings(req.Capabilities)
	return req
}

func priorAttempts(attempts []db.Attempt, currentAttemptID pgtype.UUID) []db.Attempt {
	out := make([]db.Attempt, 0, len(attempts))
	for _, attempt := range attempts {
		if attempt.ID == currentAttemptID {
			continue
		}
		out = append(out, attempt)
	}
	return out
}

func timestamptz(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

func decodeStringArray(raw []byte) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	return values, nil
}

func decodeObject(raw []byte) (map[string]any, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}, nil
	}
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	if value == nil {
		return map[string]any{}, nil
	}
	return value, nil
}
