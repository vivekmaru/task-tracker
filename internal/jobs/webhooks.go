package jobs

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

const (
	WebhookDeliveryJobKind = "forge_webhook_delivery"

	defaultWebhookBatchLimit = 25
	defaultWebhookLockTTL    = 2 * time.Minute
	defaultWebhookTimeout    = 10 * time.Second
	defaultWebhookLockBuffer = defaultWebhookTimeout
)

type WebhookDeliveryStore interface {
	ClaimPendingWebhookDeliveries(context.Context, db.ClaimPendingWebhookDeliveriesParams) ([]db.ClaimPendingWebhookDeliveriesRow, error)
	MarkWebhookDeliverySucceeded(context.Context, db.MarkWebhookDeliverySucceededParams) (db.WebhookDelivery, error)
	MarkWebhookDeliveryFailed(context.Context, db.MarkWebhookDeliveryFailedParams) (db.WebhookDelivery, error)
}

var _ WebhookDeliveryStore = (*db.Queries)(nil)

type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

type WebhookWorker struct {
	store      WebhookDeliveryStore
	client     HTTPDoer
	now        func() time.Time
	batchLimit int32
	lockTTL    time.Duration
}

type WebhookOption func(*WebhookWorker)

func WithWebhookClock(clock func() time.Time) WebhookOption {
	return func(worker *WebhookWorker) {
		worker.now = clock
	}
}

func WithWebhookBatchLimit(limit int32) WebhookOption {
	return func(worker *WebhookWorker) {
		worker.batchLimit = limit
	}
}

func WithWebhookHTTPClient(client HTTPDoer) WebhookOption {
	return func(worker *WebhookWorker) {
		worker.client = client
	}
}

func NewWebhookWorker(store WebhookDeliveryStore, opts ...WebhookOption) *WebhookWorker {
	worker := &WebhookWorker{
		store:      store,
		client:     newWebhookHTTPClient(),
		now:        time.Now,
		batchLimit: defaultWebhookBatchLimit,
		lockTTL:    defaultWebhookLockTTL,
	}
	for _, opt := range opts {
		opt(worker)
	}
	if worker.batchLimit <= 0 {
		worker.batchLimit = defaultWebhookBatchLimit
	}
	if worker.lockTTL <= 0 {
		worker.lockTTL = defaultWebhookLockTTL
	}
	return worker
}

