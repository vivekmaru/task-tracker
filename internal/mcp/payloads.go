package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
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
	RetryPolicy          map[string]any `json:"retry_policy"`
	Dependencies         []string       `json:"dependencies"`
	CreatedBy            string         `json:"created_by"`
	CreatedByID          string         `json:"created_by_id"`
	CreationReason       string         `json:"creation_reason"`
	Enqueue              bool           `json:"enqueue"`
}

func (p createTicketInput) request() (services.CreateTicketRequest, error) {
	workspaceID, err := requiredUUIDField("workspace_id", p.WorkspaceID)
	if err != nil {
		return services.CreateTicketRequest{}, err
	}
	projectID, err := requiredUUIDField("project_id", p.ProjectID)
	if err != nil {
		return services.CreateTicketRequest{}, err
	}
	parentID, err := optionalUUIDField("parent_id", p.ParentID)
	if err != nil {
		return services.CreateTicketRequest{}, err
	}
	rootID, err := optionalUUIDField("root_id", p.RootID)
	if err != nil {
		return services.CreateTicketRequest{}, err
	}
	sourceAttemptID, err := optionalUUIDField("source_attempt_id", p.SourceAttemptID)
	if err != nil {
		return services.CreateTicketRequest{}, err
	}
	sourceArtifactID, err := optionalUUIDField("source_artifact_id", p.SourceArtifactID)
	if err != nil {
		return services.CreateTicketRequest{}, err
	}
	retryPolicy, err := optionalJSONField("retry_policy", p.RetryPolicy)
	if err != nil {
		return services.CreateTicketRequest{}, err
	}
	dependencies, err := optionalUUIDListField("dependencies", p.Dependencies)
	if err != nil {
		return services.CreateTicketRequest{}, err
	}
	return services.CreateTicketRequest{
		WorkspaceID:          workspaceID,
		ProjectID:            projectID,
		ParentID:             parentID,
		RootID:               rootID,
		SourceAttemptID:      sourceAttemptID,
		SourceArtifactID:     sourceArtifactID,
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
		RetryPolicy:          retryPolicy,
		Dependencies:         dependencies,
		CreatedBy:            p.CreatedBy,
		CreatedByID:          p.CreatedByID,
		CreationReason:       p.CreationReason,
		Enqueue:              p.Enqueue,
	}, nil
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
}

