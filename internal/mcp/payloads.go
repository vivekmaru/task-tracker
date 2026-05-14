package mcp

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/services"
)

type createTicketInput struct {
	WorkspaceID          string         `json:"workspace_id"`
	ProjectID            string         `json:"project_id"`
	ParentID             string         `json:"parent_id"`
	RootID               string         `json:"root_id"`
	SourceAttemptID      string         `json:"source_attempt_id"`
	SourceArtifactID     string         `json:"source_artifact_id"`
	Title                string         `json:"title"`
	Description          string         `json:"description"`
	Type                 string         `json:"type"`
	Status               string         `json:"status"`
	Priority             *int32         `json:"priority"`
	Tags                 []string       `json:"tags"`
	AcceptanceCriteria   []string       `json:"acceptance_criteria"`
	VerificationCommands []string       `json:"verification_commands"`
	ExpectedArtifacts    []string       `json:"expected_artifacts"`
	RelevantPaths        []string       `json:"relevant_paths"`
	RequiredTools        []string       `json:"required_tools"`
	RequiredPermissions  []string       `json:"required_permissions"`
	Environment          map[string]any `json:"environment"`
	Input                map[string]any `json:"input"`
	InputSchema          string         `json:"input_schema"`
	RequiredCapabilities []string       `json:"required_capabilities"`
	AllowedHarnesses     []string       `json:"allowed_harnesses"`
	CreatedBy            string         `json:"created_by"`
	CreatedByID          string         `json:"created_by_id"`
	CreationReason       string         `json:"creation_reason"`
	Enqueue              bool           `json:"enqueue"`
	CanEnqueue           bool           `json:"can_enqueue"`
}

func (p createTicketInput) request() services.CreateTicketRequest {
	return services.CreateTicketRequest{
		WorkspaceID:          mustUUID(p.WorkspaceID),
		ProjectID:            mustUUID(p.ProjectID),
		ParentID:             mustUUID(p.ParentID),
		RootID:               mustUUID(p.RootID),
		SourceAttemptID:      mustUUID(p.SourceAttemptID),
		SourceArtifactID:     mustUUID(p.SourceArtifactID),
		Title:                p.Title,
		Description:          p.Description,
		Type:                 p.Type,
		Status:               p.Status,
		Priority:             p.Priority,
		Tags:                 p.Tags,
		AcceptanceCriteria:   p.AcceptanceCriteria,
		VerificationCommands: p.VerificationCommands,
		ExpectedArtifacts:    p.ExpectedArtifacts,
		RelevantPaths:        p.RelevantPaths,
		RequiredTools:        p.RequiredTools,
		RequiredPermissions:  p.RequiredPermissions,
		Environment:          p.Environment,
		Input:                p.Input,
		InputSchema:          p.InputSchema,
		RequiredCapabilities: p.RequiredCapabilities,
		AllowedHarnesses:     p.AllowedHarnesses,
		CreatedBy:            p.CreatedBy,
		CreatedByID:          p.CreatedByID,
		CreationReason:       p.CreationReason,
		Enqueue:              p.Enqueue,
		CanEnqueue:           p.CanEnqueue,
	}
}

type createFromAttemptInput struct {
	WorkspaceID          string         `json:"workspace_id"`
	ProjectID            string         `json:"project_id"`
	AttemptID            string         `json:"attempt_id"`
	SourceArtifactID     string         `json:"source_artifact_id"`
	TemplateKind         string         `json:"template_kind"`
	Type                 string         `json:"type"`
	Title                string         `json:"title"`
	Description          string         `json:"description"`
	Priority             *int32         `json:"priority"`
	Tags                 []string       `json:"tags"`
	AcceptanceCriteria   []string       `json:"acceptance_criteria"`
	VerificationCommands []string       `json:"verification_commands"`
	ExpectedArtifacts    []string       `json:"expected_artifacts"`
	RelevantPaths        []string       `json:"relevant_paths"`
	RequiredTools        []string       `json:"required_tools"`
	RequiredPermissions  []string       `json:"required_permissions"`
	RequiredCapabilities []string       `json:"required_capabilities"`
	AllowedHarnesses     []string       `json:"allowed_harnesses"`
	Environment          map[string]any `json:"environment"`
	Input                map[string]any `json:"input"`
	CreatedByID          string         `json:"created_by_id"`
	CreationReason       string         `json:"creation_reason"`
	Enqueue              bool           `json:"enqueue"`
	CanEnqueue           bool           `json:"can_enqueue"`
}

