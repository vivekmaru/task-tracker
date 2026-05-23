package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/services"
)

func TestWebhookWorkerPostsPayloadAndMarksSuccess(t *testing.T) {
	now := time.Date(2026, 5, 22, 9, 0, 0, 0, time.UTC)
	eventID := testUUID(51)
	deliveryID := testUUID(52)
	attemptID := testUUID(53)
	store := &fakeWebhookStore{
		claimed: []db.ClaimPendingWebhookDeliveriesRow{{
			ID:           deliveryID,
			EventID:      eventID,
			WorkspaceID:  testUUID(1),
			ProjectID:    testUUID(2),
			TicketID:     testUUID(3),
			AttemptID:    attemptID,
			EndpointUrl:  "https://example.test/hooks/forge",
			Secret:       pgtype.Text{String: "secret", Valid: true},
			Payload:      []byte(`{"event_id":"00000000-0000-0000-0000-000000000033","event_type":"completed","actor_type":"agent","actor_id":"codex-worker","data":{"output_schema":"summary.v1"},"created_at":"2026-05-22T08:59:30Z"}`),
			AttemptCount: 0,
			MaxAttempts:  3,
		}},
		attempts: map[pgtype.UUID]db.Attempt{
			attemptID: {
				ID:              attemptID,
				WorkspaceID:     testUUID(1),
				ProjectID:       testUUID(2),
				TicketID:        testUUID(3),
				AgentID:         "codex-worker",
				Harness:         "codex",
				Model:           "gpt-5",
				Status:          services.AttemptStatusSucceeded,
				ProgressPercent: 100,
				StartedAt:       pgtype.Timestamptz{Time: now.Add(-2 * time.Minute), Valid: true},
				CompletedAt:     pgtype.Timestamptz{Time: now.Add(-30 * time.Second), Valid: true},
			},
		},
		metrics: map[pgtype.UUID]db.AttemptMetric{
			attemptID: {
				AttemptID:       attemptID,
				TokensIn:        100,
				TokensOut:       25,
				CostUsd:         numericForWebhookTest(t, 0.42),
				DurationSeconds: numericForWebhookTest(t, 90.5),
				RetryCount:      1,
			},
		},
	}
	client := &fakeHTTPClient{response: &http.Response{
		StatusCode: http.StatusAccepted,
		Body:       io.NopCloser(strings.NewReader("accepted")),
	}}
	worker := NewWebhookWorker(store, WithWebhookClock(func() time.Time { return now }), WithWebhookHTTPClient(client), WithWebhookBatchLimit(10))

	result, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run webhooks: %v", err)
	}

	if result.Claimed != 1 || result.Succeeded != 1 || result.Retried != 0 || result.Failed != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	req := client.requests[0]
	if req.Method != http.MethodPost || req.URL.String() != "https://example.test/hooks/forge" {
		t.Fatalf("unexpected request target: %s %s", req.Method, req.URL.String())
	}
	if got := req.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected json content type, got %q", got)
	}
	if got := req.Header.Get("X-Forge-Event-ID"); got != "00000000-0000-0000-0000-000000000033" {
		t.Fatalf("unexpected event header: %q", got)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	if got := req.Header.Get("X-Forge-Signature-SHA256"); got != webhookSignature("secret", body) {
		t.Fatalf("unexpected signature: %q", got)
	}
	if got := req.Header.Get("X-Forge-Payload-Schema"); got != services.ObservabilitySchemaVersion {
		t.Fatalf("unexpected payload schema header: %q", got)
	}
	var payload services.ObservabilityPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal observability payload: %v\n%s", err, string(body))
	}
	if payload.SchemaVersion != "forge.observability.v1" || payload.Event.Type != services.EventAttemptCompleted {
		t.Fatalf("expected observability event envelope, got %#v", payload)
	}
	if payload.Attempt == nil || payload.Attempt.AgentID != "codex-worker" || payload.Attempt.Status != services.AttemptStatusSucceeded {
		t.Fatalf("expected attempt enrichment, got %#v", payload.Attempt)
	}
	if payload.Metrics == nil || payload.Metrics.TokensIn != 100 || payload.Metrics.TotalTokens != 125 || payload.Metrics.CostUSD != 0.42 {
		t.Fatalf("expected metrics enrichment, got %#v", payload.Metrics)
	}
	if len(store.succeeded) != 1 {
		t.Fatalf("expected success update, got %d", len(store.succeeded))
	}
	success := store.succeeded[0]
	if success.AttemptCount != 1 || !success.ResponseStatus.Valid || success.ResponseStatus.Int32 != http.StatusAccepted {
		t.Fatalf("unexpected success params: %#v", success)
	}
}