func newWebhookHTTPClient() *http.Client {
	return &http.Client{
		Timeout: defaultWebhookTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

type WebhookRunResult struct {
	Claimed   int
	Succeeded int
	Failed    int
	Retried   int
}

func (w *WebhookWorker) RunOnce(ctx context.Context) (WebhookRunResult, error) {
	now := w.now().UTC()
	deliveries, err := w.store.ClaimPendingWebhookDeliveries(ctx, db.ClaimPendingWebhookDeliveriesParams{
		Now:         timestamptz(now),
		LockedUntil: timestamptz(now.Add(w.claimLockTTL())),
		BatchLimit:  w.batchLimit,
	})
	if err != nil {
		return WebhookRunResult{}, fmt.Errorf("claim webhook deliveries: %w", err)
	}

	result := WebhookRunResult{Claimed: len(deliveries)}
	for _, delivery := range deliveries {
		outcome := w.deliver(ctx, delivery)
		if outcome.err == nil && outcome.statusCode >= 200 && outcome.statusCode < 300 {
			if err := w.markSucceeded(ctx, delivery, outcome); err != nil {
				return WebhookRunResult{}, err
			}
			result.Succeeded++
			continue
		}

		exhausted, err := w.markFailed(ctx, delivery, outcome)
		if err != nil {
			return WebhookRunResult{}, err
		}
		if exhausted {
			result.Failed++
		} else {
			result.Retried++
		}
	}
	return result, nil
}

func (w *WebhookWorker) claimLockTTL() time.Duration {
	batchTTL := time.Duration(w.batchLimit)*defaultWebhookTimeout + defaultWebhookLockBuffer
	if batchTTL > w.lockTTL {
		return batchTTL
	}
	return w.lockTTL
}

type deliveryOutcome struct {
	statusCode int32
	body       string
	err        error
}

func (w *WebhookWorker) deliver(ctx context.Context, delivery db.ClaimPendingWebhookDeliveriesRow) deliveryOutcome {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, delivery.EndpointUrl, bytes.NewReader(delivery.Payload))
	if err != nil {
		return deliveryOutcome{err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "forge-webhooks/1")
	req.Header.Set("X-Forge-Event-ID", uuidString(delivery.EventID))
	req.Header.Set("X-Forge-Delivery-ID", uuidString(delivery.ID))
	if delivery.Secret.Valid && strings.TrimSpace(delivery.Secret.String) != "" {
		req.Header.Set("X-Forge-Signature-SHA256", webhookSignature(delivery.Secret.String, delivery.Payload))
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return deliveryOutcome{err: err}
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4096))
	outcome := deliveryOutcome{statusCode: int32(resp.StatusCode), body: string(body)}
	if readErr != nil && (resp.StatusCode < 200 || resp.StatusCode >= 300) {
		outcome.err = readErr
	}
	return outcome
}

func (w *WebhookWorker) markSucceeded(ctx context.Context, delivery db.ClaimPendingWebhookDeliveriesRow, outcome deliveryOutcome) error {
	_, err := w.store.MarkWebhookDeliverySucceeded(ctx, db.MarkWebhookDeliverySucceededParams{
		ID:             delivery.ID,
		AttemptCount:   delivery.AttemptCount + 1,
		AttemptedAt:    timestamptz(w.now().UTC()),
		ResponseStatus: nullableInt4(outcome.statusCode),
		ResponseBody:   nullableText(outcome.body),
	})
	if err != nil {
		return fmt.Errorf("mark webhook delivery %v succeeded: %w", delivery.ID, err)
	}
	return nil
}

func (w *WebhookWorker) markFailed(ctx context.Context, delivery db.ClaimPendingWebhookDeliveriesRow, outcome deliveryOutcome) (bool, error) {
	attemptCount := delivery.AttemptCount + 1
	errorText := webhookDeliveryError(outcome)
	_, err := w.store.MarkWebhookDeliveryFailed(ctx, db.MarkWebhookDeliveryFailedParams{
		ID:             delivery.ID,
		AttemptCount:   attemptCount,
		AttemptedAt:    timestamptz(w.now().UTC()),
		ResponseStatus: nullableInt4(outcome.statusCode),
		ResponseBody:   nullableText(outcome.body),
		Error:          errorText,
		NextAttemptAt:  timestamptz(w.now().UTC().Add(webhookBackoff(attemptCount))),
	})
	if err != nil {
		return false, fmt.Errorf("mark webhook delivery %v failed: %w", delivery.ID, err)
	}
	return attemptCount >= delivery.MaxAttempts, nil
}

func webhookDeliveryError(outcome deliveryOutcome) string {
	if outcome.err != nil {
		return outcome.err.Error()
	}
	return fmt.Sprintf("webhook endpoint returned HTTP %d", outcome.statusCode)
}

func webhookBackoff(attemptCount int32) time.Duration {
	if attemptCount < 1 {
		attemptCount = 1
	}
	if attemptCount > 6 {
		attemptCount = 6
	}
	return time.Duration(1<<uint(attemptCount-1)) * time.Minute
}

func webhookSignature(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func nullableInt4(value int32) pgtype.Int4 {
	if value == 0 {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: value, Valid: true}
}

func nullableText(value string) pgtype.Text {
	if value == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: value, Valid: true}
}

func timestamptz(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

func uuidString(value pgtype.UUID) string {
	if !value.Valid {
		return ""
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", value.Bytes[0:4], value.Bytes[4:6], value.Bytes[6:8], value.Bytes[8:10], value.Bytes[10:16])
}
