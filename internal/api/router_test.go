package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vivek/agent-task-tracker/internal/contracts"
	"github.com/vivek/agent-task-tracker/internal/db"
	forgeruntime "github.com/vivek/agent-task-tracker/internal/runtime"
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
		{"post", "/artifacts"},
		{"get", "/artifacts/{id}"},
		{"delete", "/artifacts/{id}"},
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
		contracts.OperationReviewTicket:    {"post", "/tickets/{id}/review"},
		contracts.OperationArchiveTicket:   {"post", "/tickets/{id}/archive"},
		contracts.OperationCompleteAttempt: {"post", "/attempts/{id}/complete"},
		contracts.OperationFailAttempt:     {"post", "/attempts/{id}/fail"},
		contracts.OperationBlockAttempt:    {"post", "/attempts/{id}/block"},
		contracts.OperationListTickets:     {"get", "/tickets"},
		contracts.OperationGetTicket:       {"get", "/tickets/{id}"},
		contracts.OperationAttachArtifact:  {"post", "/artifacts"},
		contracts.OperationDecomposeTicket: {"post", "/tickets/{id}/decompose"},
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
