package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
