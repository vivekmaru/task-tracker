package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/contracts"
	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/services"
	"github.com/vivek/agent-task-tracker/internal/web"
)

type resourceRuntime interface {
	CreateTicket(context.Context, services.CreateTicketRequest) (db.Ticket, error)
	ProposeTicket(context.Context, services.CreateTicketRequest) (db.Ticket, error)
	ListTickets(context.Context, services.ListTicketsRequest) ([]db.Ticket, error)
	GetTicket(context.Context, pgtype.UUID) (db.Ticket, error)
	UpdateTicket(context.Context, services.UpdateTicketRequest) (db.Ticket, error)
	DecomposeTicket(context.Context, services.DecomposeTicketRequest) (services.DecomposeTicketResult, error)
	RegisterArtifact(context.Context, services.RegisterArtifactRequest) (db.Artifact, error)
	GetArtifact(context.Context, pgtype.UUID) (db.Artifact, error)
	DeleteLocalArtifact(context.Context, pgtype.UUID) (db.Artifact, error)
	ClaimNext(context.Context, services.ClaimNextRequest) (services.ClaimNextResult, error)
	GetAttempt(context.Context, pgtype.UUID) (db.Attempt, error)
	Heartbeat(context.Context, services.HeartbeatRequest) (db.Attempt, error)
	Checkpoint(context.Context, services.CheckpointRequest) (services.CheckpointResult, error)
	Complete(context.Context, services.CompleteAttemptRequest) (services.AttemptTransitionResult, error)
	Fail(context.Context, services.FailAttemptRequest) (services.AttemptTransitionResult, error)
	Block(context.Context, services.BlockAttemptRequest) (services.AttemptTransitionResult, error)
	Cancel(context.Context, services.CancelAttemptRequest) (services.AttemptTransitionResult, error)
	ListEvents(context.Context, services.ListEventsRequest) (services.ListEventsResult, error)
}

type ticketCreateBody struct {
	WorkspaceID          string         `json:"workspace_id" doc:"Workspace UUID"`
	ProjectID            string         `json:"project_id" doc:"Project UUID"`
	ParentID             string         `json:"parent_id,omitempty"`
	RootID               string         `json:"root_id,omitempty"`
	Title                string         `json:"title"`
	Description          string         `json:"description,omitempty"`
	Type                 string         `json:"type"`
	Status               string         `json:"status,omitempty"`
	Priority             *int32         `json:"priority,omitempty" minimum:"0" maximum:"4"`
	Tags                 []string       `json:"tags,omitempty"`
	AcceptanceCriteria   []string       `json:"acceptance_criteria"`
	VerificationCommands []string       `json:"verification_commands,omitempty"`
	ExpectedArtifacts    []string       `json:"expected_artifacts,omitempty"`
	RelevantPaths        []string       `json:"relevant_paths,omitempty"`
	RequiredTools        []string       `json:"required_tools,omitempty"`
	RequiredPermissions  []string       `json:"required_permissions,omitempty"`
	Environment          map[string]any `json:"environment,omitempty"`
	Input                map[string]any `json:"input,omitempty"`
	InputSchema          string         `json:"input_schema,omitempty"`
	RequiredCapabilities []string       `json:"required_capabilities,omitempty"`
	AllowedHarnesses     []string       `json:"allowed_harnesses,omitempty"`
	Dependencies         []string       `json:"dependencies,omitempty"`
	CreatedBy            string         `json:"created_by,omitempty"`
	CreatedByID          string         `json:"created_by_id,omitempty"`
	CreationReason       string         `json:"creation_reason,omitempty"`
	Enqueue              bool           `json:"enqueue,omitempty"`
}

