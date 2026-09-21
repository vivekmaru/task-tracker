package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/services"
)

type ticketCreationRuntime interface {
	CreateTicket(context.Context, services.CreateTicketRequest) (db.Ticket, error)
}

func (h Handler) renderNewTicket(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet, http.MethodPost) {
		return
	}
	if h.runtime == nil {
		renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Runtime unavailable", "runtime is not configured")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseForm(); err != nil {
		renderStatus(r.Context(), w, http.StatusBadRequest, "Unable to read ticket", "The form is too large or invalid.")
		return
	}
	values := r.PostForm
	if r.Method == http.MethodGet {
		values = url.Values{
			"workspace_id": {r.Form.Get("workspace_id")},
			"project_id":   {r.Form.Get("project_id")},
			"type":         {"task"},
			"priority":     {"2"},
		}
	}
	scope, err := h.loadScopeOptions(r.Context(), values.Get("workspace_id"))
	if err != nil {
		renderStatus(r.Context(), w, http.StatusInternalServerError, "Unable to load workspaces", "Refresh to try again.")
		return
	}
	// Scope selection is a draft redisplay, not a ticket creation attempt.
	if r.Method == http.MethodGet || values.Get("action") == "select_project" {
		renderComponent(r.Context(), w, http.StatusOK, newTicketPage(scope, values, ""))
		return
	}
	if !scope.hasProject(values.Get("workspace_id"), values.Get("project_id")) {
		renderComponent(r.Context(), w, http.StatusBadRequest, newTicketPage(scope, values, "Choose a project in the selected workspace."))
		return
	}
	workspaceID, _ := parseUUID(values.Get("workspace_id"))
	projectID, _ := parseUUID(values.Get("project_id"))
	priority, err := strconv.Atoi(values.Get("priority"))
	if err != nil || priority < 0 || priority > 4 {
		renderComponent(r.Context(), w, http.StatusBadRequest, newTicketPage(scope, values, "Choose a priority between 0 and 4."))
		return
	}
	creator, ok := h.runtime.(ticketCreationRuntime)
	if !ok {
		renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Ticket creation unavailable", "The ticket runtime is not configured.")
		return
	}
	priorityValue := int32(priority)
	ticket, err := creator.CreateTicket(r.Context(), services.CreateTicketRequest{
		WorkspaceID: workspaceID, ProjectID: projectID,
		Title: strings.TrimSpace(values.Get("title")), Type: values.Get("type"),
		Description: strings.TrimSpace(values.Get("description")), Priority: &priorityValue,
		AcceptanceCriteria: formLines(values.Get("acceptance")), VerificationCommands: formLines(values.Get("verification")),
		CreatedBy: services.ActorHuman, CreatedByID: "web",
	})
	if err != nil {
		var validation services.ValidationError
		if errors.As(err, &validation) {
			renderComponent(r.Context(), w, http.StatusBadRequest, newTicketPage(scope, values, validation.Error()))
			return
		}
		renderTicketServiceError(r.Context(), w, err, "Unable to create ticket")
		return
	}
	http.Redirect(w, r, "/tickets/"+uuidText(ticket.ID), http.StatusSeeOther)
}

