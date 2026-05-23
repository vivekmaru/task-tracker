package jobs

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

func TestWebhookWorkerPostsPayloadAndMarksSuccess(t *testing.T) {
	now := time.Date(2026, 5, 22, 9, 0, 0, 0, time.UTC)
	eventID := testUUID(51)
	deliveryID := testUUID(52)
	store := &fakeWebhookStore{
		claimed: []db.ClaimPendingWebhookDeliveriesRow{{
			ID:           deliveryID,
			EventID:      eventID,
			EndpointUrl:  "https://example.test/hooks/forge",
			Secret:       pgtype.Text{String: "secret", Valid: true},
			Payload:      []byte(`{"event_type":"completed","ticket_id":"ticket-1"}`),
			AttemptCount: 0,
			MaxAttempts:  3,
		}},
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
	if got := req.Header.Get("X-Forge-Signature-SHA256"); got != webhookSignature("secret", store.claimed[0].Payload) {
		t.Fatalf("unexpected signature: %q", got)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	if string(body) != string(store.claimed[0].Payload) {
		t.Fatalf("expected raw delivery payload, got %s", string(body))
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

type fakeHTTPClient struct {
	requests []*http.Request
	response *http.Response
	err      error
}

func (c *fakeHTTPClient) Do(req *http.Request) (*http.Response, error) {
	c.requests = append(c.requests, req)
	if c.err != nil {
		return nil, c.err
	}
	return c.response, nil
}
