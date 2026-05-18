package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/a-h/templ"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/services"
)

const defaultTicketListLimit int32 = 50

type Runtime interface {
	ListTickets(context.Context, services.ListTicketsRequest) ([]db.Ticket, error)
	GetTicket(context.Context, pgtype.UUID) (db.Ticket, error)
	ListAttemptsByTicket(context.Context, pgtype.UUID) ([]db.Attempt, error)
	ListAttemptCheckpointsByTicket(context.Context, pgtype.UUID) ([]db.AttemptCheckpoint, error)
	ListTicketEventsByTicket(context.Context, pgtype.UUID) ([]db.TicketEvent, error)
	ListArtifactsByTicket(context.Context, pgtype.UUID) ([]db.Artifact, error)
}

type Handler struct {
	runtime Runtime
}

func NewHandler(runtime Runtime) http.Handler {
	return Handler{runtime: runtime}
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		renderStatus(r.Context(), w, http.StatusMethodNotAllowed, "Method not allowed", "Only GET requests are supported for web inspection pages.")
		return
	}
	switch {
	case r.URL.Path == "/tickets":
		h.renderTicketList(w, r)
	case strings.HasPrefix(r.URL.Path, "/tickets/"):
		h.renderTicketDetail(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h Handler) renderTicketList(w http.ResponseWriter, r *http.Request) {
	if h.runtime == nil {
		renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Runtime unavailable", "runtime is not configured")
		return
	}
	req, err := listTicketsRequestFromQuery(r)
	if err != nil {
		renderStatus(r.Context(), w, http.StatusBadRequest, "Ticket list needs a scope", err.Error())
		return
	}
	tickets, err := h.runtime.ListTickets(r.Context(), req)
	if err != nil {
		renderStatus(r.Context(), w, http.StatusInternalServerError, "Unable to load tickets", err.Error())
		return
	}
	renderComponent(r.Context(), w, http.StatusOK, ticketListPage(ticketListView{
		Tickets:     tickets,
		WorkspaceID: req.WorkspaceID,
		ProjectID:   req.ProjectID,
		Status:      req.Status,
		Type:        req.Type,
	}))
}

func (h Handler) renderTicketDetail(w http.ResponseWriter, r *http.Request) {
	if h.runtime == nil {
		renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Runtime unavailable", "runtime is not configured")
		return
	}
	idText := strings.TrimPrefix(r.URL.Path, "/tickets/")
	if strings.Contains(idText, "/") || strings.TrimSpace(idText) == "" {
		renderStatus(r.Context(), w, http.StatusNotFound, "Ticket not found", "ticket route does not exist")
		return
	}
	ticketID, err := parseUUID(idText)
	if err != nil {
		renderStatus(r.Context(), w, http.StatusBadRequest, "Invalid ticket id", "ticket id must be a UUID")
		return
	}
	ticket, err := h.runtime.GetTicket(r.Context(), ticketID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			renderStatus(r.Context(), w, http.StatusNotFound, "Ticket not found", "No ticket exists for that id.")
			return
		}
		renderStatus(r.Context(), w, http.StatusInternalServerError, "Unable to load ticket", err.Error())
		return
	}
	timeline, timelineErr := loadTimeline(r.Context(), h.runtime, ticketID)
	renderComponent(r.Context(), w, http.StatusOK, ticketDetailPage(ticketDetailView{
		Ticket:      ticket,
		Timeline:    timeline,
		TimelineErr: timelineErr,
	}))
}

type ticketListView struct {
	Tickets     []db.Ticket
	WorkspaceID pgtype.UUID
	ProjectID   pgtype.UUID
	Status      string
	Type        string
}

type ticketDetailView struct {
	Ticket      db.Ticket
	Timeline    ticketTimeline
	TimelineErr error
}

type ticketTimeline struct {
	Attempts    []db.Attempt
	Checkpoints []db.AttemptCheckpoint
	Events      []db.TicketEvent
	Artifacts   []db.Artifact
}

