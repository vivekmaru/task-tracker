package services

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

const (
	TemplateBug           = "bug"
	TemplateFeature       = "feature"
	TemplateDocumentation = "documentation"
	TemplateReview        = "review"
	TemplateInvestigation = "investigation"
	TemplateCleanup       = "cleanup"
	TemplateFollowUp      = "follow_up"
)

type TicketTemplate struct {
	Kind            string
	Type            string
	Summary         string
	RequiredFields  []string
	OptionalFields  []string
	AcceptanceHints []string
}

type CreateTicketFromAttemptRequest struct {
	WorkspaceID      pgtype.UUID
	ProjectID        pgtype.UUID
	SourceAttemptID  pgtype.UUID
	SourceArtifactID pgtype.UUID

	TemplateKind string
	Title        string
	Description  string
	Priority     *int32
	Tags         []string

	AcceptanceCriteria   []string
	VerificationCommands []string
	ExpectedArtifacts    []string
	RelevantPaths        []string
	RequiredTools        []string
	RequiredPermissions  []string
	RequiredCapabilities []string
	AllowedHarnesses     []string
	Environment          map[string]any
	Input                map[string]any

	CreatedByID    string
	CreationReason string
	Enqueue        bool
	CanEnqueue     bool
}

var ticketTemplates = []TicketTemplate{
	{
		Kind:            TemplateBug,
		Type:            TicketTypeBug,
		Summary:         "Defect, regression, or broken behavior discovered during execution.",
		RequiredFields:  []string{"title", "description", "acceptance_criteria", "verification_commands", "creation_reason"},
		OptionalFields:  []string{"relevant_paths", "source_artifact_id", "required_capabilities"},
		AcceptanceHints: []string{"Describe the failing behavior that must no longer happen.", "Include the regression check that proves the fix."},
	},
	{
		Kind:            TemplateFeature,
		Type:            TicketTypeFeature,
		Summary:         "New user- or agent-facing behavior that should be planned and implemented.",
		RequiredFields:  []string{"title", "description", "acceptance_criteria", "creation_reason"},
		OptionalFields:  []string{"verification_commands", "expected_artifacts", "required_capabilities"},
		AcceptanceHints: []string{"State the observable behavior the feature adds.", "Name the smallest useful proof artifact."},
	},
	{
		Kind:            TemplateDocumentation,
		Type:            TicketTypeDocumentation,
		Summary:         "Documentation gap, stale instructions, or missing developer guidance.",
		RequiredFields:  []string{"title", "description", "acceptance_criteria", "relevant_paths", "creation_reason"},
		OptionalFields:  []string{"verification_commands", "expected_artifacts"},
		AcceptanceHints: []string{"Identify the audience and where the doc should live.", "Describe what a future reader can do after the update."},
	},
	{
		Kind:            TemplateReview,
		Type:            TicketTypeReview,
		Summary:         "Human or agent review work for correctness, design, security, or quality.",
		RequiredFields:  []string{"title", "description", "acceptance_criteria", "creation_reason"},
		OptionalFields:  []string{"relevant_paths", "required_tools", "expected_artifacts"},
		AcceptanceHints: []string{"State the review surface.", "Describe the decision or findings expected."},
	},
	{
		Kind:            TemplateInvestigation,
		Type:            TicketTypeInvestigation,
		Summary:         "Unknown cause or unclear requirements that need exploration before implementation.",
		RequiredFields:  []string{"title", "description", "acceptance_criteria", "creation_reason"},
		OptionalFields:  []string{"relevant_paths", "verification_commands", "required_tools"},
		AcceptanceHints: []string{"Name the question to answer.", "Describe what evidence will make the next step clear."},
	},
	{
		Kind:            TemplateCleanup,
		Type:            TicketTypeCleanup,
		Summary:         "Low-risk cleanup that removes stale, duplicated, or confusing project material.",
		RequiredFields:  []string{"title", "description", "acceptance_criteria", "verification_commands", "creation_reason"},
		OptionalFields:  []string{"relevant_paths", "expected_artifacts"},
		AcceptanceHints: []string{"State what should be simpler or removed.", "Include the command that proves behavior did not change."},
	},
	{
		Kind:            TemplateFollowUp,
		Type:            TicketTypeFollowUp,
		Summary:         "General follow-up work discovered while executing a different ticket.",
		RequiredFields:  []string{"title", "description", "acceptance_criteria", "creation_reason"},
		OptionalFields:  []string{"verification_commands", "relevant_paths", "source_artifact_id"},
		AcceptanceHints: []string{"Explain why this should be separate work.", "Describe the proof a future worker should attach."},
	},
}

