package services

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

func TestSearchTicketsDefaultsLimitAndMapsMatches(t *testing.T) {
	store := &fakeSearchStore{
		rows: []db.SearchTicketsRow{
			{
				ID:           testUUID(3),
				WorkspaceID:  testUUID(1),
				ProjectID:    testUUID(2),
				Title:        "Fix flaky deploy",
				Description:  "Deployment proof is missing from the run.",
				Type:         TicketTypeBug,
				Status:       TicketStatusTodo,
				Priority:     1,
				CreatedBy:    ActorAgent,
				MatchSources: []string{"artifact", "attempt"},
				Snippet:      "deploy log captured failed assertion",
				Rank:         0.42,
			},
		},
	}
	service := NewSearchService(store)

	results, err := service.SearchTickets(context.Background(), SearchTicketsRequest{
		WorkspaceID: testUUID(1),
		ProjectID:   testUUID(2),
		Query:       "deploy proof",
	})
	if err != nil {
		t.Fatalf("search tickets: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one search result, got %d", len(results))
	}
	if store.params.Limit != 25 {
		t.Fatalf("expected default limit 25, got %d", store.params.Limit)
	}
	if store.params.Query != "deploy proof" {
		t.Fatalf("expected trimmed query to reach store, got %q", store.params.Query)
	}
	if results[0].Ticket.Title != "Fix flaky deploy" {
		t.Fatalf("unexpected mapped ticket: %#v", results[0].Ticket)
	}
	if got := results[0].MatchSources; len(got) != 2 || got[0] != "artifact" || got[1] != "attempt" {
		t.Fatalf("unexpected match sources: %#v", got)
	}
	if results[0].Snippet != "deploy log captured failed assertion" || results[0].Rank != 0.42 {
		t.Fatalf("unexpected search metadata: %#v", results[0])
	}
}

func TestSearchTicketsValidatesScopeAndQuery(t *testing.T) {
	service := NewSearchService(&fakeSearchStore{})

	_, err := service.SearchTickets(context.Background(), SearchTicketsRequest{
		WorkspaceID: testUUID(1),
		ProjectID:   testUUID(2),
		Query:       "   ",
	})
	var validationErr ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if validationErr.Error() != "validation failed: query is required" {
		t.Fatalf("unexpected validation error: %v", validationErr)
	}
}

func TestRecommendTicketsDefaultsLimitAndMapsReasons(t *testing.T) {
	store := &fakeSearchStore{
		recommendationRows: []db.RecommendTicketsRow{
			{
				ID:                    testUUID(4),
				WorkspaceID:           testUUID(1),
				ProjectID:             testUUID(2),
				Title:                 "Harden proof upload",
				Description:           "Add retry-safe artifact proof handling.",
				Type:                  TicketTypeBug,
				Status:                TicketStatusTodo,
				Priority:              1,
				CreatedBy:             ActorAgent,
				RecommendationScore:   119,
				RecommendationReasons: []string{"priority:1", "has_verification_commands", "agent_created"},
			},
		},
	}
	service := NewSearchService(store)

	results, err := service.RecommendTickets(context.Background(), RecommendationRequest{
		WorkspaceID:  testUUID(1),
		ProjectID:    testUUID(2),
		Type:         TicketTypeBug,
		Tags:         []string{" phase-5 ", ""},
		Harness:      "codex",
		Capabilities: []string{" codegen ", "testing", ""},
	})
	if err != nil {
		t.Fatalf("recommend tickets: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one recommendation, got %d", len(results))
	}
	if store.recommendationParams.Limit != 25 {
		t.Fatalf("expected default limit 25, got %d", store.recommendationParams.Limit)
	}
	if store.recommendationParams.TicketType.String != TicketTypeBug || !store.recommendationParams.TicketType.Valid {
		t.Fatalf("expected type filter, got %#v", store.recommendationParams.TicketType)
	}
	if store.recommendationParams.Harness != "codex" {
		t.Fatalf("expected harness to reach store, got %#v", store.recommendationParams)
	}
	if got, want := store.recommendationParams.Tags, []string{"phase-5"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("expected compacted tags %#v, got %#v", want, got)
	}
	if got, want := store.recommendationParams.Capabilities, []string{"codegen", "testing"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("expected compacted capabilities %#v, got %#v", want, got)
	}
	if results[0].Ticket.ID != testUUID(4) {
		t.Fatalf("unexpected mapped ticket: %#v", results[0].Ticket)
	}
	if results[0].Score != 119 {
		t.Fatalf("unexpected score: %#v", results[0])
	}
	if got := results[0].Reasons; len(got) != 3 || got[2] != "agent_created" {
		t.Fatalf("unexpected reasons: %#v", got)
	}
}

