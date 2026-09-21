package web

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/vivek/agent-task-tracker/internal/db"
)

type scopeOptions struct {
	Workspaces []db.Workspace
	Projects   []db.Project
}

func (h Handler) loadScopeOptions(ctx context.Context, workspaceID string) (scopeOptions, error) {
	workspaces, err := h.runtime.ListWorkspaces(ctx)
	if err != nil {
		return scopeOptions{}, err
	}
	scope := scopeOptions{Workspaces: workspaces}
	id, err := parseUUID(workspaceID)
	if err == nil {
		scope.Projects, err = h.runtime.ListProjectsByWorkspace(ctx, id)
		if err != nil {
			return scopeOptions{}, err
		}
	}
	return scope, nil
}

func (scope scopeOptions) hasProject(workspaceID, projectID string) bool {
	workspace, workspaceErr := parseUUID(workspaceID)
	project, projectErr := parseUUID(projectID)
	if workspaceErr != nil || projectErr != nil {
		return false
	}
	for _, candidate := range scope.Projects {
		if candidate.ID == project && candidate.WorkspaceID == workspace {
			return true
		}
	}
	return false
}

func writeScopeFields(w io.Writer, scope scopeOptions, workspaceID, projectID string) {
	writeScopeFieldsWithSubmitter(w, scope, workspaceID, projectID, "")
}

// submitter is a JavaScript expression supplied by the renderer, never form input.
func writeScopeFieldsWithSubmitter(w io.Writer, scope scopeOptions, workspaceID, projectID, submitter string) {
	onchange := "this.form.elements.project_id.value='';this.form.requestSubmit(" + submitter + ")"
	fmt.Fprintf(w, `<label><span>Workspace</span><select name="workspace_id" aria-label="Workspace" onchange="%s"><option value="">Choose workspace</option>`, esc(onchange))
	for _, workspace := range scope.Workspaces {
		writeOption(w, uuidText(workspace.ID), workspace.Name, workspaceID)
	}
	fmt.Fprint(w, `</select></label><label><span>Project</span><select name="project_id" aria-label="Project"><option value="">Choose project</option>`)
	for _, project := range scope.Projects {
		writeOption(w, uuidText(project.ID), project.Name, projectID)
	}
	fmt.Fprint(w, `</select></label>`)
}

func writeOption(w io.Writer, value, label, selected string) {
	attribute := ""
	if value == selected {
		attribute = " selected"
	}
	fmt.Fprintf(w, `<option value="%s"%s>%s</option>`, esc(value), attribute, esc(label))
}

func writeChoiceField(w io.Writer, name, selected, emptyLabel string, values []string) {
	fmt.Fprintf(w, `<label><span>%s</span><select name="%s" aria-label="%s">`, esc(displayLabel(name)), esc(name), esc(displayLabel(name)))
	if emptyLabel != "" {
		writeOption(w, "", emptyLabel, selected)
	}
	for _, value := range values {
		writeOption(w, value, displayLabel(value), selected)
	}
	fmt.Fprint(w, `</select></label>`)
}

func displayLabel(value string) string {
	labels := map[string]string{"todo": "Ready", "in_progress": "In progress", "needs_review": "Needs review", "follow_up": "Follow-up", "q": "Search"}
	if label, ok := labels[value]; ok {
		return label
	}
	value = strings.ReplaceAll(value, "_", " ")
	if value == "" {
		return value
	}
	return strings.ToUpper(value[:1]) + value[1:]
}

func scopeFormError(r *http.Request, err error) (int, string) {
	query := r.URL.Query()
	for _, field := range []string{"workspace_id", "project_id"} {
		if _, parseErr := parseOptionalUUID(query.Get(field)); parseErr != nil {
			return http.StatusBadRequest, err.Error()
		}
	}
	if query.Get("workspace_id") == "" || query.Get("project_id") == "" {
		return http.StatusOK, "Choose a workspace and project to continue."
	}
	if r.URL.Path == "/search" && strings.TrimSpace(query.Get("q")) == "" {
		return http.StatusOK, "Enter a phrase to search tickets, checkpoints, and evidence."
	}
	return http.StatusBadRequest, err.Error()
}

var ticketTypes = []string{"task", "bug", "feature", "documentation", "research", "analysis", "planning", "review", "integration", "investigation", "cleanup", "follow_up", "custom"}
var ticketStatuses = []string{"todo", "in_progress", "blocked", "needs_review", "done", "failed", "backlog", "archived"}