func (p createFromAttemptInput) request() services.CreateTicketFromAttemptRequest {
	templateKind := p.TemplateKind
	if templateKind == "" {
		templateKind = p.Type
	}
	return services.CreateTicketFromAttemptRequest{
		WorkspaceID:          mustUUID(p.WorkspaceID),
		ProjectID:            mustUUID(p.ProjectID),
		SourceAttemptID:      mustUUID(p.AttemptID),
		SourceArtifactID:     mustUUID(p.SourceArtifactID),
		TemplateKind:         templateKind,
		Title:                p.Title,
		Description:          p.Description,
		Priority:             p.Priority,
		Tags:                 p.Tags,
		AcceptanceCriteria:   p.AcceptanceCriteria,
		VerificationCommands: p.VerificationCommands,
		ExpectedArtifacts:    p.ExpectedArtifacts,
		RelevantPaths:        p.RelevantPaths,
		RequiredTools:        p.RequiredTools,
		RequiredPermissions:  p.RequiredPermissions,
		RequiredCapabilities: p.RequiredCapabilities,
		AllowedHarnesses:     p.AllowedHarnesses,
		Environment:          p.Environment,
		Input:                p.Input,
		CreatedByID:          p.CreatedByID,
		CreationReason:       p.CreationReason,
		Enqueue:              p.Enqueue,
		CanEnqueue:           p.CanEnqueue,
	}
}

type claimNextInput struct {
	WorkspaceID    string   `json:"workspace_id"`
	ProjectID      string   `json:"project_id"`
	Type           string   `json:"type"`
	Tags           []string `json:"tags"`
	Harness        string   `json:"harness"`
	Capabilities   []string `json:"capabilities"`
	AgentID        string   `json:"agent_id"`
	Model          string   `json:"model"`
	LeaseSeconds   int64    `json:"lease_seconds"`
	IdempotencyKey string   `json:"idempotency_key"`
	IdempotencyTTL int64    `json:"idempotency_ttl"`
}

func (p claimNextInput) request() services.ClaimNextRequest {
	return services.ClaimNextRequest{
		WorkspaceID:    mustUUID(p.WorkspaceID),
		ProjectID:      mustUUID(p.ProjectID),
		Type:           p.Type,
		Tags:           p.Tags,
		Harness:        p.Harness,
		Capabilities:   p.Capabilities,
		AgentID:        p.AgentID,
		Model:          p.Model,
		Lease:          time.Duration(p.LeaseSeconds) * time.Second,
		IdempotencyKey: p.IdempotencyKey,
		IdempotencyTTL: time.Duration(p.IdempotencyTTL) * time.Second,
	}
}

type attemptLeaseInput struct {
	AttemptID    string `json:"attempt_id"`
	LeaseSeconds int64  `json:"lease_seconds"`
}

type checkpointInput struct {
	AttemptID       string   `json:"attempt_id"`
	Summary         string   `json:"summary"`
	ProgressPercent int      `json:"progress_percent"`
	FilesTouched    []string `json:"files_touched"`
	CommandsRun     []string `json:"commands_run"`
	NextStep        string   `json:"next_step"`
	Risk            string   `json:"risk"`
}

type updateTicketInput struct {
	TicketID string            `json:"ticket_id"`
	Patch    updateTicketPatch `json:"patch"`
	ActorID  string            `json:"actor_id"`
}

type updateTicketPatch struct {
	Title                *string   `json:"title"`
	Description          *string   `json:"description"`
	Tags                 *[]string `json:"tags"`
	AcceptanceCriteria   *[]string `json:"acceptance_criteria"`
	VerificationCommands *[]string `json:"verification_commands"`
	RelevantPaths        *[]string `json:"relevant_paths"`
}

func (p updateTicketInput) request() services.UpdateTicketRequest {
	return services.UpdateTicketRequest{
		TicketID:             mustUUID(p.TicketID),
		Title:                p.Patch.Title,
		Description:          p.Patch.Description,
		Tags:                 p.Patch.Tags,
		AcceptanceCriteria:   p.Patch.AcceptanceCriteria,
		VerificationCommands: p.Patch.VerificationCommands,
		RelevantPaths:        p.Patch.RelevantPaths,
		ActorType:            services.ActorAgent,
		ActorID:              p.ActorID,
	}
}