func TestRecommendTicketsValidatesScopeAndHarness(t *testing.T) {
	service := NewSearchService(&fakeSearchStore{})

	_, err := service.RecommendTickets(context.Background(), RecommendationRequest{
		WorkspaceID: testUUID(1),
		ProjectID:   testUUID(2),
	})
	var validationErr ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if validationErr.Error() != "validation failed: harness is required" {
		t.Fatalf("unexpected validation error: %v", validationErr)
	}
}

func TestRelatedWorkDefaultsLimitAndMapsMatches(t *testing.T) {
	store := &fakeSearchStore{
		sourceTicket: db.Ticket{ID: testUUID(3)},
		relatedRows: []db.SearchRelatedTicketsRow{
			{
				ID:           testUUID(4),
				WorkspaceID:  testUUID(1),
				ProjectID:    testUUID(2),
				Title:        "Recover deploy proof upload",
				Description:  "Prior attempt fixed missing proof artifacts.",
				Type:         TicketTypeBug,
				Status:       TicketStatusDone,
				Priority:     1,
				CreatedBy:    ActorAgent,
				MatchSources: []string{"attempt", "ticket"},
				AttemptIds:   []pgtype.UUID{testUUID(5)},
				Snippet:      "proof artifact upload retry",
				Rank:         0.77,
			},
		},
	}
	service := NewSearchService(store)

	results, err := service.RelatedWork(context.Background(), RelatedWorkRequest{
		TicketID: testUUID(3),
	})
	if err != nil {
		t.Fatalf("related work: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected one related work result, got %d", len(results))
	}
	if store.relatedParams.TicketID != testUUID(3) {
		t.Fatalf("expected source ticket id to reach store, got %#v", store.relatedParams.TicketID)
	}
	if store.relatedParams.Limit != 25 {
		t.Fatalf("expected default limit 25, got %d", store.relatedParams.Limit)
	}
	if results[0].Ticket.ID != testUUID(4) {
		t.Fatalf("unexpected mapped ticket: %#v", results[0].Ticket)
	}
	if got := results[0].MatchSources; len(got) != 2 || got[0] != "attempt" || got[1] != "ticket" {
		t.Fatalf("unexpected match sources: %#v", got)
	}
	if got := results[0].AttemptIDs; len(got) != 1 || got[0] != testUUID(5) {
		t.Fatalf("unexpected attempt ids: %#v", got)
	}
	if results[0].Snippet != "proof artifact upload retry" || results[0].Rank != 0.77 {
		t.Fatalf("unexpected related metadata: %#v", results[0])
	}
}

func TestRelatedWorkReturnsNotFoundForUnknownSourceTicket(t *testing.T) {
	service := NewSearchService(&fakeSearchStore{getTicketErr: pgx.ErrNoRows})

	_, err := service.RelatedWork(context.Background(), RelatedWorkRequest{
		TicketID: testUUID(3),
	})
	if !errors.Is(err, ErrTicketNotFound) {
		t.Fatalf("expected ticket not found, got %v", err)
	}
}

func TestRelatedWorkValidatesTicketID(t *testing.T) {
	service := NewSearchService(&fakeSearchStore{})

	_, err := service.RelatedWork(context.Background(), RelatedWorkRequest{})
	var validationErr ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if validationErr.Error() != "validation failed: ticket_id is required" {
		t.Fatalf("unexpected validation error: %v", validationErr)
	}
}

type fakeSearchStore struct {
	params               db.SearchTicketsParams
	rows                 []db.SearchTicketsRow
	sourceTicket         db.Ticket
	getTicketErr         error
	recommendationParams db.RecommendTicketsParams
	recommendationRows   []db.RecommendTicketsRow
	relatedParams        db.SearchRelatedTicketsParams
	relatedRows          []db.SearchRelatedTicketsRow
	err                  error
}

func (s *fakeSearchStore) GetTicket(_ context.Context, id pgtype.UUID) (db.Ticket, error) {
	if s.getTicketErr != nil {
		return db.Ticket{}, s.getTicketErr
	}
	if !s.sourceTicket.ID.Valid {
		return db.Ticket{}, pgx.ErrNoRows
	}
	if s.sourceTicket.ID != id {
		return db.Ticket{}, pgx.ErrNoRows
	}
	return s.sourceTicket, nil
}

func (s *fakeSearchStore) SearchTickets(_ context.Context, params db.SearchTicketsParams) ([]db.SearchTicketsRow, error) {
	s.params = params
	if s.err != nil {
		return nil, s.err
	}
	return s.rows, nil
}

func (s *fakeSearchStore) RecommendTickets(_ context.Context, params db.RecommendTicketsParams) ([]db.RecommendTicketsRow, error) {
	s.recommendationParams = params
	if s.err != nil {
		return nil, s.err
	}
	return s.recommendationRows, nil
}

func (s *fakeSearchStore) SearchRelatedTickets(_ context.Context, params db.SearchRelatedTicketsParams) ([]db.SearchRelatedTicketsRow, error) {
	s.relatedParams = params
	if s.err != nil {
		return nil, s.err
	}
	return s.relatedRows, nil
}
