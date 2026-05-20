package services

import (
	"context"
	"errors"
	"testing"

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

type fakeSearchStore struct {
	params db.SearchTicketsParams
	rows   []db.SearchTicketsRow
	err    error
}

func (s *fakeSearchStore) SearchTickets(_ context.Context, params db.SearchTicketsParams) ([]db.SearchTicketsRow, error) {
	s.params = params
	if s.err != nil {
		return nil, s.err
	}
	return s.rows, nil
}
