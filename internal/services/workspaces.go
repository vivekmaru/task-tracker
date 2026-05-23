package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

const (
	WorkspaceRoleOwner  = "owner"
	WorkspaceRoleAdmin  = "admin"
	WorkspaceRoleMember = "member"
	WorkspaceRoleViewer = "viewer"
)

type WorkspaceMemberStore interface {
	UpsertWorkspaceMember(context.Context, db.UpsertWorkspaceMemberParams) (db.WorkspaceMember, error)
	ListWorkspaceMembers(context.Context, pgtype.UUID) ([]db.WorkspaceMember, error)
	UpdateWorkspaceMemberRole(context.Context, db.UpdateWorkspaceMemberRoleParams) (db.WorkspaceMember, error)
	DeleteWorkspaceMember(context.Context, db.DeleteWorkspaceMemberParams) error
}

var _ WorkspaceMemberStore = (*db.Queries)(nil)

type WorkspaceService struct {
	store WorkspaceMemberStore
}

func NewWorkspaceService(store WorkspaceMemberStore) *WorkspaceService {
	return &WorkspaceService{store: store}
}

type AddWorkspaceMemberRequest struct {
	WorkspaceID pgtype.UUID
	ActorType   string
	ActorID     string
	Role        string
}

type ListWorkspaceMembersRequest struct {
	WorkspaceID pgtype.UUID
}

type UpdateWorkspaceMemberRoleRequest struct {
	WorkspaceID pgtype.UUID
	ActorType   string
	ActorID     string
	Role        string
}

type RemoveWorkspaceMemberRequest struct {
	WorkspaceID pgtype.UUID
	ActorType   string
	ActorID     string
}

func (s *WorkspaceService) AddMember(ctx context.Context, req AddWorkspaceMemberRequest) (db.WorkspaceMember, error) {
	req = trimAddWorkspaceMemberRequest(req)
	if problems := validateWorkspaceMember(req.WorkspaceID, req.ActorType, req.ActorID, req.Role); len(problems) > 0 {
		return db.WorkspaceMember{}, ValidationError{Problems: problems}
	}
	member, err := s.store.UpsertWorkspaceMember(ctx, db.UpsertWorkspaceMemberParams{
		WorkspaceID: req.WorkspaceID,
		ActorType:   req.ActorType,
		ActorID:     req.ActorID,
		Role:        req.Role,
	})
	if err != nil {
		return db.WorkspaceMember{}, fmt.Errorf("upsert workspace member: %w", err)
	}
	return member, nil
}

func (s *WorkspaceService) ListMembers(ctx context.Context, req ListWorkspaceMembersRequest) ([]db.WorkspaceMember, error) {
	if !req.WorkspaceID.Valid {
		return nil, ValidationError{Problems: []string{"workspace_id is required"}}
	}
	members, err := s.store.ListWorkspaceMembers(ctx, req.WorkspaceID)
	if err != nil {
		return nil, fmt.Errorf("list workspace members: %w", err)
	}
	return members, nil
}

func (s *WorkspaceService) UpdateMemberRole(ctx context.Context, req UpdateWorkspaceMemberRoleRequest) (db.WorkspaceMember, error) {
	req = trimUpdateWorkspaceMemberRoleRequest(req)
	if problems := validateWorkspaceMember(req.WorkspaceID, req.ActorType, req.ActorID, req.Role); len(problems) > 0 {
		return db.WorkspaceMember{}, ValidationError{Problems: problems}
	}
	member, err := s.store.UpdateWorkspaceMemberRole(ctx, db.UpdateWorkspaceMemberRoleParams{
		WorkspaceID: req.WorkspaceID,
		ActorType:   req.ActorType,
		ActorID:     req.ActorID,
		Role:        req.Role,
	})
	if err != nil {
		return db.WorkspaceMember{}, fmt.Errorf("update workspace member role: %w", err)
	}
	return member, nil
}

func (s *WorkspaceService) RemoveMember(ctx context.Context, req RemoveWorkspaceMemberRequest) error {
	req = trimRemoveWorkspaceMemberRequest(req)
	if problems := validateWorkspaceMemberIdentity(req.WorkspaceID, req.ActorType, req.ActorID); len(problems) > 0 {
		return ValidationError{Problems: problems}
	}
	if err := s.store.DeleteWorkspaceMember(ctx, db.DeleteWorkspaceMemberParams{
		WorkspaceID: req.WorkspaceID,
		ActorType:   req.ActorType,
		ActorID:     req.ActorID,
	}); err != nil {
		return fmt.Errorf("delete workspace member: %w", err)
	}
	return nil
}

func trimAddWorkspaceMemberRequest(req AddWorkspaceMemberRequest) AddWorkspaceMemberRequest {
	req.ActorType = normalizeWorkspaceMemberToken(req.ActorType)
	req.ActorID = strings.TrimSpace(req.ActorID)
	req.Role = normalizeWorkspaceMemberToken(req.Role)
	return req
}

func trimUpdateWorkspaceMemberRoleRequest(req UpdateWorkspaceMemberRoleRequest) UpdateWorkspaceMemberRoleRequest {
	req.ActorType = normalizeWorkspaceMemberToken(req.ActorType)
	req.ActorID = strings.TrimSpace(req.ActorID)
	req.Role = normalizeWorkspaceMemberToken(req.Role)
	return req
}

func trimRemoveWorkspaceMemberRequest(req RemoveWorkspaceMemberRequest) RemoveWorkspaceMemberRequest {
	req.ActorType = normalizeWorkspaceMemberToken(req.ActorType)
	req.ActorID = strings.TrimSpace(req.ActorID)
	return req
}

func normalizeWorkspaceMemberToken(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func validateWorkspaceMember(workspaceID pgtype.UUID, actorType, actorID, role string) []string {
	problems := validateWorkspaceMemberIdentity(workspaceID, actorType, actorID)
	if role == "" {
		problems = append(problems, "role is required")
	} else if !isAllowedWorkspaceRole(role) {
		problems = append(problems, "role is not valid")
	}
	return problems
}

func validateWorkspaceMemberIdentity(workspaceID pgtype.UUID, actorType, actorID string) []string {
	var problems []string
	if !workspaceID.Valid {
		problems = append(problems, "workspace_id is required")
	}
	if actorType == "" {
		problems = append(problems, "actor_type is required")
	} else if !isAllowedWorkspaceMemberActorType(actorType) {
		problems = append(problems, "actor_type is not valid")
	}
	if actorID == "" {
		problems = append(problems, "actor_id is required")
	}
	return problems
}

func isAllowedWorkspaceMemberActorType(value string) bool {
	switch value {
	case ActorHuman, ActorAgent, ActorSystem:
		return true
	default:
		return false
	}
}

func isAllowedWorkspaceRole(value string) bool {
	switch value {
	case WorkspaceRoleOwner, WorkspaceRoleAdmin, WorkspaceRoleMember, WorkspaceRoleViewer:
		return true
	default:
		return false
	}
}