func loadTimeline(ctx context.Context, runtime Runtime, ticketID pgtype.UUID) (ticketTimeline, error) {
	attempts, err := runtime.ListAttemptsByTicket(ctx, ticketID)
	if err != nil {
		return ticketTimeline{}, err
	}
	checkpoints, err := runtime.ListAttemptCheckpointsByTicket(ctx, ticketID)
	if err != nil {
		return ticketTimeline{}, err
	}
	events, err := runtime.ListTicketEventsByTicket(ctx, ticketID)
	if err != nil {
		return ticketTimeline{}, err
	}
	artifacts, err := runtime.ListArtifactsByTicket(ctx, ticketID)
	if err != nil {
		return ticketTimeline{}, err
	}
	return ticketTimeline{
		Attempts:    attempts,
		Checkpoints: checkpoints,
		Events:      events,
		Artifacts:   artifacts,
	}, nil
}

func listTicketsRequestFromQuery(r *http.Request) (services.ListTicketsRequest, error) {
	query := r.URL.Query()
	workspaceID, err := parseUUID(query.Get("workspace_id"))
	if err != nil {
		return services.ListTicketsRequest{}, errors.New("workspace_id and project_id are required")
	}
	projectID, err := parseUUID(query.Get("project_id"))
	if err != nil {
		return services.ListTicketsRequest{}, errors.New("workspace_id and project_id are required")
	}
	limit := defaultTicketListLimit
	if value := strings.TrimSpace(query.Get("limit")); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 32)
		if err != nil || parsed < 0 {
			return services.ListTicketsRequest{}, errors.New("limit must be a non-negative integer")
		}
		limit = int32(parsed)
	}
	offset := int32(0)
	if value := strings.TrimSpace(query.Get("offset")); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 32)
		if err != nil || parsed < 0 {
			return services.ListTicketsRequest{}, errors.New("offset must be a non-negative integer")
		}
		offset = int32(parsed)
	}
	return services.ListTicketsRequest{
		WorkspaceID: workspaceID,
		ProjectID:   projectID,
		Status:      strings.TrimSpace(query.Get("status")),
		Type:        strings.TrimSpace(query.Get("type")),
		Offset:      offset,
		Limit:       limit,
	}, nil
}

func parseUUID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	value = strings.TrimSpace(value)
	if value == "" {
		return pgtype.UUID{}, errors.New("uuid is required")
	}
	if err := id.Scan(value); err != nil {
		return pgtype.UUID{}, err
	}
	return id, nil
}

func renderStatus(ctx context.Context, w http.ResponseWriter, status int, title string, message string) {
	renderComponent(ctx, w, status, statusPage(title, message))
}

func renderComponent(ctx context.Context, w http.ResponseWriter, status int, component templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := component.Render(ctx, w); err != nil {
		_, _ = fmt.Fprintf(w, "render error: %s", html.EscapeString(err.Error()))
	}
}

func statusPage(title string, message string) templ.Component {
	return layout(title, func(w io.Writer) {
		fmt.Fprintf(w, `<section class="panel"><h1>%s</h1><p>%s</p><p><a href="/tickets">Back to tickets</a></p></section>`, esc(title), esc(message))
	})
}

func ticketListPage(view ticketListView) templ.Component {
	return layout("Forge Tickets", func(w io.Writer) {
		fmt.Fprint(w, `<section class="page-head"><div><h1>Forge Tickets</h1><p>Shared inspection for claimable work, proposed follow-ups, and review handoffs.</p></div>`)
		fmt.Fprintf(w, `<a class="button" href="/tickets?workspace_id=%s&project_id=%s">Refresh</a></section>`, esc(uuidText(view.WorkspaceID)), esc(uuidText(view.ProjectID)))
		fmt.Fprint(w, `<section class="filters panel"><form method="get" action="/tickets">`)
		input(w, "workspace_id", uuidText(view.WorkspaceID))
		input(w, "project_id", uuidText(view.ProjectID))
		input(w, "status", view.Status)
		input(w, "type", view.Type)
		fmt.Fprint(w, `<button type="submit">Apply</button></form></section>`)
		if len(view.Tickets) == 0 {
			fmt.Fprint(w, `<section class="panel empty"><h2>No tickets match</h2><p>Change the scope or filters to inspect a different queue.</p></section>`)
			return
		}
		fmt.Fprint(w, `<section class="ticket-list" aria-label="Tickets">`)
		for _, ticket := range view.Tickets {
			writeTicketCard(w, ticket)
		}
		fmt.Fprint(w, `</section>`)
	})
}