type completeInput struct {
	AttemptID    string         `json:"attempt_id"`
	Output       map[string]any `json:"output"`
	OutputSchema string         `json:"output_schema"`
}

type failInput struct {
	AttemptID       string         `json:"attempt_id"`
	FailureReason   string         `json:"failure_reason"`
	FailureCategory string         `json:"failure_category"`
	Output          map[string]any `json:"output"`
}

type blockInput struct {
	AttemptID       string         `json:"attempt_id"`
	BlockerReason   string         `json:"blocker_reason"`
	FailureCategory string         `json:"failure_category"`
	Blocker         map[string]any `json:"blocker"`
}

type listTicketsInput struct {
	WorkspaceID string `json:"workspace_id"`
	ProjectID   string `json:"project_id"`
	Status      string `json:"status"`
	Type        string `json:"type"`
	Offset      int    `json:"offset"`
	Limit       int    `json:"limit"`
}

type getTicketInput struct {
	TicketID string `json:"ticket_id"`
}

type attachArtifactInput struct {
	WorkspaceID    string         `json:"workspace_id"`
	ProjectID      string         `json:"project_id"`
	TicketID       string         `json:"ticket_id"`
	AttemptID      string         `json:"attempt_id"`
	Type           string         `json:"type"`
	Role           string         `json:"role"`
	Name           string         `json:"name"`
	URL            string         `json:"url"`
	StorageBackend string         `json:"storage_backend"`
	SizeBytes      int64          `json:"size_bytes"`
	MimeType       string         `json:"mime_type"`
	Metadata       map[string]any `json:"metadata"`
}

func (p attachArtifactInput) request() services.RegisterArtifactRequest {
	return services.RegisterArtifactRequest{
		WorkspaceID:    mustUUID(p.WorkspaceID),
		ProjectID:      mustUUID(p.ProjectID),
		TicketID:       mustUUID(p.TicketID),
		AttemptID:      mustUUID(p.AttemptID),
		Type:           p.Type,
		Role:           p.Role,
		Name:           p.Name,
		URL:            p.URL,
		StorageBackend: p.StorageBackend,
		SizeBytes:      p.SizeBytes,
		MimeType:       p.MimeType,
		Metadata:       p.Metadata,
	}
}

type decomposeInput struct {
	TicketID       string                `json:"ticket_id"`
	WorkspaceID    string                `json:"workspace_id"`
	ProjectID      string                `json:"project_id"`
	RootID         string                `json:"root_id"`
	Mode           string                `json:"mode"`
	CanEnqueue     bool                  `json:"can_enqueue"`
	CreatedBy      string                `json:"created_by"`
	CreatedByID    string                `json:"created_by_id"`
	CreationReason string                `json:"creation_reason"`
	Children       []decomposeChildInput `json:"children"`
}

type decomposeChildInput struct {
	Key                  string         `json:"key"`
	Title                string         `json:"title"`
	Description          string         `json:"description"`
	Type                 string         `json:"type"`
	Priority             *int32         `json:"priority"`
	Tags                 []string       `json:"tags"`
	AcceptanceCriteria   []string       `json:"acceptance_criteria"`
	VerificationCommands []string       `json:"verification_commands"`
	ExpectedArtifacts    []string       `json:"expected_artifacts"`
	RelevantPaths        []string       `json:"relevant_paths"`
	RequiredTools        []string       `json:"required_tools"`
	RequiredPermissions  []string       `json:"required_permissions"`
	RequiredCapabilities []string       `json:"required_capabilities"`
	AllowedHarnesses     []string       `json:"allowed_harnesses"`
	Environment          map[string]any `json:"environment"`
	Input                map[string]any `json:"input"`
	DependsOn            []string       `json:"depends_on"`
}

