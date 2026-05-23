package services

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

const defaultEventFeedLimit int32 = 100
const maxEventFeedLimit int32 = 500

type EventStore interface {
	ListRecentTicketEvents(context.Context, db.ListRecentTicketEventsParams) ([]db.TicketEvent, error)
	ListTicketEventsAfterCursor(context.Context, db.ListTicketEventsAfterCursorParams) ([]db.TicketEvent, error)
}

var _ EventStore = (*db.Queries)(nil)

type EventService struct {
	store EventStore
}

func NewEventService(store EventStore) *EventService {
	return &EventService{store: store}
}

type ListEventsRequest struct {
	WorkspaceID pgtype.UUID
	ProjectID   pgtype.UUID
	TicketID    pgtype.UUID
	AttemptID   pgtype.UUID
	Cursor      string
	Limit       int32
}

type ListEventsResult struct {
	Events     []db.TicketEvent `json:"events"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

type eventCursor struct {
	CreatedAt pgtype.Timestamptz
	ID        pgtype.UUID
}

func (s *EventService) ListEvents(ctx context.Context, req ListEventsRequest) (ListEventsResult, error) {
	req.Cursor = strings.TrimSpace(req.Cursor)
	if problems := validateListEventsRequest(req); len(problems) > 0 {
		return ListEventsResult{}, ValidationError{Problems: problems}
	}

	limit := req.Limit
	if limit == 0 {
		limit = defaultEventFeedLimit
	}
	if limit > maxEventFeedLimit {
		limit = maxEventFeedLimit
	}

	var (
		events []db.TicketEvent
		err    error
	)
	if req.Cursor == "" {
		events, err = s.store.ListRecentTicketEvents(ctx, db.ListRecentTicketEventsParams{
			WorkspaceID: req.WorkspaceID,
			ProjectID:   req.ProjectID,
			TicketID:    req.TicketID,
			AttemptID:   req.AttemptID,
			LimitCount:  limit,
		})
	} else {
		cursor, parseErr := parseEventCursor(req.Cursor)
		if parseErr != nil {
			return ListEventsResult{}, ValidationError{Problems: []string{"cursor is invalid"}}
		}
		events, err = s.store.ListTicketEventsAfterCursor(ctx, db.ListTicketEventsAfterCursorParams{
			WorkspaceID:    req.WorkspaceID,
			ProjectID:      req.ProjectID,
			TicketID:       req.TicketID,
			AttemptID:      req.AttemptID,
			AfterCreatedAt: cursor.CreatedAt,
			AfterID:        cursor.ID,
			LimitCount:     limit,
		})
	}
	if err != nil {
		return ListEventsResult{}, err
	}

	nextCursor := req.Cursor
	if len(events) > 0 {
		nextCursor = formatEventCursor(events[len(events)-1])
	}
	return ListEventsResult{Events: events, NextCursor: nextCursor}, nil
}

func validateListEventsRequest(req ListEventsRequest) []string {
	var problems []string
	if req.Limit < 0 {
		problems = append(problems, "limit must be non-negative")
	}
	return problems
}

func formatEventCursor(event db.TicketEvent) string {
	if !event.CreatedAt.Valid || !event.ID.Valid {
		return ""
	}
	value := event.CreatedAt.Time.UTC().Format(time.RFC3339Nano) + "|" + eventUUIDString(event.ID)
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func parseEventCursor(value string) (eventCursor, error) {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return eventCursor{}, err
	}
	parts := strings.SplitN(string(decoded), "|", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return eventCursor{}, fmt.Errorf("malformed cursor")
	}
	createdAt, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return eventCursor{}, err
	}
	var id pgtype.UUID
	if err := id.Scan(parts[1]); err != nil {
		return eventCursor{}, err
	}
	return eventCursor{
		CreatedAt: pgtype.Timestamptz{Time: createdAt, Valid: true},
		ID:        id,
	}, nil
}

func eventUUIDString(value pgtype.UUID) string {
	if !value.Valid {
		return ""
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", value.Bytes[0:4], value.Bytes[4:6], value.Bytes[6:8], value.Bytes[8:10], value.Bytes[10:16])
}
