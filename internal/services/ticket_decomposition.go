package services

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

const (
	DecomposeModePropose = "propose"
	DecomposeModeCreate  = "create"
)

type DecomposeTicketRequest struct {
	WorkspaceID pgtype.UUID
	ProjectID   pgtype.UUID
	ParentID    pgtype.UUID
	RootID      pgtype.UUID
	Mode        string
	CanEnqueue  bool

	CreatedBy      string
	CreatedByID    string
	CreationReason string

	Children []DecomposeChildRequest
}

type DecomposeChildRequest struct {
	Key         string
	Title       string
	Description string
	Type        string
	Priority    *int32
	Tags        []string

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

	DependsOn []string
}

type DecomposeTicketResult struct {
	Children []db.Ticket
}

func (s *TicketService) DecomposeTicket(ctx context.Context, req DecomposeTicketRequest) (DecomposeTicketResult, error) {
	req = trimDecomposeTicketRequest(req)
	childRequests := createRequestsFromDecomposition(req)
	if problems := validateDecomposeTicketRequest(req, childRequests); len(problems) > 0 {
		return DecomposeTicketResult{}, ValidationError{Problems: problems}
	}

	children := make([]db.Ticket, 0, len(childRequests))
	childIDsByKey := map[string]pgtype.UUID{}
	for i, childReq := range childRequests {
		var (
			child db.Ticket
			err   error
		)
		if req.Mode == DecomposeModeCreate {
			child, err = s.CreateTicket(ctx, childReq)
		} else {
			child, err = s.ProposeTicket(ctx, childReq)
		}
		if err != nil {
			return DecomposeTicketResult{}, fmt.Errorf("create decomposed child %q: %w", req.Children[i].Key, err)
		}
		children = append(children, child)
		childIDsByKey[req.Children[i].Key] = child.ID
	}

	for i, child := range children {
		for _, dependencyKey := range req.Children[i].DependsOn {
			_, err := s.store.CreateTicketDependency(ctx, db.CreateTicketDependencyParams{
				TicketID:          child.ID,
				DependsOnTicketID: childIDsByKey[dependencyKey],
				WorkspaceID:       child.WorkspaceID,
				ProjectID:         child.ProjectID,
			})
			if err != nil {
				return DecomposeTicketResult{}, fmt.Errorf("create decomposed child dependency: %w", err)
			}
		}
	}

	return DecomposeTicketResult{Children: children}, nil
}

func trimDecomposeTicketRequest(req DecomposeTicketRequest) DecomposeTicketRequest {
	req.Mode = strings.TrimSpace(req.Mode)
	if req.Mode == "" {
		req.Mode = DecomposeModePropose
	}
	req.CreatedBy = strings.TrimSpace(req.CreatedBy)
	if req.CreatedBy == "" {
		req.CreatedBy = ActorAgent
	}
	req.CreatedByID = strings.TrimSpace(req.CreatedByID)
	req.CreationReason = strings.TrimSpace(req.CreationReason)
	for i := range req.Children {
		child := &req.Children[i]
		child.Key = strings.TrimSpace(child.Key)
		child.Title = strings.TrimSpace(child.Title)
		child.Description = strings.TrimSpace(child.Description)
		child.Type = strings.TrimSpace(child.Type)
		child.Tags = compactStrings(child.Tags)
		child.AcceptanceCriteria = compactStrings(child.AcceptanceCriteria)
		child.VerificationCommands = compactStrings(child.VerificationCommands)
		child.ExpectedArtifacts = compactStrings(child.ExpectedArtifacts)
		child.RelevantPaths = compactStrings(child.RelevantPaths)
		child.RequiredTools = compactStrings(child.RequiredTools)
		child.RequiredPermissions = compactStrings(child.RequiredPermissions)
		child.RequiredCapabilities = compactStrings(child.RequiredCapabilities)
		child.AllowedHarnesses = compactStrings(child.AllowedHarnesses)
		child.DependsOn = compactStrings(child.DependsOn)
	}
	return req
}