type ticketCreateInput struct {
	Body ticketCreateBody `json:"body"`
}
type ticketResponse struct {
	ID                 string   `json:"id"`
	WorkspaceID        string   `json:"workspace_id"`
	ProjectID          string   `json:"project_id"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	Type               string   `json:"type"`
	Status             string   `json:"status"`
	Priority           int32    `json:"priority"`
	Tags               []string `json:"tags"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	CreatedBy          string   `json:"created_by"`
}
type ticketOutput struct {
	Body ticketResponse `json:"body"`
}
type ticketsOutput struct {
	Body struct {
		Tickets []ticketResponse `json:"tickets"`
	} `json:"body"`
}
type ticketGetInput struct {
	ID string `path:"id"`
}
type ticketUpdateInput struct {
	ID   string           `path:"id"`
	Body ticketUpdateBody `json:"body"`
}
type ticketUpdateBody struct {
	Title                string    `json:"title,omitempty"`
	Description          string    `json:"description,omitempty"`
	Tags                 *[]string `json:"tags,omitempty"`
	AcceptanceCriteria   *[]string `json:"acceptance_criteria,omitempty"`
	VerificationCommands *[]string `json:"verification_commands,omitempty"`
	RelevantPaths        *[]string `json:"relevant_paths,omitempty"`
	ActorType            string    `json:"actor_type,omitempty"`
	ActorID              string    `json:"actor_id,omitempty"`
}
type decomposeInput struct {
	ID   string        `path:"id"`
	Body decomposeBody `json:"body"`
}
type decomposeBody struct {
	WorkspaceID    string               `json:"workspace_id"`
	ProjectID      string               `json:"project_id"`
	RootID         string               `json:"root_id,omitempty"`
	Mode           string               `json:"mode,omitempty"`
	CreatedBy      string               `json:"created_by,omitempty"`
	CreatedByID    string               `json:"created_by_id,omitempty"`
	CreationReason string               `json:"creation_reason,omitempty"`
	Children       []decomposeChildBody `json:"children"`
}
type decomposeChildBody struct {
	Key                  string   `json:"key"`
	Title                string   `json:"title"`
	Description          string   `json:"description,omitempty"`
	Type                 string   `json:"type"`
	Priority             *int32   `json:"priority,omitempty"`
	AcceptanceCriteria   []string `json:"acceptance_criteria"`
	VerificationCommands []string `json:"verification_commands,omitempty"`
	DependsOn            []string `json:"depends_on,omitempty"`
}
type decomposeOutput struct {
	Body struct {
		Children []ticketResponse `json:"children"`
	} `json:"body"`
}