func TestWebhookWorkerClaimsDefaultBatchWithLongEnoughLock(t *testing.T) {
	now := time.Date(2026, 5, 22, 9, 0, 0, 0, time.UTC)
	store := &fakeWebhookStore{}
	worker := NewWebhookWorker(store, WithWebhookClock(func() time.Time { return now }))

	result, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run webhooks: %v", err)
	}

	if result.Claimed != 0 {
		t.Fatalf("expected no claimed deliveries, got %#v", result)
	}
	if len(store.claimParams) != 1 {
		t.Fatalf("expected one claim call, got %d", len(store.claimParams))
	}
	wantLockedUntil := now.Add(time.Duration(defaultWebhookBatchLimit)*defaultWebhookTimeout + defaultWebhookLockBuffer)
	if !store.claimParams[0].LockedUntil.Time.Equal(wantLockedUntil) {
		t.Fatalf("expected lock to cover full default batch until %v, got %v", wantLockedUntil, store.claimParams[0].LockedUntil.Time)
	}
}

func TestWebhookWorkerDoesNotFollowRedirects(t *testing.T) {
	now := time.Date(2026, 5, 22, 9, 0, 0, 0, time.UTC)
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/ok", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()
	store := &fakeWebhookStore{
		claimed: []db.ClaimPendingWebhookDeliveriesRow{{
			ID:           testUUID(57),
			EventID:      testUUID(58),
			EndpointUrl:  server.URL + "/redirect",
			Payload:      []byte(`{"event_type":"completed"}`),
			AttemptCount: 0,
			MaxAttempts:  3,
		}},
	}
	worker := NewWebhookWorker(store, WithWebhookClock(func() time.Time { return now }))

	result, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run webhooks: %v", err)
	}

	if result.Retried != 1 || result.Succeeded != 0 {
		t.Fatalf("expected redirect response to retry instead of succeeding, got %#v", result)
	}
	if strings.Join(seen, ",") != "POST /redirect" {
		t.Fatalf("expected webhook client to stop at redirect, saw %v", seen)
	}
	if len(store.failed) != 1 || !store.failed[0].ResponseStatus.Valid || store.failed[0].ResponseStatus.Int32 != http.StatusFound {
		t.Fatalf("expected redirect status to be recorded as failed attempt, got %#v", store.failed)
	}
}

func TestWebhookWorkerTreatsTwoXXWithUnreadableBodyAsSuccess(t *testing.T) {
	now := time.Date(2026, 5, 22, 9, 0, 0, 0, time.UTC)
	store := &fakeWebhookStore{
		claimed: []db.ClaimPendingWebhookDeliveriesRow{{
			ID:           testUUID(59),
			EventID:      testUUID(60),
			EndpointUrl:  "https://example.test/hooks/forge",
			Payload:      []byte(`{"event_type":"completed"}`),
			AttemptCount: 0,
			MaxAttempts:  3,
		}},
	}
	client := &fakeHTTPClient{response: &http.Response{
		StatusCode: http.StatusAccepted,
		Body:       io.NopCloser(errorReader{}),
	}}
	worker := NewWebhookWorker(store, WithWebhookClock(func() time.Time { return now }), WithWebhookHTTPClient(client))

	result, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run webhooks: %v", err)
	}

	if result.Succeeded != 1 || result.Retried != 0 || result.Failed != 0 {
		t.Fatalf("expected accepted webhook to succeed despite response body read error, got %#v", result)
	}
	if len(store.succeeded) != 1 {
		t.Fatalf("expected success update, got %d", len(store.succeeded))
	}
	if len(store.failed) != 0 {
		t.Fatalf("did not expect retry update, got %#v", store.failed)
	}
}

func TestWebhookWorkerRetriesServerFailure(t *testing.T) {
	now := time.Date(2026, 5, 22, 9, 0, 0, 0, time.UTC)
	store := &fakeWebhookStore{
		claimed: []db.ClaimPendingWebhookDeliveriesRow{{
			ID:           testUUID(53),
			EventID:      testUUID(54),
			EndpointUrl:  "https://example.test/hooks/forge",
			Payload:      []byte(`{"event_type":"failed"}`),
			AttemptCount: 1,
			MaxAttempts:  3,
		}},
	}
	client := &fakeHTTPClient{response: &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader("try later")),
	}}
	worker := NewWebhookWorker(store, WithWebhookClock(func() time.Time { return now }), WithWebhookHTTPClient(client))

	result, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run webhooks: %v", err)
	}

	if result.Retried != 1 || result.Failed != 0 || result.Succeeded != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	failed := store.failed[0]
	if failed.AttemptCount != 2 {
		t.Fatalf("expected second attempt, got %#v", failed)
	}
	if !failed.ResponseStatus.Valid || failed.ResponseStatus.Int32 != http.StatusInternalServerError {
		t.Fatalf("expected response status, got %#v", failed.ResponseStatus)
	}
	if !failed.NextAttemptAt.Time.Equal(now.Add(2 * time.Minute)) {
		t.Fatalf("expected exponential retry at %v, got %v", now.Add(2*time.Minute), failed.NextAttemptAt.Time)
	}
}