func ticketDetailPage(view ticketDetailView) templ.Component {
	ticket := view.Ticket
	return layout(ticket.Title, func(w io.Writer) {
		fmt.Fprintf(w, `<section class="page-head"><div><p class="eyebrow">%s %s P%d</p><h1>%s</h1><p>%s</p></div><a class="button" href="/tickets?workspace_id=%s&project_id=%s">Back to list</a></section>`,
			esc(ticket.Status),
			esc(ticket.Type),
			ticket.Priority,
			esc(ticket.Title),
			esc(ticket.Description),
			esc(uuidText(ticket.WorkspaceID)),
			esc(uuidText(ticket.ProjectID)),
		)
		fmt.Fprint(w, `<section class="detail-grid">`)
		fmt.Fprint(w, `<article class="panel"><h2>Context</h2>`)
		writeMeta(w, "Ticket ID", uuidText(ticket.ID))
		writeMeta(w, "Created by", ticket.CreatedBy+"/"+textValue(ticket.CreatedByID))
		writeList(w, "Tags", ticket.Tags, "")
		writeList(w, "Acceptance", ticket.AcceptanceCriteria, "")
		writeList(w, "Verification", decodeStringArray(ticket.VerificationCommands), "$ ")
		writeList(w, "Paths", ticket.RelevantPaths, "")
		fmt.Fprint(w, `</article>`)
		writeTimeline(w, view)
		fmt.Fprint(w, `</section>`)
	})
}