type artifactCreateInput struct {
	Body artifactCreateBody `json:"body"`
}
type artifactCreateBody struct {
	WorkspaceID    string         `json:"workspace_id"`
	ProjectID      string         `json:"project_id"`
	TicketID       string         `json:"ticket_id"`
	AttemptID      string         `json:"attempt_id,omitempty"`
	Type           string         `json:"type"`
	Role           string         `json:"role"`
	Name           string         `json:"name"`
	URL            string         `json:"url"`
	StorageBackend string         `json:"storage_backend,omitempty"`
	SizeBytes      int64          `json:"size_bytes,omitempty" minimum:"0"`
	MimeType       string         `json:"mime_type,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}
type artifactGetInput struct {
	ID string `path:"id"`
}
type artifactResponse struct {
	ID             string `json:"id"`
	WorkspaceID    string `json:"workspace_id"`
	ProjectID      string `json:"project_id"`
	TicketID       string `json:"ticket_id"`
	AttemptID      string `json:"attempt_id,omitempty"`
	Type           string `json:"type"`
	Role           string `json:"role"`
	Name           string `json:"name"`
	URL            string `json:"url"`
	StorageBackend string `json:"storage_backend"`
	SizeBytes      int64  `json:"size_bytes"`
	MimeType       string `json:"mime_type"`
}
type artifactOutput struct {
	Body artifactResponse `json:"body"`
}

func registerResourceRoutes(api huma.API, rt web.Runtime) {
	resources, _ := rt.(resourceRuntime)
	huma.Register[ticketCreateInput, ticketOutput](api, huma.Operation{OperationID: contracts.RESTCreateTicket, Method: http.MethodPost, Path: "/tickets", Summary: "Create ticket", Tags: []string{"Tickets"}}, func(ctx context.Context, input *ticketCreateInput) (*ticketOutput, error) {
		if resources == nil {
			return nil, huma.Error501NotImplemented("resource runtime is not configured")
		}
		req, err := mapTicketCreate(input.Body)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		ticket, err := resources.CreateTicket(ctx, req)
		if err != nil {
			return nil, resourceError(err, "create ticket failed")
		}
		return &ticketOutput{Body: makeTicketResponse(ticket)}, nil
	})
	huma.Register[ticketCreateInput, ticketOutput](api, huma.Operation{OperationID: contracts.RESTProposeTicket, Method: http.MethodPost, Path: "/tickets/propose", Summary: "Propose ticket", Tags: []string{"Tickets"}}, func(ctx context.Context, input *ticketCreateInput) (*ticketOutput, error) {
		if resources == nil {
			return nil, huma.Error501NotImplemented("resource runtime is not configured")
		}
		req, err := mapTicketCreate(input.Body)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		ticket, err := resources.ProposeTicket(ctx, req)
		if err != nil {
			return nil, resourceError(err, "propose ticket failed")
		}
		return &ticketOutput{Body: makeTicketResponse(ticket)}, nil
	})
	huma.Register[listTicketsInput, ticketsOutput](api, huma.Operation{OperationID: contracts.RESTListTickets, Method: http.MethodGet, Path: "/tickets", Summary: "List tickets", Tags: []string{"Tickets"}}, func(ctx context.Context, input *listTicketsInput) (*ticketsOutput, error) {
		if resources == nil {
			return nil, huma.Error501NotImplemented("resource runtime is not configured")
		}
		workspaceID, err := parseRequiredUUID("workspace_id", input.WorkspaceID)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		projectID, err := parseRequiredUUID("project_id", input.ProjectID)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		tickets, err := resources.ListTickets(ctx, services.ListTicketsRequest{WorkspaceID: workspaceID, ProjectID: projectID, Status: input.Status, Type: input.Type, Offset: input.Offset, Limit: input.Limit})
		if err != nil {
			return nil, resourceError(err, "list tickets failed")
		}
		out := &ticketsOutput{}
		out.Body.Tickets = makeTicketResponses(tickets)
		return out, nil
	})
	huma.Register[ticketGetInput, ticketOutput](api, huma.Operation{OperationID: contracts.RESTGetTicket, Method: http.MethodGet, Path: "/tickets/{id}", Summary: "Get ticket", Tags: []string{"Tickets"}}, func(ctx context.Context, input *ticketGetInput) (*ticketOutput, error) {
		if resources == nil {
			return nil, huma.Error501NotImplemented("resource runtime is not configured")
		}
		id, err := parseRequiredUUID("id", input.ID)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		ticket, err := resources.GetTicket(ctx, id)
		if err != nil {
			return nil, resourceError(err, "get ticket failed")
		}
		return &ticketOutput{Body: makeTicketResponse(ticket)}, nil
	})
	huma.Register[ticketUpdateInput, ticketOutput](api, huma.Operation{OperationID: contracts.RESTUpdateTicket, Method: http.MethodPatch, Path: "/tickets/{id}", Summary: "Update ticket", Tags: []string{"Tickets"}}, func(ctx context.Context, input *ticketUpdateInput) (*ticketOutput, error) {
		if resources == nil {
			return nil, huma.Error501NotImplemented("resource runtime is not configured")
		}
		id, err := parseRequiredUUID("id", input.ID)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		body := input.Body
		req := services.UpdateTicketRequest{TicketID: id, Tags: body.Tags, AcceptanceCriteria: body.AcceptanceCriteria, VerificationCommands: body.VerificationCommands, RelevantPaths: body.RelevantPaths, ActorType: body.ActorType, ActorID: body.ActorID}
		if body.Title != "" {
			req.Title = &body.Title
		}
		if body.Description != "" {
			req.Description = &body.Description
		}
		ticket, err := resources.UpdateTicket(ctx, req)
		if err != nil {
			return nil, resourceError(err, "update ticket failed")
		}
		return &ticketOutput{Body: makeTicketResponse(ticket)}, nil
	})
	huma.Register[decomposeInput, decomposeOutput](api, huma.Operation{OperationID: contracts.RESTDecomposeTicket, Method: http.MethodPost, Path: "/tickets/{id}/decompose", Summary: "Decompose ticket", Tags: []string{"Tickets"}}, func(ctx context.Context, input *decomposeInput) (*decomposeOutput, error) {
		if resources == nil {
			return nil, huma.Error501NotImplemented("resource runtime is not configured")
		}
		req, err := mapDecompose(input)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		result, err := resources.DecomposeTicket(ctx, req)
		if err != nil {
			return nil, resourceError(err, "decompose ticket failed")
		}
		out := &decomposeOutput{}
		out.Body.Children = makeTicketResponses(result.Children)
		return out, nil
	})
	huma.Register[artifactCreateInput, artifactOutput](api, huma.Operation{OperationID: contracts.RESTAttachArtifact, Method: http.MethodPost, Path: "/artifacts", Summary: "Register artifact", Tags: []string{"Artifacts"}}, func(ctx context.Context, input *artifactCreateInput) (*artifactOutput, error) {
		if resources == nil {
			return nil, huma.Error501NotImplemented("resource runtime is not configured")
		}
		req, err := mapArtifactCreate(input.Body)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		artifact, err := resources.RegisterArtifact(ctx, req)
		if err != nil {
			return nil, resourceError(err, "register artifact failed")
		}
		return &artifactOutput{Body: makeArtifactResponse(artifact)}, nil
	})
	huma.Register[artifactGetInput, artifactOutput](api, huma.Operation{OperationID: "get-artifact", Method: http.MethodGet, Path: "/artifacts/{id}", Summary: "Get artifact", Tags: []string{"Artifacts"}}, func(ctx context.Context, input *artifactGetInput) (*artifactOutput, error) {
		if resources == nil {
			return nil, huma.Error501NotImplemented("resource runtime is not configured")
		}
		id, err := parseRequiredUUID("id", input.ID)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		artifact, err := resources.GetArtifact(ctx, id)
		if err != nil {
			return nil, resourceError(err, "get artifact failed")
		}
		return &artifactOutput{Body: makeArtifactResponse(artifact)}, nil
	})
	huma.Register[artifactGetInput, artifactOutput](api, huma.Operation{OperationID: "delete-artifact", Method: http.MethodDelete, Path: "/artifacts/{id}", Summary: "Delete local artifact", Tags: []string{"Artifacts"}}, func(ctx context.Context, input *artifactGetInput) (*artifactOutput, error) {
		if resources == nil {
			return nil, huma.Error501NotImplemented("resource runtime is not configured")
		}
		id, err := parseRequiredUUID("id", input.ID)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		artifact, err := resources.DeleteLocalArtifact(ctx, id)
		if err != nil {
			return nil, resourceError(err, "delete artifact failed")
		}
		return &artifactOutput{Body: makeArtifactResponse(artifact)}, nil
	})
}

func mapTicketCreate(body ticketCreateBody) (services.CreateTicketRequest, error) {
	workspaceID, err := parseRequiredUUID("workspace_id", body.WorkspaceID)
	if err != nil {
		return services.CreateTicketRequest{}, err
	}
	projectID, err := parseRequiredUUID("project_id", body.ProjectID)
	if err != nil {
		return services.CreateTicketRequest{}, err
	}
	parentID, err := parseOptionalUUID(body.ParentID)
	if err != nil {
		return services.CreateTicketRequest{}, err
	}
	rootID, err := parseOptionalUUID(body.RootID)
	if err != nil {
		return services.CreateTicketRequest{}, err
	}
	dependencies := make([]pgtype.UUID, 0, len(body.Dependencies))
	for _, raw := range body.Dependencies {
		id, err := parseRequiredUUID("dependencies", raw)
		if err != nil {
			return services.CreateTicketRequest{}, err
		}
		dependencies = append(dependencies, id)
	}
	return services.CreateTicketRequest{WorkspaceID: workspaceID, ProjectID: projectID, ParentID: parentID, RootID: rootID, Title: body.Title, Description: body.Description, Type: body.Type, Status: body.Status, Priority: body.Priority, Tags: body.Tags, AcceptanceCriteria: body.AcceptanceCriteria, VerificationCommands: body.VerificationCommands, ExpectedArtifacts: body.ExpectedArtifacts, RelevantPaths: body.RelevantPaths, RequiredTools: body.RequiredTools, RequiredPermissions: body.RequiredPermissions, Environment: body.Environment, Input: body.Input, InputSchema: body.InputSchema, RequiredCapabilities: body.RequiredCapabilities, AllowedHarnesses: body.AllowedHarnesses, Dependencies: dependencies, CreatedBy: body.CreatedBy, CreatedByID: body.CreatedByID, CreationReason: body.CreationReason, Enqueue: body.Enqueue, CanEnqueue: true}, nil
}
func mapDecompose(input *decomposeInput) (services.DecomposeTicketRequest, error) {
	parentID, err := parseRequiredUUID("id", input.ID)
	if err != nil {
		return services.DecomposeTicketRequest{}, err
	}
	workspaceID, err := parseRequiredUUID("workspace_id", input.Body.WorkspaceID)
	if err != nil {
		return services.DecomposeTicketRequest{}, err
	}
	projectID, err := parseRequiredUUID("project_id", input.Body.ProjectID)
	if err != nil {
		return services.DecomposeTicketRequest{}, err
	}
	rootID, err := parseOptionalUUID(input.Body.RootID)
	if err != nil {
		return services.DecomposeTicketRequest{}, err
	}
	children := make([]services.DecomposeChildRequest, 0, len(input.Body.Children))
	for _, child := range input.Body.Children {
		children = append(children, services.DecomposeChildRequest{Key: child.Key, Title: child.Title, Description: child.Description, Type: child.Type, Priority: child.Priority, AcceptanceCriteria: child.AcceptanceCriteria, VerificationCommands: child.VerificationCommands, DependsOn: child.DependsOn})
	}
	return services.DecomposeTicketRequest{WorkspaceID: workspaceID, ProjectID: projectID, ParentID: parentID, RootID: rootID, Mode: input.Body.Mode, CanEnqueue: true, CreatedBy: input.Body.CreatedBy, CreatedByID: input.Body.CreatedByID, CreationReason: input.Body.CreationReason, Children: children}, nil
}
func mapArtifactCreate(body artifactCreateBody) (services.RegisterArtifactRequest, error) {
	workspaceID, err := parseRequiredUUID("workspace_id", body.WorkspaceID)
	if err != nil {
		return services.RegisterArtifactRequest{}, err
	}
	projectID, err := parseRequiredUUID("project_id", body.ProjectID)
	if err != nil {
		return services.RegisterArtifactRequest{}, err
	}
	ticketID, err := parseRequiredUUID("ticket_id", body.TicketID)
	if err != nil {
		return services.RegisterArtifactRequest{}, err
	}
	attemptID, err := parseOptionalUUID(body.AttemptID)
	if err != nil {
		return services.RegisterArtifactRequest{}, err
	}
	return services.RegisterArtifactRequest{WorkspaceID: workspaceID, ProjectID: projectID, TicketID: ticketID, AttemptID: attemptID, Type: body.Type, Role: body.Role, Name: body.Name, URL: body.URL, StorageBackend: body.StorageBackend, SizeBytes: body.SizeBytes, MimeType: body.MimeType, Metadata: body.Metadata}, nil
}
func resourceError(err error, message string) error {
	var validation services.ValidationError
	if errors.As(err, &validation) {
		return huma.Error400BadRequest(validation.Error())
	}
	if errors.Is(err, pgx.ErrNoRows) || errors.Is(err, services.ErrTicketNotFound) {
		return huma.Error404NotFound("resource not found", err)
	}
	if errors.Is(err, services.ErrTicketTransitionNotAllowed) || errors.Is(err, services.ErrArtifactDeleteUnsupported) || errors.Is(err, services.ErrAttemptNotRunning) || errors.Is(err, services.ErrIdempotencyConflict) || errors.Is(err, services.ErrNoClaimableTickets) {
		return huma.Error409Conflict(message, err)
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" {
			return huma.Error409Conflict(message, err)
		}
		if pgErr.Code == "23503" {
			return huma.Error404NotFound("referenced resource not found", err)
		}
	}
	return huma.Error500InternalServerError(message, err)
}

func makeTicketResponse(ticket db.Ticket) ticketResponse {
	return ticketResponse{ID: uuidText(ticket.ID), WorkspaceID: uuidText(ticket.WorkspaceID), ProjectID: uuidText(ticket.ProjectID), Title: ticket.Title, Description: ticket.Description, Type: ticket.Type, Status: ticket.Status, Priority: ticket.Priority, Tags: ticket.Tags, AcceptanceCriteria: ticket.AcceptanceCriteria, CreatedBy: ticket.CreatedBy}
}

func makeTicketResponses(tickets []db.Ticket) []ticketResponse {
	out := make([]ticketResponse, 0, len(tickets))
	for _, ticket := range tickets {
		out = append(out, makeTicketResponse(ticket))
	}
	return out
}

func makeArtifactResponse(artifact db.Artifact) artifactResponse {
	return artifactResponse{ID: uuidText(artifact.ID), WorkspaceID: uuidText(artifact.WorkspaceID), ProjectID: uuidText(artifact.ProjectID), TicketID: uuidText(artifact.TicketID), AttemptID: uuidText(artifact.AttemptID), Type: artifact.Type, Role: artifact.Role, Name: artifact.Name, URL: artifact.Url, StorageBackend: artifact.StorageBackend, SizeBytes: artifact.SizeBytes, MimeType: artifact.MimeType}
}