func TestWebhookWorkerMarksExhaustedNetworkFailure(t *testing.T) {
	now := time.Date(2026, 5, 22, 9, 0, 0, 0, time.UTC)
	store := &fakeWebhookStore{
		claimed: []db.ClaimPendingWebhookDeliveriesRow{{
			ID:           testUUID(55),
			EventID:      testUUID(56),
			EndpointUrl:  "https://example.test/hooks/forge",
			Payload:      []byte(`{"event_type":"blocked"}`),
			AttemptCount: 2,
			MaxAttempts:  3,
		}},
	}
	client := &fakeHTTPClient{err: errors.New("connection refused")}
	worker := NewWebhookWorker(store, WithWebhookClock(func() time.Time { return now }), WithWebhookHTTPClient(client))

	result, err := worker.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("run webhooks: %v", err)
	}

	if result.Failed != 1 || result.Retried != 0 || result.Succeeded != 0 {
		t.Fatalf("unexpected result: %#v", result)
	}
	failed := store.failed[0]
	if failed.AttemptCount != 3 || failed.Error != "connection refused" {
		t.Fatalf("unexpected exhausted failure params: %#v", failed)
	}
	if failed.ResponseStatus.Valid {
		t.Fatalf("network failures should not record a response status: %#v", failed.ResponseStatus)
	}
}

type fakeWebhookStore struct {
	claimParams []db.ClaimPendingWebhookDeliveriesParams
	claimed     []db.ClaimPendingWebhookDeliveriesRow
	succeeded   []db.MarkWebhookDeliverySucceededParams
	failed      []db.MarkWebhookDeliveryFailedParams
	attempts    map[pgtype.UUID]db.Attempt
	metrics     map[pgtype.UUID]db.AttemptMetric
}

func (s *fakeWebhookStore) ClaimPendingWebhookDeliveries(_ context.Context, params db.ClaimPendingWebhookDeliveriesParams) ([]db.ClaimPendingWebhookDeliveriesRow, error) {
	s.claimParams = append(s.claimParams, params)
	return s.claimed, nil
}

func (s *fakeWebhookStore) MarkWebhookDeliverySucceeded(_ context.Context, params db.MarkWebhookDeliverySucceededParams) (db.WebhookDelivery, error) {
	s.succeeded = append(s.succeeded, params)
	return db.WebhookDelivery{ID: params.ID, Status: "succeeded", AttemptCount: params.AttemptCount}, nil
}

func (s *fakeWebhookStore) MarkWebhookDeliveryFailed(_ context.Context, params db.MarkWebhookDeliveryFailedParams) (db.WebhookDelivery, error) {
	s.failed = append(s.failed, params)
	return db.WebhookDelivery{ID: params.ID, Status: "pending", AttemptCount: params.AttemptCount}, nil
}

func (s *fakeWebhookStore) GetAttempt(_ context.Context, id pgtype.UUID) (db.Attempt, error) {
	attempt, ok := s.attempts[id]
	if !ok {
		return db.Attempt{}, pgx.ErrNoRows
	}
	return attempt, nil
}

func (s *fakeWebhookStore) GetAttemptMetrics(_ context.Context, attemptID pgtype.UUID) (db.AttemptMetric, error) {
	metrics, ok := s.metrics[attemptID]
	if !ok {
		return db.AttemptMetric{}, pgx.ErrNoRows
	}
	return metrics, nil
}

type fakeHTTPClient struct {
	requests []*http.Request
	response *http.Response
	err      error
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) {
	return 0, errors.New("truncated response body")
}

func (c *fakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	c.requests = append(c.requests, req)
	if c.err != nil {
		return nil, c.err
	}
	return c.response, nil
}

func numericForWebhookTest(t *testing.T, value float64) pgtype.Numeric {
	t.Helper()

	var numeric pgtype.Numeric
	if err := numeric.Scan(strconv.FormatFloat(value, 'f', -1, 64)); err != nil {
		t.Fatalf("scan numeric: %v", err)
	}
	return numeric
}
