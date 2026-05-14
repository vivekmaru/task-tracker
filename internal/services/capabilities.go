package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

const (
	TransportCLI  = "cli"
	TransportREST = "rest"
	TransportMCP  = "mcp"
)

type CapabilityStore interface {
	UpsertAgentCapability(context.Context, db.UpsertAgentCapabilityParams) (db.AgentCapability, error)
	GetAgentCapability(context.Context, db.GetAgentCapabilityParams) (db.AgentCapability, error)
	ListAgentCapabilities(context.Context, db.ListAgentCapabilitiesParams) ([]db.AgentCapability, error)
}

var _ CapabilityStore = (*db.Queries)(nil)

type CapabilityService struct {
	store CapabilityStore
	now   func() time.Time
}

type CapabilityOption func(*CapabilityService)

func WithCapabilityClock(clock func() time.Time) CapabilityOption {
	return func(service *CapabilityService) {
		service.now = clock
	}
}

func NewCapabilityService(store CapabilityStore, opts ...CapabilityOption) *CapabilityService {
	service := &CapabilityService{
		store: store,
		now:   time.Now,
	}
	for _, opt := range opts {
		opt(service)
	}
	return service
}

type RegisterCapabilitiesRequest struct {
	WorkspaceID    pgtype.UUID
	ProjectID      pgtype.UUID
	AgentID        string
	Harness        string
	Model          string
	Transports     []string
	Capabilities   []string
	ToolNames      []string
	ArtifactRoles  []string
	PreferredClaim map[string]any
	Metadata       map[string]any
}

type GetCapabilitiesRequest struct {
	WorkspaceID pgtype.UUID
	ProjectID   pgtype.UUID
	AgentID     string
	Harness     string
}

type ListCapabilitiesRequest struct {
	WorkspaceID pgtype.UUID
	ProjectID   pgtype.UUID
	Harness     string
	Capability  string
}

func (s *CapabilityService) Register(ctx context.Context, req RegisterCapabilitiesRequest) (db.AgentCapability, error) {
	req = trimRegisterCapabilitiesRequest(req)
	if problems := validateRegisterCapabilitiesRequest(req); len(problems) > 0 {
		return db.AgentCapability{}, ValidationError{Problems: problems}
	}
	preferredClaim, err := encodeJSONObject(req.PreferredClaim)
	if err != nil {
		return db.AgentCapability{}, fmt.Errorf("marshal preferred claim: %w", err)
	}
	metadata, err := encodeJSONObject(req.Metadata)
	if err != nil {
		return db.AgentCapability{}, fmt.Errorf("marshal metadata: %w", err)
	}

	record, err := s.store.UpsertAgentCapability(ctx, db.UpsertAgentCapabilityParams{
		WorkspaceID:    req.WorkspaceID,
		ProjectID:      req.ProjectID,
		AgentID:        req.AgentID,
		Harness:        req.Harness,
		Model:          req.Model,
		Transports:     req.Transports,
		Capabilities:   req.Capabilities,
		ToolNames:      req.ToolNames,
		ArtifactRoles:  req.ArtifactRoles,
		PreferredClaim: preferredClaim,
		Metadata:       metadata,
		LastSeenAt:     timestamptz(s.now().UTC()),
	})
	if err != nil {
		return db.AgentCapability{}, fmt.Errorf("upsert agent capability: %w", err)
	}
	return record, nil
}

func (s *CapabilityService) Get(ctx context.Context, req GetCapabilitiesRequest) (db.AgentCapability, error) {
	req = trimGetCapabilitiesRequest(req)
	if problems := validateGetCapabilitiesRequest(req); len(problems) > 0 {
		return db.AgentCapability{}, ValidationError{Problems: problems}
	}
	record, err := s.store.GetAgentCapability(ctx, db.GetAgentCapabilityParams{
		WorkspaceID: req.WorkspaceID,
		ProjectID:   req.ProjectID,
		AgentID:     req.AgentID,
		Harness:     req.Harness,
	})
	if err != nil {
		return db.AgentCapability{}, fmt.Errorf("get agent capability: %w", err)
	}
	return record, nil
}

