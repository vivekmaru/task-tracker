package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/contracts"
	"github.com/vivek/agent-task-tracker/internal/db"
	forgeruntime "github.com/vivek/agent-task-tracker/internal/runtime"
	"github.com/vivek/agent-task-tracker/internal/web"
)

func TestOpenAPIIncludesPhaseOneRoutes(t *testing.T) {
	router := NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected openapi status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var spec struct {
		Paths map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("decode openapi: %v", err)
	}

	for _, route := range []struct {
		method string
		path   string
	}{
		{"post", "/tickets"},
		{"post", "/tickets/propose"},
		{"get", "/tickets"},
		{"get", "/tickets/{id}"},
		{"patch", "/tickets/{id}"},
		{"post", "/tickets/{id}/decompose"},
		{"post", "/tickets/{id}/ready"},
		{"post", "/tickets/{id}/reopen"},
		{"post", "/tickets/{id}/unblock"},
		{"post", "/tickets/{id}/request-review"},
		{"post", "/tickets/{id}/review"},
		{"post", "/tickets/{id}/archive"},
		{"post", "/tickets/claim-next"},
		{"get", "/attempts/{id}"},
		{"patch", "/attempts/{id}"},
		{"post", "/attempts/{id}/heartbeat"},
		{"post", "/attempts/{id}/checkpoint"},
		{"post", "/attempts/{id}/complete"},
		{"post", "/attempts/{id}/fail"},
		{"post", "/attempts/{id}/block"},
		{"post", "/attempts/{id}/cancel"},
		{"get", "/tickets/{id}/events"},
		{"get", "/attempts/{id}/events"},
		{"get", "/events"},
		{"post", "/artifacts"},
		{"get", "/artifacts/{id}"},
		{"delete", "/artifacts/{id}"},
		{"get", "/analytics/summary"},
		{"get", "/analytics/by-model"},
		{"get", "/analytics/by-harness"},
		{"get", "/analytics/by-status"},
		{"get", "/analytics/by-agent"},
		{"get", "/observability/subscriptions"},
		{"post", "/observability/subscriptions"},
	} {
		methods, ok := spec.Paths[route.path]
		if !ok {
			t.Fatalf("expected OpenAPI path %s", route.path)
		}
		if _, ok := methods[route.method]; !ok {
			t.Fatalf("expected OpenAPI operation %s %s", route.method, route.path)
		}
	}
}

