package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

const (
	ArtifactTypeCode          = "code"
	ArtifactTypeDocument      = "document"
	ArtifactTypeImage         = "image"
	ArtifactTypeDataset       = "dataset"
	ArtifactTypeLog           = "log"
	ArtifactTypeDiff          = "diff"
	ArtifactTypeTrace         = "trace"
	ArtifactTypeTestOutput    = "test_output"
	ArtifactTypeScreenshot    = "screenshot"
	ArtifactTypeHandoff       = "handoff"
	ArtifactTypeDiagnostic    = "diagnostic"
	ArtifactTypeFinalResponse = "final_response"
	ArtifactTypeOther         = "other"

	ArtifactRoleEvidence   = "evidence"
	ArtifactRolePatch      = "patch"
	ArtifactRoleContext    = "context"
	ArtifactRoleOutput     = "output"
	ArtifactRoleDiagnostic = "diagnostic"
	ArtifactRoleHandoff    = "handoff"

	ArtifactStorageLocal = "local"
	ArtifactStorageS3    = "s3"
)

type ArtifactStore interface {
	CreateArtifact(context.Context, db.CreateArtifactParams) (db.Artifact, error)
	ListArtifactsByTicket(context.Context, pgtype.UUID) ([]db.Artifact, error)
	ListArtifactsByAttempt(context.Context, pgtype.UUID) ([]db.Artifact, error)
	GetArtifact(context.Context, pgtype.UUID) (db.Artifact, error)
}

var _ ArtifactStore = (*db.Queries)(nil)

type ArtifactService struct {
	store ArtifactStore
}

func NewArtifactService(store ArtifactStore) *ArtifactService {
	return &ArtifactService{store: store}
}

type RegisterArtifactRequest struct {
	WorkspaceID    pgtype.UUID
	ProjectID      pgtype.UUID
	TicketID       pgtype.UUID
	AttemptID      pgtype.UUID
	Type           string
	Role           string
	Name           string
	URL            string
	StorageBackend string
	SizeBytes      int64
	MimeType       string
	Metadata       map[string]any
}

func (s *ArtifactService) RegisterArtifact(ctx context.Context, req RegisterArtifactRequest) (db.Artifact, error) {
	req = trimRegisterArtifactRequest(req)
	if problems := validateRegisterArtifactRequest(req); len(problems) > 0 {
		return db.Artifact{}, ValidationError{Problems: problems}
	}
	metadata, err := encodeJSONObject(req.Metadata)
	if err != nil {
		return db.Artifact{}, fmt.Errorf("marshal artifact metadata: %w", err)
	}
	artifact, err := s.store.CreateArtifact(ctx, db.CreateArtifactParams{
		WorkspaceID:    req.WorkspaceID,
		ProjectID:      req.ProjectID,
		TicketID:       req.TicketID,
		AttemptID:      req.AttemptID,
		Type:           req.Type,
		Role:           req.Role,
		Name:           req.Name,
		Url:            req.URL,
		StorageBackend: req.StorageBackend,
		SizeBytes:      req.SizeBytes,
		MimeType:       req.MimeType,
		Metadata:       metadata,
	})
	if err != nil {
		return db.Artifact{}, fmt.Errorf("create artifact: %w", err)
	}
	return artifact, nil
}

func (s *ArtifactService) ListArtifactsByTicket(ctx context.Context, ticketID pgtype.UUID) ([]db.Artifact, error) {
	return s.store.ListArtifactsByTicket(ctx, ticketID)
}

func (s *ArtifactService) ListArtifactsByAttempt(ctx context.Context, attemptID pgtype.UUID) ([]db.Artifact, error) {
	return s.store.ListArtifactsByAttempt(ctx, attemptID)
}

func (s *ArtifactService) GetArtifact(ctx context.Context, id pgtype.UUID) (db.Artifact, error) {
	return s.store.GetArtifact(ctx, id)
}

func trimRegisterArtifactRequest(req RegisterArtifactRequest) RegisterArtifactRequest {
	req.Type = strings.TrimSpace(req.Type)
	req.Role = strings.TrimSpace(req.Role)
	req.Name = strings.TrimSpace(req.Name)
	req.URL = strings.TrimSpace(req.URL)
	req.StorageBackend = strings.TrimSpace(req.StorageBackend)
	req.MimeType = strings.TrimSpace(req.MimeType)
	if req.StorageBackend == "" {
		req.StorageBackend = ArtifactStorageLocal
	}
	return req
}

func validateRegisterArtifactRequest(req RegisterArtifactRequest) []string {
	var problems []string
	if !req.WorkspaceID.Valid {
		problems = append(problems, "workspace_id is required")
	}
	if !req.ProjectID.Valid {
		problems = append(problems, "project_id is required")
	}
	if !req.TicketID.Valid {
		problems = append(problems, "ticket_id is required")
	}
	if req.Type == "" {
		problems = append(problems, "type is required")
	} else if !isAllowedArtifactType(req.Type) {
		problems = append(problems, "type is not valid")
	}
	if req.Role == "" {
		problems = append(problems, "role is required")
	} else if !isAllowedArtifactRole(req.Role) {
		problems = append(problems, "role is not valid")
	}
	if req.Name == "" {
		problems = append(problems, "name is required")
	}
	if req.URL == "" {
		problems = append(problems, "url is required")
	}
	if req.StorageBackend != ArtifactStorageLocal && req.StorageBackend != ArtifactStorageS3 {
		problems = append(problems, "storage_backend must be local or s3")
	}
	if req.SizeBytes < 0 {
		problems = append(problems, "size_bytes must be non-negative")
	}
	return problems
}

func isAllowedArtifactType(value string) bool {
	switch value {
	case ArtifactTypeCode,
		ArtifactTypeDocument,
		ArtifactTypeImage,
		ArtifactTypeDataset,
		ArtifactTypeLog,
		ArtifactTypeDiff,
		ArtifactTypeTrace,
		ArtifactTypeTestOutput,
		ArtifactTypeScreenshot,
		ArtifactTypeHandoff,
		ArtifactTypeDiagnostic,
		ArtifactTypeFinalResponse,
		ArtifactTypeOther:
		return true
	default:
		return false
	}
}

func isAllowedArtifactRole(value string) bool {
	switch value {
	case ArtifactRoleEvidence,
		ArtifactRolePatch,
		ArtifactRoleContext,
		ArtifactRoleOutput,
		ArtifactRoleDiagnostic,
		ArtifactRoleHandoff:
		return true
	default:
		return false
	}
}

func artifactMetadata(raw []byte) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var metadata map[string]any
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return nil, err
	}
	return metadata, nil
}
