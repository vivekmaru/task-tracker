package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

func TestRegisterArtifactStoresProofMetadata(t *testing.T) {
	store := &fakeArtifactStore{}
	service := NewArtifactService(store)

	artifact, err := service.RegisterArtifact(context.Background(), RegisterArtifactRequest{
		WorkspaceID:    testUUID(1),
		ProjectID:      testUUID(2),
		TicketID:       testUUID(3),
		AttemptID:      testUUID(4),
		Type:           ArtifactTypeTestOutput,
		Role:           ArtifactRoleEvidence,
		Name:           "test-output.txt",
		URL:            "local://artifacts/test-output.txt",
		StorageBackend: ArtifactStorageLocal,
		MimeType:       "text/plain",
		Metadata:       map[string]any{"command": "go test ./..."},
	})
	if err != nil {
		t.Fatalf("register artifact: %v", err)
	}

	params := store.createParams[0]
	if params.Type != ArtifactTypeTestOutput || params.Role != ArtifactRoleEvidence {
		t.Fatalf("unexpected artifact classification: %#v", params)
	}
	var metadata map[string]string
	if err := json.Unmarshal(params.Metadata, &metadata); err != nil {
		t.Fatalf("decode metadata: %v", err)
	}
	if metadata["command"] != "go test ./..." {
		t.Fatalf("expected metadata command, got %#v", metadata)
	}
	if artifact.ID != store.created.ID {
		t.Fatalf("expected created artifact, got %#v", artifact)
	}
}

func TestRegisterArtifactValidation(t *testing.T) {
	service := NewArtifactService(&fakeArtifactStore{})

	_, err := service.RegisterArtifact(context.Background(), RegisterArtifactRequest{})
	if err == nil {
		t.Fatal("expected validation error")
	}
	var validationErr ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	for _, want := range []string{
		"workspace_id is required",
		"project_id is required",
		"ticket_id is required",
		"type is required",
		"role is required",
		"name is required",
		"url is required",
	} {
		if !containsString(validationErr.Problems, want) {
			t.Fatalf("expected validation problem %q in %#v", want, validationErr.Problems)
		}
	}
}

func TestListArtifactsRequiresWorkspaceProjectScope(t *testing.T) {
	service := NewArtifactService(&fakeArtifactStore{})

	_, err := service.ListArtifacts(context.Background(), ListArtifactsRequest{})

	var validationErr ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected validation error, got %v", err)
	}
	if !containsProblem(validationErr.Problems, "workspace_id is required") || !containsProblem(validationErr.Problems, "project_id is required") {
		t.Fatalf("expected workspace/project validation, got %#v", validationErr.Problems)
	}
}

func TestListArtifactsPassesScopeAndDefaultsLimit(t *testing.T) {
	store := &fakeArtifactStore{}
	service := NewArtifactService(store)
	workspaceID := testUUID(1)
	projectID := testUUID(2)
	ticketID := testUUID(3)

	_, err := service.ListArtifacts(context.Background(), ListArtifactsRequest{
		WorkspaceID: workspaceID,
		ProjectID:   projectID,
		TicketID:    ticketID,
	})

	if err != nil {
		t.Fatalf("list artifacts: %v", err)
	}
	if store.listScopeParams.WorkspaceID != workspaceID || store.listScopeParams.ProjectID != projectID || store.listScopeParams.TicketID != ticketID {
		t.Fatalf("unexpected list scope params: %#v", store.listScopeParams)
	}
	if store.listScopeParams.LimitCount != 100 || store.listScopeParams.OffsetCount != 0 {
		t.Fatalf("expected default pagination, got %#v", store.listScopeParams)
	}
}

func TestDeleteArtifactRejectsNonLocalStorage(t *testing.T) {
	store := &fakeArtifactStore{
		artifact: db.Artifact{
			ID:             testUUID(7),
			StorageBackend: ArtifactStorageS3,
			Url:            "https://example.test/proof.log",
		},
	}
	service := NewArtifactService(store)

	_, err := service.DeleteLocalArtifact(context.Background(), testUUID(7), func(string) error {
		t.Fatal("cleanup should not run for non-local artifacts")
		return nil
	})

	if !errors.Is(err, ErrArtifactDeleteUnsupported) {
		t.Fatalf("expected unsupported delete error, got %v", err)
	}
	if store.deletedID.Valid {
		t.Fatalf("non-local delete should not remove metadata, got %#v", store.deletedID)
	}
}

