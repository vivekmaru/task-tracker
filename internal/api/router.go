package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
)

const basePath = "/api/v1"

func NewRouter() http.Handler {
	mux := http.NewServeMux()
	api := humago.NewWithPrefix(mux, basePath, huma.DefaultConfig("Forge API", "0.1.0"))
	RegisterPhaseOneRoutes(api)
	return mux
}

func RegisterPhaseOneRoutes(api huma.API) {
	register[bodyInput](api, http.MethodPost, "/tickets", "create-ticket", "Create ticket")
	register[bodyInput](api, http.MethodPost, "/tickets/propose", "propose-ticket", "Propose ticket")
	register[listTicketsInput](api, http.MethodGet, "/tickets", "list-tickets", "List tickets")
	register[idInput](api, http.MethodGet, "/tickets/{id}", "get-ticket", "Get ticket")
	register[idBodyInput](api, http.MethodPatch, "/tickets/{id}", "update-ticket", "Update ticket")
	register[idBodyInput](api, http.MethodPost, "/tickets/{id}/decompose", "decompose-ticket", "Decompose ticket")
	register[idBodyInput](api, http.MethodPost, "/tickets/{id}/ready", "ready-ticket", "Move ticket to todo")

	register[bodyInput](api, http.MethodPost, "/tickets/claim-next", "claim-next-ticket", "Claim next ticket")

	register[idInput](api, http.MethodGet, "/attempts/{id}", "get-attempt", "Get attempt")
	register[idBodyInput](api, http.MethodPatch, "/attempts/{id}", "update-attempt", "Update attempt")
	register[idBodyInput](api, http.MethodPost, "/attempts/{id}/heartbeat", "heartbeat-attempt", "Heartbeat attempt")
	register[idBodyInput](api, http.MethodPost, "/attempts/{id}/checkpoint", "checkpoint-attempt", "Checkpoint attempt")
	register[idBodyInput](api, http.MethodPost, "/attempts/{id}/complete", "complete-attempt", "Complete attempt")
	register[idBodyInput](api, http.MethodPost, "/attempts/{id}/fail", "fail-attempt", "Fail attempt")
	register[idBodyInput](api, http.MethodPost, "/attempts/{id}/block", "block-attempt", "Block attempt")
	register[idBodyInput](api, http.MethodPost, "/attempts/{id}/cancel", "cancel-attempt", "Cancel attempt")

	register[idInput](api, http.MethodGet, "/tickets/{id}/events", "list-ticket-events", "List ticket events")
	register[idInput](api, http.MethodGet, "/attempts/{id}/events", "list-attempt-events", "List attempt events")

	register[bodyInput](api, http.MethodPost, "/artifacts", "create-artifact", "Register artifact")
	register[idInput](api, http.MethodGet, "/artifacts/{id}", "get-artifact", "Get artifact")
	register[idInput](api, http.MethodDelete, "/artifacts/{id}", "delete-artifact", "Delete artifact")
}

func register[I any](api huma.API, method, path, operationID, summary string) {
	huma.Register[I, placeholderOutput](api, huma.Operation{
		OperationID: operationID,
		Method:      method,
		Path:        path,
		Summary:     summary,
		Tags:        []string{"Phase 1"},
	}, func(context.Context, *I) (*placeholderOutput, error) {
		return nil, huma.Error501NotImplemented("route is registered; handler wiring is not implemented yet")
	})
}

type bodyInput struct {
	Body map[string]any `json:"body,omitempty"`
}

type idInput struct {
	ID string `path:"id" doc:"Resource ID"`
}

type idBodyInput struct {
	ID   string         `path:"id" doc:"Resource ID"`
	Body map[string]any `json:"body,omitempty"`
}

type listTicketsInput struct {
	WorkspaceID string `query:"workspace_id,omitempty"`
	ProjectID   string `query:"project_id,omitempty"`
	Status      string `query:"status,omitempty"`
	Type        string `query:"type,omitempty"`
	Offset      int32  `query:"offset,omitempty"`
	Limit       int32  `query:"limit,omitempty"`
}

type placeholderOutput struct {
	Body map[string]string `json:"body"`
}