func TicketTemplates() []TicketTemplate {
	out := make([]TicketTemplate, len(ticketTemplates))
	copy(out, ticketTemplates)
	return out
}

func TicketTemplateByKind(kind string) (TicketTemplate, bool) {
	kind = strings.TrimSpace(kind)
	for _, template := range ticketTemplates {
		if template.Kind == kind {
			return template, true
		}
	}
	return TicketTemplate{}, false
}

func (s *TicketService) CreateTicketFromAttempt(ctx context.Context, req CreateTicketFromAttemptRequest) (db.Ticket, error) {
	req = trimCreateTicketFromAttemptRequest(req)
	template, ok := TicketTemplateByKind(req.TemplateKind)

	ticketReq := createTicketRequestFromAttempt(req, template)
	problems := validateCreateTicketFromAttemptRequest(req, ok, ticketReq)
	if len(problems) > 0 {
		return db.Ticket{}, ValidationError{Problems: problems}
	}

	if req.Enqueue {
		return s.CreateTicket(ctx, ticketReq)
	}
	return s.ProposeTicket(ctx, ticketReq)
}

func createTicketRequestFromAttempt(req CreateTicketFromAttemptRequest, template TicketTemplate) CreateTicketRequest {
	status := TicketStatusBacklog
	if req.Enqueue {
		status = TicketStatusTodo
	}
	return CreateTicketRequest{
		WorkspaceID:          req.WorkspaceID,
		ProjectID:            req.ProjectID,
		SourceAttemptID:      req.SourceAttemptID,
		SourceArtifactID:     req.SourceArtifactID,
		Title:                req.Title,
		Description:          req.Description,
		Type:                 template.Type,
		Status:               status,
		Priority:             req.Priority,
		Tags:                 req.Tags,
		AcceptanceCriteria:   req.AcceptanceCriteria,
		VerificationCommands: req.VerificationCommands,
		ExpectedArtifacts:    req.ExpectedArtifacts,
		RelevantPaths:        req.RelevantPaths,
		RequiredTools:        req.RequiredTools,
		RequiredPermissions:  req.RequiredPermissions,
		Environment:          req.Environment,
		Input:                req.Input,
		RequiredCapabilities: req.RequiredCapabilities,
		AllowedHarnesses:     req.AllowedHarnesses,
		CreatedBy:            ActorAgent,
		CreatedByID:          req.CreatedByID,
		CreationReason:       req.CreationReason,
		Enqueue:              req.Enqueue,
		CanEnqueue:           req.CanEnqueue,
	}
}

func trimCreateTicketFromAttemptRequest(req CreateTicketFromAttemptRequest) CreateTicketFromAttemptRequest {
	req.TemplateKind = strings.TrimSpace(req.TemplateKind)
	req.Title = strings.TrimSpace(req.Title)
	req.Description = strings.TrimSpace(req.Description)
	req.CreatedByID = strings.TrimSpace(req.CreatedByID)
	req.CreationReason = strings.TrimSpace(req.CreationReason)
	req.Tags = compactStrings(req.Tags)
	req.AcceptanceCriteria = compactStrings(req.AcceptanceCriteria)
	req.VerificationCommands = compactStrings(req.VerificationCommands)
	req.ExpectedArtifacts = compactStrings(req.ExpectedArtifacts)
	req.RelevantPaths = compactStrings(req.RelevantPaths)
	req.RequiredTools = compactStrings(req.RequiredTools)
	req.RequiredPermissions = compactStrings(req.RequiredPermissions)
	req.RequiredCapabilities = compactStrings(req.RequiredCapabilities)
	req.AllowedHarnesses = compactStrings(req.AllowedHarnesses)
	return req
}

func validateCreateTicketFromAttemptRequest(req CreateTicketFromAttemptRequest, knownTemplate bool, ticketReq CreateTicketRequest) []string {
	var problems []string
	if !req.SourceAttemptID.Valid {
		problems = append(problems, "source_attempt_id is required")
	}
	if req.TemplateKind == "" {
		problems = append(problems, "template_kind is required")
	} else if !knownTemplate {
		problems = append(problems, "template_kind is not valid")
	}
	problems = append(problems, validateCreateTicketRequest(ticketReq)...)
	return problems
}