func (s *CapabilityService) List(ctx context.Context, req ListCapabilitiesRequest) ([]db.AgentCapability, error) {
	req = trimListCapabilitiesRequest(req)
	if problems := validateListCapabilitiesRequest(req); len(problems) > 0 {
		return nil, ValidationError{Problems: problems}
	}
	records, err := s.store.ListAgentCapabilities(ctx, db.ListAgentCapabilitiesParams{
		WorkspaceID: req.WorkspaceID,
		ProjectID:   req.ProjectID,
		Harness:     optionalText(req.Harness),
		Capability:  optionalText(req.Capability),
	})
	if err != nil {
		return nil, fmt.Errorf("list agent capabilities: %w", err)
	}
	return records, nil
}

func trimRegisterCapabilitiesRequest(req RegisterCapabilitiesRequest) RegisterCapabilitiesRequest {
	req.AgentID = strings.TrimSpace(req.AgentID)
	req.Harness = strings.TrimSpace(req.Harness)
	req.Model = strings.TrimSpace(req.Model)
	req.Transports = compactStrings(req.Transports)
	req.Capabilities = compactStrings(req.Capabilities)
	req.ToolNames = compactStrings(req.ToolNames)
	req.ArtifactRoles = compactStrings(req.ArtifactRoles)
	return req
}

func trimGetCapabilitiesRequest(req GetCapabilitiesRequest) GetCapabilitiesRequest {
	req.AgentID = strings.TrimSpace(req.AgentID)
	req.Harness = strings.TrimSpace(req.Harness)
	return req
}

func trimListCapabilitiesRequest(req ListCapabilitiesRequest) ListCapabilitiesRequest {
	req.Harness = strings.TrimSpace(req.Harness)
	req.Capability = strings.TrimSpace(req.Capability)
	return req
}

func validateRegisterCapabilitiesRequest(req RegisterCapabilitiesRequest) []string {
	var problems []string
	problems = append(problems, validateCapabilityScope(req.WorkspaceID, req.ProjectID)...)
	if req.AgentID == "" {
		problems = append(problems, "agent_id is required")
	}
	if req.Harness == "" {
		problems = append(problems, "harness is required")
	}
	if len(req.Transports) == 0 {
		problems = append(problems, "transports is required")
	}
	for _, transport := range req.Transports {
		if !isAllowedTransport(transport) {
			problems = append(problems, "transports contains an invalid transport")
			break
		}
	}
	if len(req.Capabilities) == 0 {
		problems = append(problems, "capabilities is required")
	}
	for _, role := range req.ArtifactRoles {
		if !isAllowedArtifactRole(role) {
			problems = append(problems, "artifact_roles contains an invalid role")
			break
		}
	}
	return problems
}

func validateGetCapabilitiesRequest(req GetCapabilitiesRequest) []string {
	var problems []string
	problems = append(problems, validateCapabilityScope(req.WorkspaceID, req.ProjectID)...)
	if req.AgentID == "" {
		problems = append(problems, "agent_id is required")
	}
	if req.Harness == "" {
		problems = append(problems, "harness is required")
	}
	return problems
}

func validateListCapabilitiesRequest(req ListCapabilitiesRequest) []string {
	return validateCapabilityScope(req.WorkspaceID, req.ProjectID)
}

func validateCapabilityScope(workspaceID, projectID pgtype.UUID) []string {
	var problems []string
	if !workspaceID.Valid {
		problems = append(problems, "workspace_id is required")
	}
	if !projectID.Valid {
		problems = append(problems, "project_id is required")
	}
	return problems
}

func isAllowedTransport(value string) bool {
	switch value {
	case TransportCLI, TransportREST, TransportMCP:
		return true
	default:
		return false
	}
}
