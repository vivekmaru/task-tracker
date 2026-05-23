package services

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

func TestWorkspaceServiceAddsAndUpdatesMembers(t *testing.T) {
	store := &fakeWorkspaceMemberStore{
		list: []db.WorkspaceMember{
			{WorkspaceID: testUUID(1), ActorType: ActorHuman, ActorID: "vivek", Role: WorkspaceRoleOwner},
			{WorkspaceID: testUUID(1), ActorType: ActorAgent, ActorID: "codex", Role: WorkspaceRoleMember},
		},
	}
	service := NewWorkspaceService(store)

	member, err := service.AddMember(context.Background(), AddWorkspaceMemberRequest{
		WorkspaceID: testUUID(1),
		ActorType:   " human ",
		ActorID:     " vivek ",
		Role:        " owner ",
	})
	if err != nil {
		t.Fatalf("add workspace member: %v", err)
	}
	if member.ActorType != ActorHuman || member.ActorID != "vivek" || member.Role != WorkspaceRoleOwner {
		t.Fatalf("expected normalized owner member, got %#v", member)
	}
	if store.upsert.ActorType != ActorHuman || store.upsert.ActorID != "vivek" || store.upsert.Role != WorkspaceRoleOwner {
		t.Fatalf("expected normalized upsert params, got %#v", store.upsert)
	}

	members, err := service.ListMembers(context.Background(), ListWorkspaceMembersRequest{WorkspaceID: testUUID(1)})
	if err != nil {
		t.Fatalf("list workspace members: %v", err)
	}
	if len(members) != 2 {
		t.Fatalf("expected two members, got %#v", members)
	}
	if store.listWorkspaceID != testUUID(1) {
		t.Fatalf("expected list scoped to workspace, got %#v", store.listWorkspaceID)
	}

	updated, err := service.UpdateMemberRole(context.Background(), UpdateWorkspaceMemberRoleRequest{
		WorkspaceID: testUUID(1),
		ActorType:   ActorAgent,
		ActorID:     "codex",
		Role:        WorkspaceRoleAdmin,
	})
	if err != nil {
		t.Fatalf("update workspace member role: %v", err)
	}
	if updated.Role != WorkspaceRoleAdmin {
		t.Fatalf("expected updated admin role, got %#v", updated)
	}
	if store.update.Role != WorkspaceRoleAdmin {
		t.Fatalf("expected update params to carry admin role, got %#v", store.update)
	}
}

func TestWorkspaceServiceRemovesMembers(t *testing.T) {
	store := &fakeWorkspaceMemberStore{}
	service := NewWorkspaceService(store)

	if err := service.RemoveMember(context.Background(), RemoveWorkspaceMemberRequest{
		WorkspaceID: testUUID(1),
		ActorType:   ActorHuman,
		ActorID:     "vivek",
	}); err != nil {
		t.Fatalf("remove workspace member: %v", err)
	}
	if store.delete.WorkspaceID != testUUID(1) || store.delete.ActorType != ActorHuman || store.delete.ActorID != "vivek" {
		t.Fatalf("unexpected delete params: %#v", store.delete)
	}
}

func TestWorkspaceServiceRejectsMalformedMembers(t *testing.T) {
	service := NewWorkspaceService(&fakeWorkspaceMemberStore{})

	_, err := service.AddMember(context.Background(), AddWorkspaceMemberRequest{
		WorkspaceID: pgtype.UUID{},
		ActorType:   "service-account",
		ActorID:     " ",
		Role:        "superadmin",
	})
	if err == nil {
		t.Fatal("expected validation error")
	}
	validation, ok := err.(ValidationError)
	if !ok {
		t.Fatalf("expected ValidationError, got %T: %v", err, err)
	}
	for _, want := range []string{
		"workspace_id is required",
		"actor_type is not valid",
		"actor_id is required",
		"role is not valid",
	} {
		if !containsProblem(validation.Problems, want) {
			t.Fatalf("expected validation problem %q, got %#v", want, validation.Problems)
		}
	}
}

type fakeWorkspaceMemberStore struct {
	upsert          db.UpsertWorkspaceMemberParams
	update          db.UpdateWorkspaceMemberRoleParams
	delete          db.DeleteWorkspaceMemberParams
	listWorkspaceID pgtype.UUID
	list            []db.WorkspaceMember
}

func (f *fakeWorkspaceMemberStore) UpsertWorkspaceMember(_ context.Context, params db.UpsertWorkspaceMemberParams) (db.WorkspaceMember, error) {
	f.upsert = params
	return db.WorkspaceMember{
		WorkspaceID: params.WorkspaceID,
		ActorType:   params.ActorType,
		ActorID:     params.ActorID,
		Role:        params.Role,
	}, nil
}

func (f *fakeWorkspaceMemberStore) ListWorkspaceMembers(_ context.Context, workspaceID pgtype.UUID) ([]db.WorkspaceMember, error) {
	f.listWorkspaceID = workspaceID
	return f.list, nil
}

func (f *fakeWorkspaceMemberStore) UpdateWorkspaceMemberRole(_ context.Context, params db.UpdateWorkspaceMemberRoleParams) (db.WorkspaceMember, error) {
	f.update = params
	return db.WorkspaceMember{
		WorkspaceID: params.WorkspaceID,
		ActorType:   params.ActorType,
		ActorID:     params.ActorID,
		Role:        params.Role,
	}, nil
}

func (f *fakeWorkspaceMemberStore) DeleteWorkspaceMember(_ context.Context, params db.DeleteWorkspaceMemberParams) error {
	f.delete = params
	return nil
}
