package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

func TestListEventsReturnsRecentWindowWithNextCursor(t *testing.T) {
	lastCreatedAt := time.Date(2026, 5, 23, 8, 30, 0, 0, time.UTC)
	store := &fakeEventStore{
		recentEvents: []db.TicketEvent{
			{ID: testUUID(10), CreatedAt: timestamptzForTest(lastCreatedAt.Add(-time.Second))},
			{ID: testUUID(11), CreatedAt: timestamptzForTest(lastCreatedAt)},
		},
	}
	service := NewEventService(store)

	result, err := service.ListEvents(context.Background(), ListEventsRequest{
		WorkspaceID: testUUID(1),
		ProjectID:   testUUID(2),
		TicketID:    testUUID(3),
		AttemptID:   testUUID(4),
	})
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	if len(result.Events) != 2 {
		t.Fatalf("expected two events, got %d", len(result.Events))
	}
	if store.recentParams.WorkspaceID != testUUID(1) || store.recentParams.ProjectID != testUUID(2) {
		t.Fatalf("scope filters did not reach store: %#v", store.recentParams)
	}
	if store.recentParams.TicketID != testUUID(3) || store.recentParams.AttemptID != testUUID(4) {
		t.Fatalf("event filters did not reach store: %#v", store.recentParams)
	}
	if store.recentParams.LimitCount != defaultEventFeedLimit {
		t.Fatalf("expected default limit %d, got %d", defaultEventFeedLimit, store.recentParams.LimitCount)
	}
	cursor, err := parseEventCursor(result.NextCursor)
	if err != nil {
		t.Fatalf("parse next cursor: %v", err)
	}
	if cursor.ID != testUUID(11) || !cursor.CreatedAt.Time.Equal(lastCreatedAt) {
		t.Fatalf("next cursor points at wrong event: %#v", cursor)
	}
}

func TestListEventsUsesCursorForFollowUpRequests(t *testing.T) {
	createdAt := time.Date(2026, 5, 23, 9, 0, 0, 0, time.UTC)
	cursor := formatEventCursor(db.TicketEvent{ID: testUUID(20), CreatedAt: timestamptzForTest(createdAt)})
	store := &fakeEventStore{}
	service := NewEventService(store)

	result, err := service.ListEvents(context.Background(), ListEventsRequest{
		WorkspaceID: testUUID(1),
		Cursor:      cursor,
		Limit:       maxEventFeedLimit + 100,
	})
	if err != nil {
		t.Fatalf("list events after cursor: %v", err)
	}
	if result.NextCursor != cursor {
		t.Fatalf("expected empty follow-up to keep cursor %q, got %q", cursor, result.NextCursor)
	}
	if store.afterParams.WorkspaceID != testUUID(1) {
		t.Fatalf("workspace filter did not reach follow-up query: %#v", store.afterParams)
	}
	if !store.afterParams.AfterCreatedAt.Time.Equal(createdAt) {
		t.Fatalf("cursor did not reach follow-up query: %#v", store.afterParams)
	}
	if store.afterParams.LimitCount != maxEventFeedLimit {
		t.Fatalf("expected capped limit %d, got %d", maxEventFeedLimit, store.afterParams.LimitCount)
	}
}

func TestListEventsValidatesCursorAndLimit(t *testing.T) {
	service := NewEventService(&fakeEventStore{})

	_, err := service.ListEvents(context.Background(), ListEventsRequest{Limit: -1})
	var validationErr ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected validation error for limit, got %v", err)
	}
	if validationErr.Error() != "validation failed: limit must be non-negative" {
		t.Fatalf("unexpected limit error: %v", validationErr)
	}

	_, err = service.ListEvents(context.Background(), ListEventsRequest{Cursor: "not-base64"})
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected validation error for cursor, got %v", err)
	}
	if validationErr.Error() != "validation failed: cursor is invalid" {
		t.Fatalf("unexpected cursor error: %v", validationErr)
	}
}

type fakeEventStore struct {
	recentParams db.ListRecentTicketEventsParams
	afterParams  db.ListTicketEventsAfterCursorParams
	recentEvents []db.TicketEvent
	afterEvents  []db.TicketEvent
	err          error
}

func (s *fakeEventStore) ListRecentTicketEvents(_ context.Context, params db.ListRecentTicketEventsParams) ([]db.TicketEvent, error) {
	s.recentParams = params
	if s.err != nil {
		return nil, s.err
	}
	return s.recentEvents, nil
}

func (s *fakeEventStore) ListTicketEventsAfterCursor(_ context.Context, params db.ListTicketEventsAfterCursorParams) ([]db.TicketEvent, error) {
	s.afterParams = params
	if s.err != nil {
		return nil, s.err
	}
	return s.afterEvents, nil
}

func timestamptzForTest(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}