func layout(title string, body func(io.Writer)) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title><script src="https://unpkg.com/htmx.org@2.0.4"></script><style>%s</style></head><body hx-boost="true"><main>`, esc(title), pageCSS())
		body(w)
		fmt.Fprint(w, `</main></body></html>`)
		return nil
	})
}

func input(w io.Writer, name string, value string) {
	fmt.Fprintf(w, `<label><span>%s</span><input name="%s" value="%s"></label>`, esc(strings.ReplaceAll(name, "_", " ")), esc(name), esc(value))
}

func writeTicketCard(w io.Writer, ticket db.Ticket) {
	fmt.Fprintf(w, `<article class="ticket-card"><a href="/tickets/%s"><span class="title">%s</span><span class="summary">%s %s P%d</span></a>`,
		esc(uuidText(ticket.ID)),
		esc(ticket.Title),
		esc(ticket.Status),
		esc(ticket.Type),
		ticket.Priority,
	)
	writeList(w, "Tags", ticket.Tags, "")
	if ticket.CreatedBy != "" {
		writeMeta(w, "Source", ticket.CreatedBy)
	}
	fmt.Fprint(w, `</article>`)
}

func writeTimeline(w io.Writer, view ticketDetailView) {
	if view.TimelineErr != nil {
		fmt.Fprintf(w, `<article class="panel warning"><h2>Timeline unavailable</h2><p>%s</p></article>`, esc(view.TimelineErr.Error()))
		return
	}
	fmt.Fprint(w, `<article class="panel"><h2>Attempts</h2>`)
	if len(view.Timeline.Attempts) == 0 {
		fmt.Fprint(w, `<p class="empty-text">No attempts recorded yet.</p>`)
	}
	for _, attempt := range view.Timeline.Attempts {
		fmt.Fprintf(w, `<div class="timeline-item"><strong>%s</strong><span>%s/%s</span>`, esc(attempt.Status), esc(attempt.AgentID), esc(attempt.Model))
		if attempt.CurrentSummary.Valid {
			fmt.Fprintf(w, `<p>%s</p>`, esc(attempt.CurrentSummary.String))
		}
		fmt.Fprint(w, `</div>`)
	}
	fmt.Fprint(w, `<h2>Events</h2>`)
	if len(view.Timeline.Events) == 0 {
		fmt.Fprint(w, `<p class="empty-text">No ticket events recorded.</p>`)
	}
	for _, event := range view.Timeline.Events {
		fmt.Fprintf(w, `<div class="timeline-item"><strong>%s</strong><span>%s/%s</span>`, esc(event.Type), esc(event.ActorType), esc(textValue(event.ActorID)))
		if reason := timelineReason(event.Data); reason != "" {
			fmt.Fprintf(w, `<p>%s</p>`, esc(reason))
		}
		fmt.Fprint(w, `</div>`)
	}
	fmt.Fprint(w, `<h2>Proof artifacts</h2>`)
	if len(view.Timeline.Artifacts) == 0 {
		fmt.Fprint(w, `<p class="empty-text">No proof artifacts recorded.</p>`)
	}
	for _, artifact := range view.Timeline.Artifacts {
		fmt.Fprintf(w, `<div class="timeline-item"><strong>%s</strong><span>%s %s</span>`, esc(artifact.Name), esc(artifact.Role), esc(artifact.Type))
		if artifact.Url != "" {
			fmt.Fprintf(w, `<p><a href="%s">%s</a></p>`, esc(artifact.Url), esc(artifact.Url))
		}
		fmt.Fprint(w, `</div>`)
	}
	fmt.Fprint(w, `</article>`)
}

func writeMeta(w io.Writer, label string, value string) {
	if value == "" || value == "-" {
		return
	}
	fmt.Fprintf(w, `<p class="meta"><span>%s</span><strong>%s</strong></p>`, esc(label), esc(value))
}

func writeList(w io.Writer, title string, values []string, prefix string) {
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(w, `<div class="list"><h3>%s</h3><ul>`, esc(title))
	for _, value := range values {
		fmt.Fprintf(w, `<li>%s%s</li>`, esc(prefix), esc(value))
	}
	fmt.Fprint(w, `</ul></div>`)
}

func decodeStringArray(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil
	}
	return values
}

func timelineReason(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return string(raw)
	}
	for _, key := range []string{"reason", "summary", "message", "detail"} {
		if value, ok := data[key].(string); ok && value != "" {
			return value
		}
	}
	return string(raw)
}

func textValue(value pgtype.Text) string {
	if !value.Valid || value.String == "" {
		return "-"
	}
	return value.String
}

func uuidText(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	value, err := id.Value()
	if err != nil {
		return ""
	}
	text, _ := value.(string)
	return text
}

func esc(value string) string {
	return html.EscapeString(value)
}

func pageCSS() string {
	return `:root{font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;color:#202124;background:#f7f7f4}body{margin:0}main{max-width:1120px;margin:0 auto;padding:32px 20px 56px}.page-head{display:flex;justify-content:space-between;gap:24px;align-items:flex-start;margin-bottom:18px}.page-head h1{margin:4px 0 8px;font-size:32px;line-height:1.1;letter-spacing:0}.page-head p{margin:0;color:#5c625d;max-width:720px}.eyebrow{text-transform:uppercase;font-size:12px;font-weight:700;color:#5b6b5b}.button,button{border:1px solid #202124;background:#202124;color:#fff;text-decoration:none;padding:9px 12px;border-radius:6px;font-weight:700;white-space:nowrap}.panel{background:#fff;border:1px solid #d9ddd5;border-radius:8px;padding:18px}.filters form{display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:12px;align-items:end}.filters span{display:block;font-size:12px;font-weight:700;color:#5c625d;margin-bottom:5px;text-transform:capitalize}.filters input{width:100%;box-sizing:border-box;border:1px solid #c7ccc3;border-radius:6px;padding:9px;background:#fff}.ticket-list{display:grid;gap:10px;margin-top:16px}.ticket-card{background:#fff;border:1px solid #d9ddd5;border-radius:8px;padding:14px}.ticket-card a{display:flex;justify-content:space-between;gap:16px;color:inherit;text-decoration:none}.title{font-weight:800}.summary,.meta span,.empty-text{color:#61665f}.meta{display:flex;gap:10px;align-items:baseline;margin:8px 0}.meta span{min-width:84px;font-size:12px;font-weight:700;text-transform:uppercase}.list h3{font-size:13px;margin:16px 0 6px;color:#3c463e}.list ul{margin:0;padding-left:20px}.detail-grid{display:grid;grid-template-columns:minmax(0,1fr) minmax(320px,.85fr);gap:16px}.timeline-item{border-top:1px solid #e5e8e1;padding:12px 0}.timeline-item strong{display:block}.timeline-item span{color:#61665f;font-size:13px}.warning{border-color:#d8b45f;background:#fffaf0}@media(max-width:760px){main{padding:20px 14px}.page-head,.ticket-card a{display:block}.filters form,.detail-grid{grid-template-columns:1fr}.button{display:inline-block;margin-top:12px}}`
}
