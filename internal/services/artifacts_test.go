package services

import (
	"context"
	"encoding/json"
	"errors"
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

type fakeArtifactStore struct {
	createParams []db.CreateArtifactParams
	created      db.Artifact
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
	return db.Artifact{}, nil
}