func TestObservabilitySubscriptionAPICreatesAndLists(t *testing.T) {
	rt := &fakeObservabilityRuntime{
		subscription: db.WebhookSubscription{
			ID:          testUUID(10),
			WorkspaceID: testUUID(2),
			ProjectID:   testUUID(3),
			EndpointUrl: "https://observability.example.test/forge/events",
			Secret:      pgtype.Text{String: "shared-secret", Valid: true},
			EventTypes:  []string{"completed", "failed"},
			Active:      true,
			MaxAttempts: 5,
			Description: "External sink",
		},
		subscriptions: []db.WebhookSubscription{{
			ID:          testUUID(10),
			WorkspaceID: testUUID(2),
			ProjectID:   testUUID(3),
			EndpointUrl: "https://observability.example.test/forge/events",
			Active:      true,
			MaxAttempts: 5,
			Description: "External sink",
		}},
	}
	router := NewRouterWithRuntime(rt)
	body := `{"workspace_id":"` + uuidString(t, testUUID(2)) + `","project_id":"` + uuidString(t, testUUID(3)) + `","endpoint_url":"https://observability.example.test/forge/events","secret":"shared-secret","event_types":["completed","failed"],"max_attempts":5,"description":"External sink"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/observability/subscriptions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected create status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rt.createReq.WorkspaceID != testUUID(2) || rt.createReq.ProjectID != testUUID(3) {
		t.Fatalf("unexpected create scope: %#v", rt.createReq)
	}
	if !rt.createReq.Secret.Valid || rt.createReq.Secret.String != "shared-secret" {
		t.Fatalf("expected secret in create request, got %#v", rt.createReq.Secret)
	}
	if got := strings.Join(rt.createReq.EventTypes, ","); got != "completed,failed" {
		t.Fatalf("expected event types completed,failed, got %#v", rt.createReq.EventTypes)
	}
	if !strings.Contains(rec.Body.String(), `"secret_set":true`) {
		t.Fatalf("expected redacted secret output, got %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/observability/subscriptions?workspace_id="+uuidString(t, testUUID(2))+"&project_id="+uuidString(t, testUUID(3))+"&all=true", nil)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected list status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rt.listReq.WorkspaceID != testUUID(2) || rt.listReq.ProjectID != testUUID(3) {
		t.Fatalf("unexpected list scope: %#v", rt.listReq)
	}
	if rt.listReq.ActiveOnly {
		t.Fatalf("expected all=true to include inactive subscriptions, got %#v", rt.listReq)
	}
	if !strings.Contains(rec.Body.String(), `"subscriptions":[`) {
		t.Fatalf("expected subscriptions output, got %s", rec.Body.String())
	}
}

func TestObservabilitySubscriptionAPIDefaultsOmittedEventTypesAndAttempts(t *testing.T) {
	rt := &fakeObservabilityRuntime{}
	router := NewRouterWithRuntime(rt)
	body := `{"workspace_id":"` + uuidString(t, testUUID(2)) + `","project_id":"` + uuidString(t, testUUID(3)) + `","endpoint_url":"https://observability.example.test/forge/events"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/observability/subscriptions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected create status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if rt.createReq.EventTypes == nil {
		t.Fatalf("expected omitted event_types to be an empty slice, got nil")
	}
	if len(rt.createReq.EventTypes) != 0 {
		t.Fatalf("expected omitted event_types to subscribe to all events, got %#v", rt.createReq.EventTypes)
	}
	if rt.createReq.MaxAttempts != 3 {
		t.Fatalf("expected omitted max_attempts to default to 3, got %d", rt.createReq.MaxAttempts)
	}
}

func TestObservabilitySubscriptionAPIRejectsExplicitZeroAttempts(t *testing.T) {
	router := NewRouterWithRuntime(&fakeObservabilityRuntime{})
	body := `{"workspace_id":"` + uuidString(t, testUUID(2)) + `","project_id":"` + uuidString(t, testUUID(3)) + `","endpoint_url":"https://observability.example.test/forge/events","max_attempts":0}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/observability/subscriptions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "max_attempts must be greater than zero") {
		t.Fatalf("expected max_attempts validation error, got %s", rec.Body.String())
	}
}

func TestObservabilitySubscriptionAPIRejectsUnsafeURL(t *testing.T) {
	router := NewRouterWithRuntime(&fakeObservabilityRuntime{})
	body := `{"workspace_id":"` + uuidString(t, testUUID(2)) + `","project_id":"` + uuidString(t, testUUID(3)) + `","endpoint_url":"file:///tmp/sink"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/observability/subscriptions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "endpoint_url must use http or https") {
		t.Fatalf("expected unsafe URL error, got %s", rec.Body.String())
	}
}

func TestObservabilitySubscriptionAPIReturnsNotFoundForMissingScope(t *testing.T) {
	rt := &fakeObservabilityRuntime{
		createErr: &pgconn.PgError{Code: "23503", Message: "insert or update violates foreign key constraint"},
	}
	router := NewRouterWithRuntime(rt)
	body := `{"workspace_id":"` + uuidString(t, testUUID(2)) + `","project_id":"` + uuidString(t, testUUID(3)) + `","endpoint_url":"https://observability.example.test/forge/events"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/observability/subscriptions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "referenced workspace or project does not exist") {
		t.Fatalf("expected missing scope error, got %s", rec.Body.String())
	}
}

func TestListEventsRouteValidatesQueryParams(t *testing.T) {
	router := NewRouterWithRuntime(forgeruntime.New(db.New(nil)))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events?workspace_id=not-a-uuid", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected event route status 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

type fakeObservabilityRuntime struct {
	web.Runtime
	createReq     db.CreateWebhookSubscriptionParams
	subscription  db.WebhookSubscription
	createErr     error
	listReq       db.ListWebhookSubscriptionsParams
	subscriptions []db.WebhookSubscription
}

func (f *fakeObservabilityRuntime) CreateWebhookSubscription(_ context.Context, req db.CreateWebhookSubscriptionParams) (db.WebhookSubscription, error) {
	f.createReq = req
	if f.createErr != nil {
		return db.WebhookSubscription{}, f.createErr
	}
	return f.subscription, nil
}

func (f *fakeObservabilityRuntime) ListWebhookSubscriptions(_ context.Context, req db.ListWebhookSubscriptionsParams) ([]db.WebhookSubscription, error) {
	f.listReq = req
	return f.subscriptions, nil
}

func testUUID(seed byte) pgtype.UUID {
	var bytes [16]byte
	bytes[15] = seed
	return pgtype.UUID{Bytes: bytes, Valid: true}
}

func uuidString(t *testing.T, id pgtype.UUID) string {
	t.Helper()
	value, err := id.Value()
	if err != nil {
		t.Fatalf("uuid value: %v", err)
	}
	text, ok := value.(string)
	if !ok {
		t.Fatalf("expected uuid string, got %T", value)
	}
	return text
}

func TestNewRouterWithRuntimeKeepsOpenAPIRoutes(t *testing.T) {
	router := NewRouterWithRuntime(forgeruntime.New(db.New(nil)))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected openapi status 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestNewRouterWithRuntimeMountsSearchPage(t *testing.T) {
	router := NewRouterWithRuntime(forgeruntime.New(db.New(nil)))
	req := httptest.NewRequest(http.MethodGet, "/search?workspace_id=00000000-0000-0000-0000-000000000001&project_id=00000000-0000-0000-0000-000000000002", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected search page status 400 for missing query, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "Forge Search") || !strings.Contains(body, "query is required") {
		t.Fatalf("expected mounted search page guidance, got:\n%s", body)
	}
}

func TestNewRouterWithRuntimeMountsEventLedgerPage(t *testing.T) {
	router := NewRouterWithRuntime(forgeruntime.New(db.New(nil)))
	req := httptest.NewRequest(http.MethodGet, "/events?limit=-1", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected event ledger page status 400 for invalid limit, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "Execution Ledger") || !strings.Contains(body, "limit must be a non-negative integer") {
		t.Fatalf("expected mounted event ledger guidance, got:\n%s", body)
	}
}

func TestNewRouterWithRuntimeMountsArtifactListPage(t *testing.T) {
	router := NewRouterWithRuntime(forgeruntime.New(db.New(nil)))
	req := httptest.NewRequest(http.MethodGet, "/artifacts", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected artifact list page status 400 for missing scope, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "Artifacts") || !strings.Contains(body, "workspace_id and project_id are required") {
		t.Fatalf("expected mounted artifact list guidance, got:\n%s", body)
	}
}

func TestNewRouterWithRuntimeMountsProposedListPageWithoutSlashRedirect(t *testing.T) {
	router := NewRouterWithRuntime(forgeruntime.New(db.New(nil)))
	req := httptest.NewRequest(http.MethodGet, "/proposed", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected proposed list page status 400 for missing scope, got %d: %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "Proposed Work") || !strings.Contains(body, "workspace_id is required") {
		t.Fatalf("expected mounted proposed list guidance, got:\n%s", body)
	}
}

func TestOpenAPIUsesContractOperationIDsForRESTBoundOperations(t *testing.T) {
	router := NewRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected openapi status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var spec struct {
		Paths map[string]map[string]struct {
			OperationID string `json:"operationId"`
		} `json:"paths"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("decode openapi: %v", err)
	}

	routes := map[string]struct {
		method string
		path   string
	}{
		contracts.OperationCreateTicket:     {"post", "/tickets"},
		contracts.OperationProposeTicket:    {"post", "/tickets/propose"},
		contracts.OperationClaimNextTicket:  {"post", "/tickets/claim-next"},
		contracts.OperationHeartbeatAttempt: {"post", "/attempts/{id}/heartbeat"},
		contracts.OperationCheckpointAttempt: {
			method: "post",
			path:   "/attempts/{id}/checkpoint",
		},
		contracts.OperationUpdateTicket:    {"patch", "/tickets/{id}"},
		contracts.OperationMarkTicketReady: {"post", "/tickets/{id}/ready"},
		contracts.OperationReopenTicket:    {"post", "/tickets/{id}/reopen"},
		contracts.OperationUnblockTicket:   {"post", "/tickets/{id}/unblock"},
		contracts.OperationRequestTicketReview: {
			method: "post",
			path:   "/tickets/{id}/request-review",
		},
		contracts.OperationReviewTicket:       {"post", "/tickets/{id}/review"},
		contracts.OperationArchiveTicket:      {"post", "/tickets/{id}/archive"},
		contracts.OperationCompleteAttempt:    {"post", "/attempts/{id}/complete"},
		contracts.OperationFailAttempt:        {"post", "/attempts/{id}/fail"},
		contracts.OperationBlockAttempt:       {"post", "/attempts/{id}/block"},
		contracts.OperationListTickets:        {"get", "/tickets"},
		contracts.OperationGetTicket:          {"get", "/tickets/{id}"},
		contracts.OperationAttachArtifact:     {"post", "/artifacts"},
		contracts.OperationDecomposeTicket:    {"post", "/tickets/{id}/decompose"},
		contracts.OperationAnalyticsSummary:   {"get", "/analytics/summary"},
		contracts.OperationAnalyticsByModel:   {"get", "/analytics/by-model"},
		contracts.OperationAnalyticsByHarness: {"get", "/analytics/by-harness"},
		contracts.OperationAnalyticsByStatus:  {"get", "/analytics/by-status"},
		contracts.OperationAnalyticsByAgent:   {"get", "/analytics/by-agent"},
	}

	for operationName, route := range routes {
		operation := contracts.MustOperation(operationName)
		if operation.Bindings.RESTOperationID == "" {
			t.Fatalf("%s should declare the REST binding exposed by %s %s", operationName, route.method, route.path)
		}
		methods, ok := spec.Paths[route.path]
		if !ok {
			t.Fatalf("expected OpenAPI path %s for %s", route.path, operationName)
		}
		operationSpec, ok := methods[route.method]
		if !ok {
			t.Fatalf("expected OpenAPI operation %s %s for %s", route.method, route.path, operationName)
		}
		if operationSpec.OperationID != operation.Bindings.RESTOperationID {
			t.Fatalf("%s OpenAPI operationId = %q, want contract binding %q", operationName, operationSpec.OperationID, operation.Bindings.RESTOperationID)
		}
	}
}
