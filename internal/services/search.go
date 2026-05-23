package services

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

const defaultSearchLimit int32 = 25
const maxSearchLimit int32 = 100

type SearchStore interface {
	GetTicket(context.Context, pgtype.UUID) (db.Ticket, error)
	SearchTickets(context.Context, db.SearchTicketsParams) ([]db.SearchTicketsRow, error)
	SearchRelatedTickets(context.Context, db.SearchRelatedTicketsParams) ([]db.SearchRelatedTicketsRow, error)
}

var _ SearchStore = (*db.Queries)(nil)

type SearchService struct {
	store SearchStore
}

func NewSearchService(store SearchStore) *SearchService {
	return &SearchService{store: store}
}

type SearchTicketsRequest struct {
	WorkspaceID pgtype.UUID
	ProjectID   pgtype.UUID
	Query       string
	Offset      int32
	Limit       int32
}

type SearchResult struct {
	Ticket       db.Ticket
	MatchSources []string
	Snippet      string
	Rank         float32
}

type RelatedWorkRequest struct {
	TicketID pgtype.UUID
	Offset   int32
	Limit    int32
}

type RelatedWorkResult struct {
	Ticket       db.Ticket
	MatchSources []string
	AttemptIDs   []pgtype.UUID
	Snippet      string
	Rank         float32
}

func (s *SearchService) SearchTickets(ctx context.Context, req SearchTicketsRequest) ([]SearchResult, error) {
	req.Query = strings.TrimSpace(req.Query)
	if problems := validateSearchTicketsRequest(req); len(problems) > 0 {
		return nil, ValidationError{Problems: problems}
	}

	limit := req.Limit
	if limit == 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	rows, err := s.store.SearchTickets(ctx, db.SearchTicketsParams{
		WorkspaceID: req.WorkspaceID,
		ProjectID:   req.ProjectID,
		Query:       req.Query,
		Offset:      req.Offset,
		Limit:       limit,
	})
	if err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, SearchResult{
			Ticket:       searchRowTicket(row),
			MatchSources: row.MatchSources,
			Snippet:      row.Snippet,
			Rank:         row.Rank,
		})
	}
	return results, nil
}

func (s *SearchService) RelatedWork(ctx context.Context, req RelatedWorkRequest) ([]RelatedWorkResult, error) {
	if problems := validateRelatedWorkRequest(req); len(problems) > 0 {
		return nil, ValidationError{Problems: problems}
	}
	if _, err := s.store.GetTicket(ctx, req.TicketID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTicketNotFound
		}
		return nil, err
	}

	limit := req.Limit
	if limit == 0 {
		limit = defaultSearchLimit
	}
	if limit > maxSearchLimit {
		limit = maxSearchLimit
	}

	rows, err := s.store.SearchRelatedTickets(ctx, db.SearchRelatedTicketsParams{
		TicketID: req.TicketID,
		Offset:   req.Offset,
		Limit:    limit,
	})
	if err != nil {
		return nil, err
	}

	results := make([]RelatedWorkResult, 0, len(rows))
	for _, row := range rows {
		results = append(results, RelatedWorkResult{
			Ticket:       relatedRowTicket(row),
			MatchSources: row.MatchSources,
			AttemptIDs:   row.AttemptIds,
			Snippet:      row.Snippet,
			Rank:         row.Rank,
		})
	}
	return results, nil
}

func validateSearchTicketsRequest(req SearchTicketsRequest) []string {
	var problems []string
	if !req.WorkspaceID.Valid {
		problems = append(problems, "workspace_id is required")
	}
	if !req.ProjectID.Valid {
		problems = append(problems, "project_id is required")
	}
	if req.Query == "" {
		problems = append(problems, "query is required")
	}
	if req.Offset < 0 {
		problems = append(problems, "offset must be non-negative")
	}
	if req.Limit < 0 {
		problems = append(problems, "limit must be non-negative")
	}
	return problems
}

func validateRelatedWorkRequest(req RelatedWorkRequest) []string {
	var problems []string
	if !req.TicketID.Valid {
		problems = append(problems, "ticket_id is required")
	}
	if req.Offset < 0 {
		problems = append(problems, "offset must be non-negative")
	}
	if req.Limit < 0 {
		problems = append(problems, "limit must be non-negative")
	}
	return problems
}

func searchRowTicket(row db.SearchTicketsRow) db.Ticket {
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

func relatedRowTicket(row db.SearchRelatedTicketsRow) db.Ticket {
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