func createRequestsFromDecomposition(req DecomposeTicketRequest) []CreateTicketRequest {
	rootID := req.RootID
	if !rootID.Valid {
		rootID = req.ParentID
	}
	status := TicketStatusBacklog
	enqueue := false
	if req.Mode == DecomposeModeCreate {
		status = TicketStatusTodo
		enqueue = true
	}

	children := make([]CreateTicketRequest, 0, len(req.Children))
	for _, child := range req.Children {
		children = append(children, CreateTicketRequest{
			WorkspaceID:          req.WorkspaceID,
			ProjectID:            req.ProjectID,
			ParentID:             req.ParentID,
			RootID:               rootID,
			Title:                child.Title,
			Description:          child.Description,
			Type:                 child.Type,
			Status:               status,
			Priority:             child.Priority,
			Tags:                 child.Tags,
			AcceptanceCriteria:   child.AcceptanceCriteria,
			VerificationCommands: child.VerificationCommands,
			ExpectedArtifacts:    child.ExpectedArtifacts,
			RelevantPaths:        child.RelevantPaths,
			RequiredTools:        child.RequiredTools,
			RequiredPermissions:  child.RequiredPermissions,
			Environment:          child.Environment,
			Input:                child.Input,
			RequiredCapabilities: child.RequiredCapabilities,
			AllowedHarnesses:     child.AllowedHarnesses,
			CreatedBy:            req.CreatedBy,
			CreatedByID:          req.CreatedByID,
			CreationReason:       req.CreationReason,
			Enqueue:              enqueue,
			CanEnqueue:           req.CanEnqueue,
		})
	}
	return children
}

func validateDecomposeTicketRequest(req DecomposeTicketRequest, children []CreateTicketRequest) []string {
	var problems []string
	if !req.WorkspaceID.Valid {
		problems = append(problems, "workspace_id is required")
	}
	if !req.ProjectID.Valid {
		problems = append(problems, "project_id is required")
	}
	if !req.ParentID.Valid {
		problems = append(problems, "parent_id is required")
	}
	if req.Mode != DecomposeModePropose && req.Mode != DecomposeModeCreate {
		problems = append(problems, "mode must be propose or create")
	}
	if len(req.Children) == 0 {
		problems = append(problems, "children is required")
	}

	keys := map[string]struct{}{}
	for i, child := range req.Children {
		if child.Key == "" {
			problems = append(problems, fmt.Sprintf("children[%d].key is required", i))
		} else if _, exists := keys[child.Key]; exists {
			problems = append(problems, fmt.Sprintf("children[%d].key is duplicated", i))
		} else {
			keys[child.Key] = struct{}{}
		}
	}
	for i, child := range req.Children {
		for _, dependencyKey := range child.DependsOn {
			if _, ok := keys[dependencyKey]; !ok {
				problems = append(problems, fmt.Sprintf("children[%d].depends_on references unknown child %q", i, dependencyKey))
			}
			if dependencyKey == child.Key {
				problems = append(problems, fmt.Sprintf("children[%d].depends_on cannot reference itself", i))
			}
		}
	}
	if len(problems) == 0 && hasDependencyCycle(req.Children) {
		problems = append(problems, "children dependencies contain a cycle")
	}
	for i, childReq := range children {
		for _, problem := range validateCreateTicketRequest(childReq) {
			problems = append(problems, fmt.Sprintf("children[%d].%s", i, problem))
		}
	}
	return problems
}

func hasDependencyCycle(children []DecomposeChildRequest) bool {
	dependencies := make(map[string][]string, len(children))
	for _, child := range children {
		dependencies[child.Key] = child.DependsOn
	}

	const (
		unvisited = 0
		visiting  = 1
		visited   = 2
	)
	state := make(map[string]int, len(children))
	var visit func(string) bool
	visit = func(key string) bool {
		switch state[key] {
		case visiting:
			return true
		case visited:
			return false
		}
		state[key] = visiting
		for _, dependency := range dependencies[key] {
			if visit(dependency) {
				return true
			}
		}
		state[key] = visited
		return false
	}

	for _, child := range children {
		if state[child.Key] == unvisited && visit(child.Key) {
			return true
		}
	}
	return false
}
