package api

import (
	"context"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/vivek/agent-task-tracker/internal/contracts"
	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/services"
	"github.com/vivek/agent-task-tracker/internal/web"
)

type claimInput struct {
	IdempotencyKey string    `header:"Idempotency-Key"`
	Body           claimBody `json:"body"`
}
type claimBody struct {
	WorkspaceID  string   `json:"workspace_id"`
	ProjectID    string   `json:"project_id"`
	AgentID      string   `json:"agent_id"`
	Harness      string   `json:"harness"`
	Model        string   `json:"model,omitempty"`
	Type         string   `json:"type,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	LeaseSeconds int64    `json:"lease_seconds,omitempty"`
}
type attemptInput struct {
	ID string `path:"id"`
}
type heartbeatInput struct {
	ID   string `path:"id"`
	Body struct {
		LeaseSeconds int64 `json:"lease_seconds"`
	} `json:"body"`
}
type checkpointInput struct {
	ID   string `path:"id"`
	Body struct {
		Summary         string   `json:"summary"`
		ProgressPercent int32    `json:"progress_percent" minimum:"0" maximum:"100"`
		FilesTouched    []string `json:"files_touched,omitempty"`
		CommandsRun     []string `json:"commands_run,omitempty"`
		NextStep        string   `json:"next_step,omitempty"`
		Risk            string   `json:"risk,omitempty"`
	} `json:"body"`
}
type completeInput struct {
	ID   string `path:"id"`
	Body struct {
		Output       map[string]any `json:"output,omitempty"`
		OutputSchema string         `json:"output_schema,omitempty"`
		Metrics      *metricsBody   `json:"metrics,omitempty"`
	} `json:"body"`
}
type failInput struct {
	ID   string `path:"id"`
	Body struct {
		FailureReason   string         `json:"failure_reason"`
		FailureCategory string         `json:"failure_category"`
		Output          map[string]any `json:"output,omitempty"`
		Metrics         *metricsBody   `json:"metrics,omitempty"`
	} `json:"body"`
}
type blockInput struct {
	ID   string `path:"id"`
	Body struct {
		BlockerReason   string         `json:"blocker_reason"`
		FailureCategory string         `json:"failure_category"`
		Blocker         map[string]any `json:"blocker,omitempty"`
		Metrics         *metricsBody   `json:"metrics,omitempty"`
	} `json:"body"`
}
type cancelInput struct {
	ID   string `path:"id"`
	Body struct {
		Reason string `json:"reason"`
	} `json:"body"`
}
type metricsBody struct {
	TokensIn        int64   `json:"tokens_in,omitempty"`
	TokensOut       int64   `json:"tokens_out,omitempty"`
	CostUSD         float64 `json:"cost_usd,omitempty"`
	DurationSeconds float64 `json:"duration_seconds,omitempty"`
	RetryCount      int32   `json:"retry_count,omitempty"`
}
type attemptResponse struct {
	ID              string `json:"id"`
	TicketID        string `json:"ticket_id"`
	WorkspaceID     string `json:"workspace_id"`
	ProjectID       string `json:"project_id"`
	Status          string `json:"status"`
	ProgressPercent int32  `json:"progress_percent"`
	AgentID         string `json:"agent_id"`
	Harness         string `json:"harness"`
}
type attemptOutput struct {
	Body attemptResponse `json:"body"`
}
type claimOutput struct {
	Body struct {
		Ticket  ticketResponse  `json:"ticket"`
		Attempt attemptResponse `json:"attempt"`
	} `json:"body"`
}
type checkpointOutput struct {
	Body struct {
		CheckpointID    string `json:"checkpoint_id"`
		ProgressPercent int32  `json:"progress_percent"`
	} `json:"body"`
}
type transitionOutput struct {
	Body struct {
		AttemptID     string `json:"attempt_id"`
		TicketID      string `json:"ticket_id"`
		AttemptStatus string `json:"attempt_status"`
		TicketStatus  string `json:"ticket_status"`
	} `json:"body"`
}
type eventReadInput struct {
	ID     string `path:"id"`
	Cursor string `query:"cursor,omitempty"`
	Limit  int32  `query:"limit,omitempty"`
}
type eventOutput struct {
	Body services.ListEventsResult `json:"body"`
}

func registerLifecycleRoutes(api huma.API, rt web.Runtime) {
	r, _ := rt.(resourceRuntime)
	notConfigured := func() error { return huma.Error501NotImplemented("resource runtime is not configured") }
	huma.Register[claimInput, claimOutput](api, huma.Operation{OperationID: contracts.RESTClaimNextTicket, Method: http.MethodPost, Path: "/tickets/claim-next", Summary: "Claim next ticket", Tags: []string{"Execution"}}, func(ctx context.Context, in *claimInput) (*claimOutput, error) {
		if r == nil {
			return nil, notConfigured()
		}
		ws, err := parseRequiredUUID("workspace_id", in.Body.WorkspaceID)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		p, err := parseRequiredUUID("project_id", in.Body.ProjectID)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		lease := in.Body.LeaseSeconds
		if lease == 0 {
			lease = 300
		}
		result, err := r.ClaimNext(ctx, services.ClaimNextRequest{WorkspaceID: ws, ProjectID: p, AgentID: in.Body.AgentID, Harness: in.Body.Harness, Model: in.Body.Model, Type: in.Body.Type, Tags: in.Body.Tags, Capabilities: in.Body.Capabilities, Lease: time.Duration(lease) * time.Second, IdempotencyKey: in.IdempotencyKey})
		if err != nil {
			return nil, resourceError(err, "claim next ticket failed")
		}
		out := &claimOutput{}
		out.Body.Ticket = makeTicketResponse(result.Ticket)
		out.Body.Attempt = makeAttemptResponse(result.Attempt)
		return out, nil
	})
	huma.Register[attemptInput, attemptOutput](api, huma.Operation{OperationID: "get-attempt", Method: http.MethodGet, Path: "/attempts/{id}", Summary: "Get attempt", Tags: []string{"Execution"}}, func(ctx context.Context, in *attemptInput) (*attemptOutput, error) {
		if r == nil {
			return nil, notConfigured()
		}
		id, err := parseRequiredUUID("id", in.ID)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		a, err := r.GetAttempt(ctx, id)
		if err != nil {
			return nil, resourceError(err, "get attempt failed")
		}
		return &attemptOutput{Body: makeAttemptResponse(a)}, nil
	})
	huma.Register[heartbeatInput, attemptOutput](api, huma.Operation{OperationID: contracts.RESTHeartbeat, Method: http.MethodPost, Path: "/attempts/{id}/heartbeat", Summary: "Heartbeat attempt", Tags: []string{"Execution"}}, func(ctx context.Context, in *heartbeatInput) (*attemptOutput, error) {
		if r == nil {
			return nil, notConfigured()
		}
		id, err := parseRequiredUUID("id", in.ID)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		a, err := r.Heartbeat(ctx, services.HeartbeatRequest{AttemptID: id, Lease: time.Duration(in.Body.LeaseSeconds) * time.Second})
		if err != nil {
			return nil, resourceError(err, "heartbeat failed")
		}
		return &attemptOutput{Body: makeAttemptResponse(a)}, nil
	})
	huma.Register[checkpointInput, checkpointOutput](api, huma.Operation{OperationID: contracts.RESTCheckpoint, Method: http.MethodPost, Path: "/attempts/{id}/checkpoint", Summary: "Checkpoint attempt", Tags: []string{"Execution"}}, func(ctx context.Context, in *checkpointInput) (*checkpointOutput, error) {
		if r == nil {
			return nil, notConfigured()
		}
		id, err := parseRequiredUUID("id", in.ID)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		b := in.Body
		result, err := r.Checkpoint(ctx, services.CheckpointRequest{AttemptID: id, Summary: b.Summary, ProgressPercent: b.ProgressPercent, FilesTouched: b.FilesTouched, CommandsRun: b.CommandsRun, NextStep: b.NextStep, Risk: b.Risk})
		if err != nil {
			return nil, resourceError(err, "checkpoint failed")
		}
		out := &checkpointOutput{}
		out.Body.CheckpointID = uuidText(result.Checkpoint.ID)
		out.Body.ProgressPercent = result.ProgressPercent
		return out, nil
	})
	registerTransitionRoutes(api, r, notConfigured)
	registerEventReadRoutes(api, r, notConfigured)
}

func registerTransitionRoutes(api huma.API, r resourceRuntime, notConfigured func() error) {
	transition := func(result services.AttemptTransitionResult) *transitionOutput {
		out := &transitionOutput{}
		out.Body.AttemptID = uuidText(result.AttemptID)
		out.Body.TicketID = uuidText(result.TicketID)
		out.Body.AttemptStatus = result.AttemptStatus
		out.Body.TicketStatus = result.TicketStatus
		return out
	}
	huma.Register[completeInput, transitionOutput](api, huma.Operation{OperationID: contracts.RESTCompleteAttempt, Method: http.MethodPost, Path: "/attempts/{id}/complete", Summary: "Complete attempt", Tags: []string{"Execution"}}, func(ctx context.Context, in *completeInput) (*transitionOutput, error) {
		if r == nil {
			return nil, notConfigured()
		}
		id, err := parseRequiredUUID("id", in.ID)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		result, err := r.Complete(ctx, services.CompleteAttemptRequest{AttemptID: id, Output: in.Body.Output, OutputSchema: in.Body.OutputSchema, Metrics: mapMetrics(in.Body.Metrics)})
		if err != nil {
			return nil, resourceError(err, "complete attempt failed")
		}
		return transition(result), nil
	})
	huma.Register[failInput, transitionOutput](api, huma.Operation{OperationID: contracts.RESTFailAttempt, Method: http.MethodPost, Path: "/attempts/{id}/fail", Summary: "Fail attempt", Tags: []string{"Execution"}}, func(ctx context.Context, in *failInput) (*transitionOutput, error) {
		if r == nil {
			return nil, notConfigured()
		}
		id, err := parseRequiredUUID("id", in.ID)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		result, err := r.Fail(ctx, services.FailAttemptRequest{AttemptID: id, FailureReason: in.Body.FailureReason, FailureCategory: in.Body.FailureCategory, Output: in.Body.Output, Metrics: mapMetrics(in.Body.Metrics)})
		if err != nil {
			return nil, resourceError(err, "fail attempt failed")
		}
		return transition(result), nil
	})
	huma.Register[blockInput, transitionOutput](api, huma.Operation{OperationID: contracts.RESTBlockAttempt, Method: http.MethodPost, Path: "/attempts/{id}/block", Summary: "Block attempt", Tags: []string{"Execution"}}, func(ctx context.Context, in *blockInput) (*transitionOutput, error) {
		if r == nil {
			return nil, notConfigured()
		}
		id, err := parseRequiredUUID("id", in.ID)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		result, err := r.Block(ctx, services.BlockAttemptRequest{AttemptID: id, BlockerReason: in.Body.BlockerReason, FailureCategory: in.Body.FailureCategory, Blocker: in.Body.Blocker, Metrics: mapMetrics(in.Body.Metrics)})
		if err != nil {
			return nil, resourceError(err, "block attempt failed")
		}
		return transition(result), nil
	})
	huma.Register[cancelInput, transitionOutput](api, huma.Operation{OperationID: "cancel-attempt", Method: http.MethodPost, Path: "/attempts/{id}/cancel", Summary: "Cancel attempt", Tags: []string{"Execution"}}, func(ctx context.Context, in *cancelInput) (*transitionOutput, error) {
		if r == nil {
			return nil, notConfigured()
		}
		id, err := parseRequiredUUID("id", in.ID)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		result, err := r.Cancel(ctx, services.CancelAttemptRequest{AttemptID: id, Reason: in.Body.Reason})
		if err != nil {
			return nil, resourceError(err, "cancel attempt failed")
		}
		return transition(result), nil
	})
}
func registerEventReadRoutes(api huma.API, r resourceRuntime, notConfigured func() error) {
	register := func(path, operation string, attempt bool) {
		huma.Register[eventReadInput, eventOutput](api, huma.Operation{OperationID: operation, Method: http.MethodGet, Path: path, Summary: "List execution events", Tags: []string{"Execution"}}, func(ctx context.Context, in *eventReadInput) (*eventOutput, error) {
			if r == nil {
				return nil, notConfigured()
			}
			id, err := parseRequiredUUID("id", in.ID)
			if err != nil {
				return nil, huma.Error400BadRequest(err.Error())
			}
			req := services.ListEventsRequest{Cursor: in.Cursor, Limit: in.Limit}
			if attempt {
				req.AttemptID = id
			} else {
				req.TicketID = id
			}
			result, err := r.ListEvents(ctx, req)
			if err != nil {
				return nil, resourceError(err, "list events failed")
			}
			return &eventOutput{Body: result}, nil
		})
	}
	register("/tickets/{id}/events", "list-ticket-events", false)
	register("/attempts/{id}/events", "list-attempt-events", true)
}
func mapMetrics(body *metricsBody) *services.AttemptMetricsRequest {
	if body == nil {
		return nil
	}
	return &services.AttemptMetricsRequest{TokensIn: body.TokensIn, TokensOut: body.TokensOut, CostUSD: body.CostUSD, DurationSeconds: body.DurationSeconds, RetryCount: body.RetryCount}
}
func makeAttemptResponse(a db.Attempt) attemptResponse {
	return attemptResponse{ID: uuidText(a.ID), TicketID: uuidText(a.TicketID), WorkspaceID: uuidText(a.WorkspaceID), ProjectID: uuidText(a.ProjectID), Status: a.Status, ProgressPercent: a.ProgressPercent, AgentID: a.AgentID, Harness: a.Harness}
}