func (p decomposeInput) request() services.DecomposeTicketRequest {
	children := make([]services.DecomposeChildRequest, 0, len(p.Children))
	for _, child := range p.Children {
		children = append(children, services.DecomposeChildRequest{
			Key:                  child.Key,
			Title:                child.Title,
			Description:          child.Description,
			Type:                 child.Type,
			Priority:             child.Priority,
			Tags:                 child.Tags,
			AcceptanceCriteria:   child.AcceptanceCriteria,
			VerificationCommands: child.VerificationCommands,
			ExpectedArtifacts:    child.ExpectedArtifacts,
			RelevantPaths:        child.RelevantPaths,
			RequiredTools:        child.RequiredTools,
			RequiredPermissions:  child.RequiredPermissions,
			RequiredCapabilities: child.RequiredCapabilities,
			AllowedHarnesses:     child.AllowedHarnesses,
			Environment:          child.Environment,
			Input:                child.Input,
			DependsOn:            child.DependsOn,
		})
	}
	return services.DecomposeTicketRequest{
		WorkspaceID:    mustUUID(p.WorkspaceID),
		ProjectID:      mustUUID(p.ProjectID),
		ParentID:       mustUUID(p.TicketID),
		RootID:         mustUUID(p.RootID),
		Mode:           p.Mode,
		CanEnqueue:     p.CanEnqueue,
		CreatedBy:      p.CreatedBy,
		CreatedByID:    p.CreatedByID,
		CreationReason: p.CreationReason,
		Children:       children,
	}
}

type registerCapabilitiesInput struct {
	WorkspaceID    string         `json:"workspace_id"`
	ProjectID      string         `json:"project_id"`
	AgentID        string         `json:"agent_id"`
	Harness        string         `json:"harness"`
	Model          string         `json:"model"`
	Transports     []string       `json:"transports"`
	Capabilities   []string       `json:"capabilities"`
	ToolNames      []string       `json:"tool_names"`
	ArtifactRoles  []string       `json:"artifact_roles"`
	PreferredClaim map[string]any `json:"preferred_claim"`
	Metadata       map[string]any `json:"metadata"`
}

func (p registerCapabilitiesInput) request() services.RegisterCapabilitiesRequest {
	return services.RegisterCapabilitiesRequest{
		WorkspaceID:    mustUUID(p.WorkspaceID),
		ProjectID:      mustUUID(p.ProjectID),
		AgentID:        p.AgentID,
		Harness:        p.Harness,
		Model:          p.Model,
		Transports:     p.Transports,
		Capabilities:   p.Capabilities,
		ToolNames:      p.ToolNames,
		ArtifactRoles:  p.ArtifactRoles,
		PreferredClaim: p.PreferredClaim,
		Metadata:       p.Metadata,
	}
}

func mustUUID(value string) pgtype.UUID {
	var id pgtype.UUID
	if value == "" {
		return id
	}
	_ = id.Scan(value)
	return id
}

func uuidText(id pgtype.UUID) string {
	value, err := id.Value()
	if err != nil {
		return ""
	}
	text, _ := value.(string)
	return text
}

func ticketPayload(ticket db.Ticket) map[string]any {
	return map[string]any{
		"id":           uuidText(ticket.ID),
		"title":        ticket.Title,
		"type":         ticket.Type,
		"status":       ticket.Status,
		"priority":     ticket.Priority,
		"project_id":   uuidText(ticket.ProjectID),
		"workspace_id": uuidText(ticket.WorkspaceID),
	}
}

func attemptPayload(attempt db.Attempt) map[string]any {
	return map[string]any{
		"id":        uuidText(attempt.ID),
		"ticket_id": uuidText(attempt.TicketID),
		"agent_id":  attempt.AgentID,
		"harness":   attempt.Harness,
		"model":     attempt.Model,
		"status":    attempt.Status,
	}
}

func transitionPayload(result services.AttemptTransitionResult) map[string]any {
	return map[string]any{
		"attempt_id":     uuidText(result.AttemptID),
		"ticket_id":      uuidText(result.TicketID),
		"attempt_status": result.AttemptStatus,
		"ticket_status":  result.TicketStatus,
	}
}

func artifactPayload(artifact db.Artifact) map[string]any {
	return map[string]any{
		"id":              uuidText(artifact.ID),
		"ticket_id":       uuidText(artifact.TicketID),
		"attempt_id":      uuidText(artifact.AttemptID),
		"type":            artifact.Type,
		"role":            artifact.Role,
		"name":            artifact.Name,
		"url":             artifact.Url,
		"storage_backend": artifact.StorageBackend,
		"size_bytes":      artifact.SizeBytes,
		"mime_type":       artifact.MimeType,
	}
}