func newTicketPage(scope scopeOptions, values url.Values, message string) templ.Component {
	workspaceID, projectID := values.Get("workspace_id"), values.Get("project_id")
	return layoutWithPage(pageContext{Title: "New ticket", ActiveRoute: "tickets", WorkspaceID: workspaceID, ProjectID: projectID}, func(w io.Writer) {
		fmt.Fprintf(w, `<section class="page-head"><div><p class="eyebrow">Give an agent a clear next step</p><h1>New ticket</h1><p>Small scope. Clear outcome. Evidence you can review.</p></div><a class="button secondary" href="%s">Back to queue</a></section>`, esc(scopedPagePath("/tickets", workspaceID, projectID)))
		if message != "" {
			fmt.Fprintf(w, `<p class="panel warning" role="alert">%s</p>`, esc(message))
		}
		fmt.Fprint(w, `<section class="create-grid"><form class="panel ticket-create" method="post" action="/tickets/new" hx-boost="false"><div class="form-row">`)
		writeScopeFieldsWithSubmitter(w, scope, workspaceID, projectID, "this.form.querySelector('button[value=select_project]')")
		fmt.Fprint(w, `</div><div><button class="secondary" type="submit" name="action" value="select_project" formnovalidate>Select project</button></div>`)
		canCreate := scope.hasProject(workspaceID, projectID)
		if !canCreate {
			fmt.Fprint(w, `<p class="empty-text">Choose a workspace and project above to create this ticket. You can keep editing your draft.</p>`)
		}
		if len(scope.Workspaces) == 0 {
			fmt.Fprint(w, `<a class="button secondary" href="/workspaces">Manage workspaces</a>`)
		}
		fmt.Fprintf(w, `<label><span>Title</span><input name="title" value="%s" placeholder="Fix session refresh after a connection timeout" required autofocus></label>`, esc(values.Get("title")))
		fmt.Fprint(w, `<div class="form-row">`)
		types := ticketTypes
		if selected := values.Get("type"); !slices.Contains(types, selected) {
			types = append([]string{selected}, types...)
		}
		writeChoiceField(w, "type", values.Get("type"), "", types)
		fmt.Fprint(w, `<label><span>Priority</span><select name="priority" aria-label="Priority">`)
		priorityFound := false
		for i, label := range []string{"P0 · Urgent", "P1 · High", "P2 · Normal", "P3 · Low", "P4 · Later"} {
			value := strconv.Itoa(i)
			writeOption(w, value, label, values.Get("priority"))
			priorityFound = priorityFound || value == values.Get("priority")
		}
		if !priorityFound {
			writeOption(w, values.Get("priority"), values.Get("priority"), values.Get("priority"))
		}
		fmt.Fprint(w, `</select></label></div>`)
		fmt.Fprintf(w, `<label><span>Context</span><textarea name="description" rows="4" placeholder="What is happening, and what should change?" required>%s</textarea></label>`, esc(values.Get("description")))
		fmt.Fprintf(w, `<label><span>Acceptance criteria</span><textarea name="acceptance" rows="3" placeholder="One observable outcome per line" required>%s</textarea><small>How will you know this work is done?</small></label>`, esc(values.Get("acceptance")))
		fmt.Fprintf(w, `<label><span>Verification commands <small>(optional)</small></span><textarea name="verification" rows="2" placeholder="go test ./...">%s</textarea><small>One command per line, for the agent to run in its own environment.</small></label>`, esc(values.Get("verification")))
		disabled := ""
		if !canCreate {
			disabled = " disabled"
		}
		fmt.Fprintf(w, `<div class="actions"><button type="submit" name="action" value="create"%s>Create ticket</button><span class="empty-text">Ready for an agent to claim</span></div></form><aside class="panel onboarding"><p class="eyebrow">The work loop</p><h2>From intent to evidence</h2><ol><li><strong>Describe the outcome</strong><p>Give one agent a bounded task and useful context.</p></li><li><strong>Let an agent claim it</strong><p>Forge records ownership, checkpoints, and each attempt.</p></li><li><strong>Inspect the evidence</strong><p>Review the result and proof before moving on.</p></li></ol><p class="empty-text">Forge tracks work; it does not launch agents or execute verification commands.</p></aside></section>`, disabled)
	})
}

func formLines(value string) []string {
	lines := []string{}
	for _, line := range strings.Split(value, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

func writeAgentSetup(w io.Writer, workspaceID, projectID string) {
	fmt.Fprintf(w, `<details class="agent-setup"><summary>Connect an agent to this queue</summary><p>On a host configured with this Forge database and artifact store, claim the next ready ticket:</p><pre><code>forge codex claim --workspace-id %s --project-id %s --agent-id my-agent --lease 30m</code></pre><p>Keep the lease alive with <code>forge heartbeat</code>, record progress with <code>forge checkpoint</code>, and attach evidence with <code>forge codex complete --proof</code>; use <code>--help</code> for required arguments.</p><p>For MCP clients, run <code>forge mcp --config /path/to/forge.json</code> on the configured host. CLI and MCP connect directly to PostgreSQL; remote agents need database access, not just the web URL.</p></details>`, esc(workspaceID), esc(projectID))
}

func writeQueueTabs(w io.Writer, view ticketListView) {
	fmt.Fprint(w, `<nav class="queue-tabs" aria-label="Ticket status">`)
	for _, status := range []string{"", "todo", "in_progress", "blocked", "needs_review", "done"} {
		label, active := displayLabel(status), ""
		if status == "" {
			label = "All work"
		}
		if status == view.Status {
			active = ` aria-current="page"`
		}
		filtered := view
		filtered.Status = status
		fmt.Fprintf(w, `<a href="%s"%s>%s</a>`, esc(ticketListPagePath(filtered, 0)), active, esc(label))
	}
	fmt.Fprint(w, `</nav>`)
}
