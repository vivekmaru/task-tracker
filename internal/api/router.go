package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/vivek/agent-task-tracker/internal/contracts"
	forgeruntime "github.com/vivek/agent-task-tracker/internal/runtime"
)

const basePath = "/api/v1"

func NewRouter() http.Handler {
	return NewRouterWithRuntime(nil)
}

func NewRouterWithRuntime(rt *forgeruntime.Runtime) http.Handler {
	mux := http.NewServeMux()
	api := humago.NewWithPrefix(mux, basePath, huma.DefaultConfig("Forge API", "0.1.0"))
	RegisterPhaseOneRoutes(api, rt)
	return mux
}

func RegisterPhaseOneRoutes(api huma.API, rt *forgeruntime.Runtime) {
	_ = rt
	register[bodyInput](api, http.MethodPost, "/tickets", contracts.RESTCreateTicket, "Create ticket")
	register[bodyInput](api, http.MethodPost, "/tickets/propose", contracts.RESTProposeTicket, "Propose ticket")
	register[listTicketsInput](api, http.MethodGet, "/tickets", contracts.RESTListTickets, "List tickets")
	register[idInput](api, http.MethodGet, "/tickets/{id}", contracts.RESTGetTicket, "Get ticket")
	register[idBodyInput](api, http.MethodPatch, "/tickets/{id}", "update-ticket", "Update ticket")
	register[idBodyInput](api, http.MethodPost, "/tickets/{id}/decompose", contracts.RESTDecomposeTicket, "Decompose ticket")
	register[idBodyInput](api, http.MethodPost, "/tickets/{id}/ready", "ready-ticket", "Move ticket to todo")

	register[bodyInput](api, http.MethodPost, "/tickets/claim-next", contracts.RESTClaimNextTicket, "Claim next ticket")

	register[idInput](api, http.MethodGet, "/attempts/{id}", "get-attempt", "Get attempt")
	register[idBodyInput](api, http.MethodPatch, "/attempts/{id}", "update-attempt", "Update attempt")
	register[idBodyInput](api, http.MethodPost, "/attempts/{id}/heartbeat", contracts.RESTHeartbeat, "Heartbeat attempt")
	register[idBodyInput](api, http.MethodPost, "/attempts/{id}/checkpoint", contracts.RESTCheckpoint, "Checkpoint attempt")
	register[idBodyInput](api, http.MethodPost, "/attempts/{id}/complete", contracts.RESTCompleteAttempt, "Complete attempt")
	register[idBodyInput](api, http.MethodPost, "/attempts/{id}/fail", contracts.RESTFailAttempt, "Fail attempt")
	register[idBodyInput](api, http.MethodPost, "/attempts/{id}/block", contracts.RESTBlockAttempt, "Block attempt")
	register[idBodyInput](api, http.MethodPost, "/attempts/{id}/cancel", "cancel-attempt", "Cancel attempt")

	register[idInput](api, http.MethodGet, "/tickets/{id}/events", "list-ticket-events", "List ticket events")
	register[idInput](api, http.MethodGet, "/attempts/{id}/events", "list-attempt-events", "List attempt events")

	register[bodyInput](api, http.MethodPost, "/artifacts", contracts.RESTAttachArtifact, "Register artifact")
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