func TestDeleteArtifactCleansLocalObjectBeforeMetadata(t *testing.T) {
	artifactID := testUUID(7)
	store := &fakeArtifactStore{
		artifact: db.Artifact{
			ID:             artifactID,
			StorageBackend: ArtifactStorageLocal,
			Url:            "local://artifacts/proof.log",
		},
	}
	service := NewArtifactService(store)
	var cleanedURL string

	artifact, err := service.DeleteLocalArtifact(context.Background(), artifactID, func(rawURL string) error {
		cleanedURL = rawURL
		if store.deletedID.Valid {
			t.Fatal("metadata should not be deleted before local object cleanup")
		}
		return nil
	})

	if err != nil {
		t.Fatalf("delete local artifact: %v", err)
	}
	if artifact.ID != artifactID || cleanedURL != "local://artifacts/proof.log" {
		t.Fatalf("unexpected delete result artifact=%#v cleaned=%q", artifact, cleanedURL)
	}
	if store.deletedID != artifactID {
		t.Fatalf("expected metadata deletion after cleanup, got %#v", store.deletedID)
	}
}

func TestDeleteArtifactKeepsMetadataWhenCleanupFails(t *testing.T) {
	artifactID := testUUID(7)
	store := &fakeArtifactStore{
		artifact: db.Artifact{
			ID:             artifactID,
			StorageBackend: ArtifactStorageLocal,
			Url:            "local://artifacts/proof.log",
		},
	}
	service := NewArtifactService(store)

	_, err := service.DeleteLocalArtifact(context.Background(), artifactID, func(string) error {
		return errors.New("permission denied")
	})

	if err == nil || !strings.Contains(err.Error(), "remove local artifact") {
		t.Fatalf("expected cleanup error, got %v", err)
	}
	if store.deletedID.Valid {
		t.Fatalf("metadata should remain when cleanup fails, got %#v", store.deletedID)
	}
}

type fakeArtifactStore struct {
	createParams    []db.CreateArtifactParams
	created         db.Artifact
	artifact        db.Artifact
	listScopeParams db.ListArtifactsByScopeParams
	deletedID       pgtype.UUID
}

func (s *fakeArtifactStore) CreateArtifact(_ context.Context, params db.CreateArtifactParams) (db.Artifact, error) {
	s.createParams = append(s.createParams, params)
	if !s.created.ID.Valid {
		s.created = db.Artifact{
			ID:             testUUID(9),
			WorkspaceID:    params.WorkspaceID,
			ProjectID:      params.ProjectID,
			TicketID:       params.TicketID,
			AttemptID:      params.AttemptID,
			Type:           params.Type,
			Role:           params.Role,
			Name:           params.Name,
			Url:            params.Url,
			StorageBackend: params.StorageBackend,
			MimeType:       params.MimeType,
			Metadata:       params.Metadata,
		}
	}
	return s.created, nil
}

func (s *fakeArtifactStore) ListArtifactsByTicket(context.Context, pgtype.UUID) ([]db.Artifact, error) {
	return nil, nil
}

func (s *fakeArtifactStore) ListArtifactsByAttempt(context.Context, pgtype.UUID) ([]db.Artifact, error) {
	return nil, nil
}

func (s *fakeArtifactStore) GetArtifact(context.Context, pgtype.UUID) (db.Artifact, error) {
	return s.artifact, nil
}

func (s *fakeArtifactStore) ListArtifactsByScope(_ context.Context, params db.ListArtifactsByScopeParams) ([]db.Artifact, error) {
	s.listScopeParams = params
	return nil, nil
}

func (s *fakeArtifactStore) DeleteArtifact(_ context.Context, id pgtype.UUID) error {
	s.deletedID = id
	return nil
}