func (p createFromAttemptInput) request() (services.CreateTicketFromAttemptRequest, error) {
	if strings.TrimSpace(p.TemplateKind) != "" {
		return services.CreateTicketFromAttemptRequest{}, services.ValidationError{Problems: []string{"template_kind is not accepted from MCP callers; use type"}}
	}
	if strings.TrimSpace(p.Type) == "" {
		return services.CreateTicketFromAttemptRequest{}, services.ValidationError{Problems: []string{"type is required"}}
	}
	workspaceID, err := requiredUUIDField("workspace_id", p.WorkspaceID)
	if err != nil {
		return services.CreateTicketFromAttemptRequest{}, err
	}
	projectID, err := requiredUUIDField("project_id", p.ProjectID)
	if err != nil {
		return services.CreateTicketFromAttemptRequest{}, err
	}
	attemptID, err := requiredUUIDField("attempt_id", p.AttemptID)
	if err != nil {
		return services.CreateTicketFromAttemptRequest{}, err
	}
	sourceArtifactID, err := optionalUUIDField("source_artifact_id", p.SourceArtifactID)
	if err != nil {
		return services.CreateTicketFromAttemptRequest{}, err
	}
	return services.CreateTicketFromAttemptRequest{
		WorkspaceID:          workspaceID,
		ProjectID:            projectID,
		SourceAttemptID:      attemptID,
		SourceArtifactID:     sourceArtifactID,
		TemplateKind:         p.Type,
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
	}, nil
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

func (p claimNextInput) request() (services.ClaimNextRequest, error) {
	workspaceID, err := requiredUUIDField("workspace_id", p.WorkspaceID)
	if err != nil {
		return services.ClaimNextRequest{}, err
	}
	projectID, err := requiredUUIDField("project_id", p.ProjectID)
	if err != nil {
		return services.ClaimNextRequest{}, err
	}
	return services.ClaimNextRequest{
		WorkspaceID:    workspaceID,
		ProjectID:      projectID,
		Type:           p.Type,
		Tags:           p.Tags,
		Harness:        p.Harness,
		Capabilities:   p.Capabilities,
		AgentID:        p.AgentID,
		Model:          p.Model,
		Lease:          time.Duration(p.LeaseSeconds) * time.Second,
		IdempotencyKey: p.IdempotencyKey,
		IdempotencyTTL: time.Duration(p.IdempotencyTTL) * time.Second,
	}, nil
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

type ticketTransitionInput struct {
	TicketID string `json:"ticket_id"`
	ActorID  string `json:"actor_id"`
	Reason   string `json:"reason"`
}

func (p ticketTransitionInput) request() (services.TicketTransitionRequest, error) {
	ticketID, err := requiredUUIDField("ticket_id", p.TicketID)
	if err != nil {
		return services.TicketTransitionRequest{}, err
	}
	return services.TicketTransitionRequest{
		TicketID:  ticketID,
		ActorType: services.ActorAgent,
		ActorID:   p.ActorID,
		Reason:    p.Reason,
	}, nil
}

type reviewTicketInput struct {
	TicketID string `json:"ticket_id"`
	Decision string `json:"decision"`
	ActorID  string `json:"actor_id"`
	Reason   string `json:"reason"`
}

func (p reviewTicketInput) request() (services.ReviewTicketRequest, error) {
	ticketID, err := requiredUUIDField("ticket_id", p.TicketID)
	if err != nil {
		return services.ReviewTicketRequest{}, err
	}
	return services.ReviewTicketRequest{
		TicketID:  ticketID,
		Decision:  p.Decision,
		ActorType: services.ActorAgent,
		ActorID:   p.ActorID,
		Reason:    p.Reason,
	}, nil
}

func (p updateTicketInput) request() (services.UpdateTicketRequest, error) {
	ticketID, err := requiredUUIDField("ticket_id", p.TicketID)
	if err != nil {
		return services.UpdateTicketRequest{}, err
	}
	return services.UpdateTicketRequest{
		TicketID:             ticketID,
		Title:                p.Patch.Title,
		Description:          p.Patch.Description,
		Tags:                 p.Patch.Tags,
		AcceptanceCriteria:   p.Patch.AcceptanceCriteria,
		VerificationCommands: p.Patch.VerificationCommands,
		RelevantPaths:        p.Patch.RelevantPaths,
		ActorType:            services.ActorAgent,
		ActorID:              p.ActorID,
	}, nil
}

type completeInput struct {
	AttemptID    string         `json:"attempt_id"`
	Output       map[string]any `json:"output"`
	OutputSchema string         `json:"output_schema"`
	Metrics      metricsInput   `json:"metrics"`
}

type failInput struct {
	AttemptID       string         `json:"attempt_id"`
	FailureReason   string         `json:"failure_reason"`
	FailureCategory string         `json:"failure_category"`
	Output          map[string]any `json:"output"`
	Metrics         metricsInput   `json:"metrics"`
}

type blockInput struct {
	AttemptID       string         `json:"attempt_id"`
	BlockerReason   string         `json:"blocker_reason"`
	FailureCategory string         `json:"failure_category"`
	Blocker         map[string]any `json:"blocker"`
	Metrics         metricsInput   `json:"metrics"`
}

type metricsInput struct {
	TokensIn        int64   `json:"tokens_in"`
	TokensOut       int64   `json:"tokens_out"`
	CostUSD         float64 `json:"cost_usd"`
	DurationSeconds float64 `json:"duration_seconds"`
	RetryCount      int32   `json:"retry_count"`
}

func (p metricsInput) request() *services.AttemptMetricsRequest {
	if p.TokensIn == 0 && p.TokensOut == 0 && p.CostUSD == 0 && p.DurationSeconds == 0 && p.RetryCount == 0 {
		return nil
	}
	return &services.AttemptMetricsRequest{
		TokensIn:        p.TokensIn,
		TokensOut:       p.TokensOut,
		CostUSD:         p.CostUSD,
		DurationSeconds: p.DurationSeconds,
		RetryCount:      p.RetryCount,
	}
}

type analyticsInput struct {
	WorkspaceID string `json:"workspace_id"`
	ProjectID   string `json:"project_id"`
}

func (p analyticsInput) filter() (services.AnalyticsFilter, error) {
	workspaceID, err := optionalUUIDField("workspace_id", p.WorkspaceID)
	if err != nil {
		return services.AnalyticsFilter{}, err
	}
	projectID, err := optionalUUIDField("project_id", p.ProjectID)
	if err != nil {
		return services.AnalyticsFilter{}, err
	}
	return services.AnalyticsFilter{WorkspaceID: workspaceID, ProjectID: projectID}, nil
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

func (p attachArtifactInput) request() (services.RegisterArtifactRequest, error) {
	workspaceID, err := requiredUUIDField("workspace_id", p.WorkspaceID)
	if err != nil {
		return services.RegisterArtifactRequest{}, err
	}
	projectID, err := requiredUUIDField("project_id", p.ProjectID)
	if err != nil {
		return services.RegisterArtifactRequest{}, err
	}
	ticketID, err := requiredUUIDField("ticket_id", p.TicketID)
	if err != nil {
		return services.RegisterArtifactRequest{}, err
	}
	attemptID, err := optionalUUIDField("attempt_id", p.AttemptID)
	if err != nil {
		return services.RegisterArtifactRequest{}, err
	}
	return services.RegisterArtifactRequest{
		WorkspaceID:    workspaceID,
		ProjectID:      projectID,
		TicketID:       ticketID,
		AttemptID:      attemptID,
		Type:           p.Type,
		Role:           p.Role,
		Name:           p.Name,
		URL:            p.URL,
		StorageBackend: p.StorageBackend,
		SizeBytes:      p.SizeBytes,
		MimeType:       p.MimeType,
		Metadata:       p.Metadata,
	}, nil
}

type decomposeInput struct {
	TicketID       string                `json:"ticket_id"`
	WorkspaceID    string                `json:"workspace_id"`
	ProjectID      string                `json:"project_id"`
	RootID         string                `json:"root_id"`
	Mode           string                `json:"mode"`
	CanEnqueue     bool                  `json:"can_enqueue"`
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

func (p decomposeInput) request() (services.DecomposeTicketRequest, error) {
	if p.CanEnqueue {
		return services.DecomposeTicketRequest{}, services.ValidationError{Problems: []string{"can_enqueue is not accepted from MCP callers"}}
	}
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
	workspaceID, err := requiredUUIDField("workspace_id", p.WorkspaceID)
	if err != nil {
		return services.DecomposeTicketRequest{}, err
	}
	projectID, err := requiredUUIDField("project_id", p.ProjectID)
	if err != nil {
		return services.DecomposeTicketRequest{}, err
	}
	parentID, err := requiredUUIDField("ticket_id", p.TicketID)
	if err != nil {
		return services.DecomposeTicketRequest{}, err
	}
	rootID, err := optionalUUIDField("root_id", p.RootID)
	if err != nil {
		return services.DecomposeTicketRequest{}, err
	}
	return services.DecomposeTicketRequest{
		WorkspaceID:    workspaceID,
		ProjectID:      projectID,
		ParentID:       parentID,
		RootID:         rootID,
		Mode:           p.Mode,
		CanEnqueue:     p.Mode == services.DecomposeModeCreate,
		CreatedBy:      services.ActorAgent,
		CreatedByID:    p.CreatedByID,
		CreationReason: p.CreationReason,
		Children:       children,
	}, nil
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

func (p registerCapabilitiesInput) request() (services.RegisterCapabilitiesRequest, error) {
	workspaceID, err := requiredUUIDField("workspace_id", p.WorkspaceID)
	if err != nil {
		return services.RegisterCapabilitiesRequest{}, err
	}
	projectID, err := requiredUUIDField("project_id", p.ProjectID)
	if err != nil {
		return services.RegisterCapabilitiesRequest{}, err
	}
	return services.RegisterCapabilitiesRequest{
		WorkspaceID:    workspaceID,
		ProjectID:      projectID,
		AgentID:        p.AgentID,
		Harness:        p.Harness,
		Model:          p.Model,
		Transports:     p.Transports,
		Capabilities:   p.Capabilities,
		ToolNames:      p.ToolNames,
		ArtifactRoles:  p.ArtifactRoles,
		PreferredClaim: p.PreferredClaim,
		Metadata:       p.Metadata,
	}, nil
}

func requiredUUIDField(name, value string) (pgtype.UUID, error) {
	if value == "" {
		return pgtype.UUID{}, services.ValidationError{Problems: []string{fmt.Sprintf("%s is required", name)}}
	}
	return parseUUIDField(name, value)
}

func optionalUUIDField(name, value string) (pgtype.UUID, error) {
	if value == "" {
		return pgtype.UUID{}, nil
	}
	return parseUUIDField(name, value)
}

func optionalUUIDListField(name string, values []string) ([]pgtype.UUID, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]pgtype.UUID, 0, len(values))
	for i, value := range values {
		id, err := requiredUUIDField(fmt.Sprintf("%s[%d]", name, i), value)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

func parseUUIDField(name, value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	if err := id.Scan(value); err != nil {
		return pgtype.UUID{}, services.ValidationError{Problems: []string{fmt.Sprintf("%s must be a valid UUID", name)}}
	}
	return id, nil
}

func optionalJSONField(name string, value map[string]any) ([]byte, error) {
	if len(value) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%s must be valid JSON: %w", name, err)
	}
	return data, nil
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

func claimContextPayload(bundle services.ClaimContextBundle) map[string]any {
	priorAttempts := make([]map[string]any, 0, len(bundle.PriorAttempts))
	for _, attempt := range bundle.PriorAttempts {
		priorAttempts = append(priorAttempts, attemptPayload(attempt))
	}
	checkpoints := make([]map[string]any, 0, len(bundle.Checkpoints))
	for _, checkpoint := range bundle.Checkpoints {
		checkpoints = append(checkpoints, map[string]any{
			"id":        uuidText(checkpoint.ID),
			"summary":   checkpoint.Summary,
			"next_step": textValue(checkpoint.NextStep),
			"risk":      textValue(checkpoint.Risk),
		})
	}
	artifacts := make([]map[string]any, 0, len(bundle.Artifacts))
	for _, artifact := range bundle.Artifacts {
		artifacts = append(artifacts, artifactPayload(artifact))
	}
	return map[string]any{
		"ticket":                ticketPayload(bundle.Ticket),
		"attempt":               attemptPayload(bundle.Attempt),
		"acceptance_criteria":   stringSliceValue(bundle.AcceptanceCriteria),
		"verification_commands": stringSliceValue(bundle.VerificationCommands),
		"environment":           objectValue(bundle.Environment),
		"input":                 objectValue(bundle.Input),
		"relevant_paths":        stringSliceValue(bundle.RelevantPaths),
		"required_tools":        stringSliceValue(bundle.RequiredTools),
		"required_permissions":  stringSliceValue(bundle.RequiredPermissions),
		"expected_artifacts":    stringSliceValue(bundle.ExpectedArtifacts),
		"prior_attempts":        priorAttempts,
		"checkpoints":           checkpoints,
		"artifacts":             artifacts,
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

func textValue(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func stringSliceValue(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func objectValue(value map[string]any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func artifactPayload(artifact db.Artifact) map[string]any {
	payload := map[string]any{
		"id":              uuidText(artifact.ID),
		"ticket_id":       uuidText(artifact.TicketID),
		"type":            artifact.Type,
		"role":            artifact.Role,
		"name":            artifact.Name,
		"url":             artifact.Url,
		"storage_backend": artifact.StorageBackend,
		"size_bytes":      artifact.SizeBytes,
		"mime_type":       artifact.MimeType,
	}
	if artifact.AttemptID.Valid {
		payload["attempt_id"] = uuidText(artifact.AttemptID)
	}
	return payload
}
