package web

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/services"
)

const defaultTicketListLimit int32 = 50
const defaultSessionCookieName = "forge_admin_session"

type Runtime interface {
	ListTickets(context.Context, services.ListTicketsRequest) ([]db.Ticket, error)
	SearchTickets(context.Context, services.SearchTicketsRequest) ([]services.SearchResult, error)
	GetTicket(context.Context, pgtype.UUID) (db.Ticket, error)
	GetAttempt(context.Context, pgtype.UUID) (db.Attempt, error)
	ListAttemptsByTicket(context.Context, pgtype.UUID) ([]db.Attempt, error)
	ListAttemptCheckpointsByTicket(context.Context, pgtype.UUID) ([]db.AttemptCheckpoint, error)
	ListTicketEventsByTicket(context.Context, pgtype.UUID) ([]db.TicketEvent, error)
	ListArtifactsByTicket(context.Context, pgtype.UUID) ([]db.Artifact, error)
	ListArtifactsByAttempt(context.Context, pgtype.UUID) ([]db.Artifact, error)
	GetArtifact(context.Context, pgtype.UUID) (db.Artifact, error)
	ListWorkspaces(context.Context) ([]db.Workspace, error)
	GetWorkspace(context.Context, pgtype.UUID) (db.Workspace, error)
	CreateWorkspace(context.Context, string) (db.Workspace, error)
	ListProjectsByWorkspace(context.Context, pgtype.UUID) ([]db.Project, error)
	CreateProject(context.Context, pgtype.UUID, string) (db.Project, error)
}

type Handler struct {
	runtime Runtime
	auth    AuthOptions
}

func NewHandler(runtime Runtime) http.Handler {
	return Handler{runtime: runtime}
}

func NewHandlerWithAuth(runtime Runtime, auth AuthOptions) http.Handler {
	return Handler{runtime: runtime, auth: auth.normalized()}
}

type AuthOptions struct {
	AdminToken   string
	CookieName   string
	SecureCookie bool
	SessionTTL   time.Duration
	Now          func() time.Time
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if h.auth.enabled() {
		switch r.URL.Path {
		case "/login":
			h.handleLogin(w, r)
			return
		}
		if !h.isAuthorized(r) {
			h.requireLogin(w, r)
			return
		}
	}
	switch {
	case r.URL.Path == "/tickets":
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		h.renderTicketList(w, r)
	case r.URL.Path == "/search":
		h.renderSearch(w, r)
	case strings.HasPrefix(r.URL.Path, "/tickets/"):
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		h.renderTicketDetail(w, r)
	case strings.HasPrefix(r.URL.Path, "/attempts/"):
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		h.renderAttemptDetail(w, r)
	case strings.HasPrefix(r.URL.Path, "/artifacts/"):
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		h.renderArtifactDetail(w, r)
	case strings.HasPrefix(r.URL.Path, "/proposed/"):
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		h.renderProposedDetail(w, r)
	case r.URL.Path == "/workspaces":
		h.renderWorkspaceIndex(w, r)
	case strings.HasPrefix(r.URL.Path, "/workspaces/"):
		h.renderWorkspaceRoute(w, r)
	default:
		http.NotFound(w, r)
	}
}

func requireMethod(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	for _, method := range methods {
		if r.Method == method {
			return true
		}
	}
	w.Header().Set("Allow", strings.Join(methods, ", "))
	renderStatus(r.Context(), w, http.StatusMethodNotAllowed, "Method not allowed", "This page does not support that request method.")
	return false
}

func (h Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		renderComponent(r.Context(), w, http.StatusOK, loginPage(sanitizeNext(r.URL.Query().Get("next")), ""))
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			renderComponent(r.Context(), w, http.StatusBadRequest, loginPage(sanitizeNext(r.FormValue("next")), "Unable to read login form."))
			return
		}
		next := sanitizeNext(r.FormValue("next"))
		if !constantTimeTokenEqual(r.FormValue("admin_token"), h.auth.AdminToken) {
			renderComponent(r.Context(), w, http.StatusUnauthorized, loginPage(next, "Invalid admin token."))
			return
		}
		expiresAt := h.auth.now().Add(h.auth.sessionTTL())
		http.SetCookie(w, &http.Cookie{
			Name:     h.auth.cookieName(),
			Value:    h.auth.sessionValue(expiresAt),
			Path:     "/",
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
			Secure:   h.auth.SecureCookie,
			Expires:  expiresAt,
			MaxAge:   int(h.auth.sessionTTL().Seconds()),
		})
		http.Redirect(w, r, next, http.StatusSeeOther)
	default:
		w.Header().Set("Allow", "GET, POST")
		renderStatus(r.Context(), w, http.StatusMethodNotAllowed, "Method not allowed", "Login accepts GET and POST requests.")
	}
}

func (h Handler) requireLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		loginURL := "/login?next=" + url.QueryEscape(r.URL.RequestURI())
		http.Redirect(w, r, loginURL, http.StatusSeeOther)
		return
	}
	w.Header().Set("WWW-Authenticate", `Bearer realm="Forge human web"`)
	renderStatus(r.Context(), w, http.StatusUnauthorized, "Login required", "Open /login or provide a valid bearer token.")
}

func (h Handler) isAuthorized(r *http.Request) bool {
	if token := bearerToken(r.Header.Get("Authorization")); token != "" && constantTimeTokenEqual(token, h.auth.AdminToken) {
		return true
	}
	if token := r.Header.Get("X-Forge-Admin-Token"); token != "" && constantTimeTokenEqual(token, h.auth.AdminToken) {
		return true
	}
	cookie, err := r.Cookie(h.auth.cookieName())
	if err != nil {
		return false
	}
	return h.auth.validSessionValue(cookie.Value)
}

func (a AuthOptions) normalized() AuthOptions {
	a.AdminToken = strings.TrimSpace(a.AdminToken)
	if a.CookieName == "" {
		a.CookieName = defaultSessionCookieName
	}
	if a.SessionTTL <= 0 {
		a.SessionTTL = 8 * time.Hour
	}
	if a.Now == nil {
		a.Now = time.Now
	}
	return a
}

func (a AuthOptions) enabled() bool {
	return strings.TrimSpace(a.AdminToken) != ""
}

func (a AuthOptions) cookieName() string {
	if a.CookieName == "" {
		return defaultSessionCookieName
	}
	return a.CookieName
}

func (a AuthOptions) sessionTTL() time.Duration {
	if a.SessionTTL <= 0 {
		return 8 * time.Hour
	}
	return a.SessionTTL
}

func (a AuthOptions) now() time.Time {
	if a.Now == nil {
		return time.Now()
	}
	return a.Now()
}

func (a AuthOptions) sessionValue(expiresAt time.Time) string {
	expiresUnix := expiresAt.Unix()
	message := fmt.Sprintf("forge-human-session-v1|%d", expiresUnix)
	mac := hmac.New(sha256.New, []byte(a.AdminToken))
	_, _ = mac.Write([]byte(message))
	return fmt.Sprintf("%d.%s", expiresUnix, hex.EncodeToString(mac.Sum(nil)))
}

func (a AuthOptions) validSessionValue(value string) bool {
	expiresText, sig, ok := strings.Cut(strings.TrimSpace(value), ".")
	if !ok || expiresText == "" || sig == "" {
		return false
	}
	expiresUnix, err := strconv.ParseInt(expiresText, 10, 64)
	if err != nil {
		return false
	}
	expiresAt := time.Unix(expiresUnix, 0)
	if !a.now().Before(expiresAt) {
		return false
	}
	return constantTimeTokenEqual(value, a.sessionValue(expiresAt))
}

func bearerToken(value string) string {
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return parts[1]
}

func constantTimeTokenEqual(got string, want string) bool {
	got = strings.TrimSpace(got)
	want = strings.TrimSpace(want)
	if got == "" || want == "" {
		return false
	}
	gotHash := sha256.Sum256([]byte(got))
	wantHash := sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(gotHash[:], wantHash[:]) == 1
}

func sanitizeNext(value string) string {
	if strings.TrimSpace(value) == "" || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") {
		return "/tickets"
	}
	return value
}

func (h Handler) renderTicketList(w http.ResponseWriter, r *http.Request) {
	if h.runtime == nil {
		renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Runtime unavailable", "runtime is not configured")
		return
	}
	req, err := listTicketsRequestFromQuery(r)
	if err != nil {
		query := r.URL.Query()
		renderComponent(r.Context(), w, http.StatusBadRequest, ticketListPage(ticketListView{
			Status:  strings.TrimSpace(query.Get("status")),
			Type:    strings.TrimSpace(query.Get("type")),
			Message: err.Error(),
		}))
		return
	}
	tickets, err := h.runtime.ListTickets(r.Context(), req)
	if err != nil {
		var validationErr services.ValidationError
		if errors.As(err, &validationErr) {
			renderStatus(r.Context(), w, http.StatusBadRequest, "Invalid ticket filters", validationErr.Error())
			return
		}
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

func (h Handler) renderSearch(w http.ResponseWriter, r *http.Request) {
	if h.runtime == nil {
		renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Runtime unavailable", "runtime is not configured")
		return
	}
	req, err := searchTicketsRequestFromQuery(r)
	if err != nil {
		query := r.URL.Query()
		renderComponent(r.Context(), w, http.StatusBadRequest, searchPage(searchView{
			WorkspaceIDText: strings.TrimSpace(query.Get("workspace_id")),
			ProjectIDText:   strings.TrimSpace(query.Get("project_id")),
			Query:           strings.TrimSpace(query.Get("q")),
			Message:         err.Error(),
		}))
		return
	}
	results, err := h.runtime.SearchTickets(r.Context(), req)
	if err != nil {
		var validationErr services.ValidationError
		if errors.As(err, &validationErr) {
			renderComponent(r.Context(), w, http.StatusBadRequest, searchPage(searchView{
				WorkspaceIDText: uuidText(req.WorkspaceID),
				ProjectIDText:   uuidText(req.ProjectID),
				Query:           req.Query,
				Message:         validationErr.Error(),
			}))
			return
		}
		renderStatus(r.Context(), w, http.StatusInternalServerError, "Unable to search tickets", err.Error())
		return
	}
	renderComponent(r.Context(), w, http.StatusOK, searchPage(searchView{
		Results:         results,
		WorkspaceIDText: uuidText(req.WorkspaceID),
		ProjectIDText:   uuidText(req.ProjectID),
		Query:           req.Query,
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

func (h Handler) renderAttemptDetail(w http.ResponseWriter, r *http.Request) {
	if h.runtime == nil {
		renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Runtime unavailable", "runtime is not configured")
		return
	}
	attemptID, err := parseIDFromPath(r.URL.Path, "/attempts/")
	if err != nil {
		renderStatus(r.Context(), w, http.StatusBadRequest, "Invalid attempt id", "attempt id must be a UUID")
		return
	}
	attempt, err := h.runtime.GetAttempt(r.Context(), attemptID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			renderStatus(r.Context(), w, http.StatusNotFound, "Attempt not found", "No attempt exists for that id.")
			return
		}
		renderStatus(r.Context(), w, http.StatusInternalServerError, "Unable to load attempt", err.Error())
		return
	}
	artifacts, err := h.runtime.ListArtifactsByAttempt(r.Context(), attemptID)
	if err != nil {
		renderStatus(r.Context(), w, http.StatusInternalServerError, "Unable to load attempt artifacts", err.Error())
		return
	}
	renderComponent(r.Context(), w, http.StatusOK, attemptDetailPage(attempt, artifacts))
}

func (h Handler) renderArtifactDetail(w http.ResponseWriter, r *http.Request) {
	if h.runtime == nil {
		renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Runtime unavailable", "runtime is not configured")
		return
	}
	artifactID, err := parseIDFromPath(r.URL.Path, "/artifacts/")
	if err != nil {
		renderStatus(r.Context(), w, http.StatusBadRequest, "Invalid artifact id", "artifact id must be a UUID")
		return
	}
	artifact, err := h.runtime.GetArtifact(r.Context(), artifactID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			renderStatus(r.Context(), w, http.StatusNotFound, "Artifact not found", "No artifact exists for that id.")
			return
		}
		renderStatus(r.Context(), w, http.StatusInternalServerError, "Unable to load artifact", err.Error())
		return
	}
	renderComponent(r.Context(), w, http.StatusOK, artifactDetailPage(artifact))
}

func (h Handler) renderProposedDetail(w http.ResponseWriter, r *http.Request) {
	if h.runtime == nil {
		renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Runtime unavailable", "runtime is not configured")
		return
	}
	ticketID, err := parseIDFromPath(r.URL.Path, "/proposed/")
	if err != nil {
		renderStatus(r.Context(), w, http.StatusBadRequest, "Invalid proposed ticket id", "proposed ticket id must be a UUID")
		return
	}
	ticket, err := h.runtime.GetTicket(r.Context(), ticketID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			renderStatus(r.Context(), w, http.StatusNotFound, "Proposed follow-up not found", "No proposed follow-up exists for that id.")
			return
		}
		renderStatus(r.Context(), w, http.StatusInternalServerError, "Unable to load proposed follow-up", err.Error())
		return
	}
	if !isProposedTicket(ticket) {
		renderStatus(r.Context(), w, http.StatusNotFound, "Proposed follow-up not found", "That ticket is not proposed follow-up work.")
		return
	}
	renderComponent(r.Context(), w, http.StatusOK, proposedDetailPage(ticket))
}

func (h Handler) renderWorkspaceIndex(w http.ResponseWriter, r *http.Request) {
	if h.runtime == nil {
		renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Runtime unavailable", "runtime is not configured")
		return
	}
	switch r.Method {
	case http.MethodGet:
		workspaces, err := h.runtime.ListWorkspaces(r.Context())
		if err != nil {
			renderStatus(r.Context(), w, http.StatusInternalServerError, "Unable to load workspaces", err.Error())
			return
		}
		renderComponent(r.Context(), w, http.StatusOK, workspaceIndexPage(workspaceIndexView{Workspaces: workspaces}))
	case http.MethodPost:
		if err := r.ParseForm(); err != nil {
			renderComponent(r.Context(), w, http.StatusBadRequest, workspaceIndexPage(workspaceIndexView{Message: "Unable to read workspace form."}))
			return
		}
		name := strings.TrimSpace(r.FormValue("name"))
		if name == "" {
			renderComponent(r.Context(), w, http.StatusBadRequest, workspaceIndexPage(workspaceIndexView{Message: "Workspace name is required."}))
			return
		}
		workspace, err := h.runtime.CreateWorkspace(r.Context(), name)
		if err != nil {
			renderCreateError(r.Context(), w, "Unable to create workspace", err)
			return
		}
		http.Redirect(w, r, "/workspaces/"+uuidText(workspace.ID), http.StatusSeeOther)
	default:
		w.Header().Set("Allow", "GET, POST")
		renderStatus(r.Context(), w, http.StatusMethodNotAllowed, "Method not allowed", "Workspaces accept GET and POST requests.")
	}
}

func (h Handler) renderWorkspaceRoute(w http.ResponseWriter, r *http.Request) {
	if h.runtime == nil {
		renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Runtime unavailable", "runtime is not configured")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/workspaces/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || strings.TrimSpace(parts[0]) == "" {
		http.NotFound(w, r)
		return
	}
	workspaceID, err := parseUUID(parts[0])
	if err != nil {
		renderStatus(r.Context(), w, http.StatusBadRequest, "Invalid workspace id", "workspace id must be a UUID")
		return
	}
	if len(parts) == 2 && parts[1] == "projects" {
		h.createProject(w, r, workspaceID)
		return
	}
	if len(parts) != 1 {
		http.NotFound(w, r)
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	workspace, err := h.runtime.GetWorkspace(r.Context(), workspaceID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			renderStatus(r.Context(), w, http.StatusNotFound, "Workspace not found", "No workspace exists for that id.")
			return
		}
		renderStatus(r.Context(), w, http.StatusInternalServerError, "Unable to load workspace", err.Error())
		return
	}
	projects, err := h.runtime.ListProjectsByWorkspace(r.Context(), workspaceID)
	if err != nil {
		renderStatus(r.Context(), w, http.StatusInternalServerError, "Unable to load projects", err.Error())
		return
	}
	renderComponent(r.Context(), w, http.StatusOK, workspaceDetailPage(workspaceDetailView{Workspace: workspace, Projects: projects}))
}

func (h Handler) createProject(w http.ResponseWriter, r *http.Request, workspaceID pgtype.UUID) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if err := r.ParseForm(); err != nil {
		renderStatus(r.Context(), w, http.StatusBadRequest, "Unable to create project", "Unable to read project form.")
		return
	}
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		renderStatus(r.Context(), w, http.StatusBadRequest, "Unable to create project", "Project name is required.")
		return
	}
	if _, err := h.runtime.CreateProject(r.Context(), workspaceID, name); err != nil {
		renderCreateError(r.Context(), w, "Unable to create project", err)
		return
	}
	http.Redirect(w, r, "/workspaces/"+uuidText(workspaceID), http.StatusSeeOther)
}

func renderCreateError(ctx context.Context, w http.ResponseWriter, title string, err error) {
	status := http.StatusInternalServerError
	message := err.Error()

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23503":
			status = http.StatusNotFound
			message = "Referenced workspace does not exist."
		case "23505":
			status = http.StatusConflict
			message = "A workspace or project with that name already exists."
		case "23502", "23514":
			status = http.StatusBadRequest
		}
	}

	renderStatus(ctx, w, status, title, message)
}

type ticketListView struct {
	Tickets     []db.Ticket
	WorkspaceID pgtype.UUID
	ProjectID   pgtype.UUID
	Status      string
	Type        string
	Message     string
}

type ticketDetailView struct {
	Ticket      db.Ticket
	Timeline    ticketTimeline
	TimelineErr error
}

type searchView struct {
	Results         []services.SearchResult
	WorkspaceIDText string
	ProjectIDText   string
	Query           string
	Message         string
}

type ticketTimeline struct {
	Attempts    []db.Attempt
	Checkpoints []db.AttemptCheckpoint
	Events      []db.TicketEvent
	Artifacts   []db.Artifact
}

type workspaceIndexView struct {
	Workspaces []db.Workspace
	Message    string
}

type workspaceDetailView struct {
	Workspace db.Workspace
	Projects  []db.Project
}

func loadTimeline(ctx context.Context, runtime Runtime, ticketID pgtype.UUID) (ticketTimeline, error) {
	attempts, err := runtime.ListAttemptsByTicket(ctx, ticketID)
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
		Attempts:  attempts,
		Events:    events,
		Artifacts: artifacts,
	}, nil
}

func searchTicketsRequestFromQuery(r *http.Request) (services.SearchTicketsRequest, error) {
	query := r.URL.Query()
	workspaceID, err := parseUUID(query.Get("workspace_id"))
	if err != nil {
		return services.SearchTicketsRequest{}, errors.New("workspace_id and project_id are required")
	}
	projectID, err := parseUUID(query.Get("project_id"))
	if err != nil {
		return services.SearchTicketsRequest{}, errors.New("workspace_id and project_id are required")
	}
	searchQuery := strings.TrimSpace(query.Get("q"))
	if searchQuery == "" {
		return services.SearchTicketsRequest{}, errors.New("query is required")
	}
	limit := int32(0)
	if value := strings.TrimSpace(query.Get("limit")); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 32)
		if err != nil || parsed < 0 {
			return services.SearchTicketsRequest{}, errors.New("limit must be a non-negative integer")
		}
		limit = int32(parsed)
	}
	offset := int32(0)
	if value := strings.TrimSpace(query.Get("offset")); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 32)
		if err != nil || parsed < 0 {
			return services.SearchTicketsRequest{}, errors.New("offset must be a non-negative integer")
		}
		offset = int32(parsed)
	}
	return services.SearchTicketsRequest{
		WorkspaceID: workspaceID,
		ProjectID:   projectID,
		Query:       searchQuery,
		Offset:      offset,
		Limit:       limit,
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

func parseIDFromPath(path string, prefix string) (pgtype.UUID, error) {
	idText := strings.TrimPrefix(path, prefix)
	if strings.Contains(idText, "/") || strings.TrimSpace(idText) == "" {
		return pgtype.UUID{}, errors.New("invalid route id")
	}
	return parseUUID(idText)
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

func loginPage(next string, message string) templ.Component {
	return layout("Forge Login", func(w io.Writer) {
		fmt.Fprint(w, `<section class="auth-panel panel"><h1>Forge Login</h1>`)
		if message != "" {
			fmt.Fprintf(w, `<p class="auth-error">%s</p>`, esc(message))
		}
		fmt.Fprint(w, `<form method="post" action="/login">`)
		fmt.Fprintf(w, `<input type="hidden" name="next" value="%s">`, esc(sanitizeNext(next)))
		fmt.Fprint(w, `<label><span>Admin token</span><input type="password" name="admin_token" autocomplete="current-password" autofocus></label>`)
		fmt.Fprint(w, `<button type="submit">Sign in</button></form></section>`)
	})
}

func ticketListPage(view ticketListView) templ.Component {
	return layout("Forge Tickets", func(w io.Writer) {
		fmt.Fprint(w, `<section class="page-head"><div><h1>Forge Tickets</h1><p>Shared inspection for claimable work, proposed follow-ups, and review handoffs.</p></div>`)
		fmt.Fprintf(w, `<div class="actions"><a class="button" href="/search?workspace_id=%s&project_id=%s">Search</a><a class="button" href="/tickets?workspace_id=%s&project_id=%s">Refresh</a></div></section>`, esc(uuidText(view.WorkspaceID)), esc(uuidText(view.ProjectID)), esc(uuidText(view.WorkspaceID)), esc(uuidText(view.ProjectID)))
		fmt.Fprint(w, `<section class="filters panel"><form method="get" action="/tickets">`)
		input(w, "workspace_id", uuidText(view.WorkspaceID))
		input(w, "project_id", uuidText(view.ProjectID))
		input(w, "status", view.Status)
		input(w, "type", view.Type)
		fmt.Fprint(w, `<button type="submit">Apply</button></form></section>`)
		if view.Message != "" {
			fmt.Fprintf(w, `<section class="panel empty"><h2>Ticket list needs a scope</h2><p>%s</p></section>`, esc(view.Message))
			return
		}
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

func searchPage(view searchView) templ.Component {
	return layout("Forge Search", func(w io.Writer) {
		fmt.Fprint(w, `<section class="page-head"><div><h1>Forge Search</h1><p>Find tickets through titles, descriptions, attempts, events, and proof artifacts.</p></div>`)
		fmt.Fprintf(w, `<a class="button" href="/tickets?workspace_id=%s&project_id=%s">Tickets</a></section>`, esc(view.WorkspaceIDText), esc(view.ProjectIDText))
		fmt.Fprint(w, `<section class="filters panel"><form method="get" action="/search">`)
		input(w, "workspace_id", view.WorkspaceIDText)
		input(w, "project_id", view.ProjectIDText)
		input(w, "q", view.Query)
		fmt.Fprint(w, `<button type="submit">Search</button></form></section>`)
		if view.Message != "" {
			fmt.Fprintf(w, `<section class="panel empty"><h2>Search needs a scope and query</h2><p>%s</p></section>`, esc(view.Message))
			return
		}
		if len(view.Results) == 0 {
			fmt.Fprint(w, `<section class="panel empty"><h2>No search results</h2><p>Try another execution detail, artifact name, or ticket phrase.</p></section>`)
			return
		}
		fmt.Fprint(w, `<section class="ticket-list" aria-label="Search results">`)
		for _, result := range view.Results {
			writeSearchResult(w, result)
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
		writeShareLinks(w, view)
		fmt.Fprint(w, `</article>`)
		writeTimeline(w, view)
		fmt.Fprint(w, `</section>`)
	})
}

func attemptDetailPage(attempt db.Attempt, artifacts []db.Artifact) templ.Component {
	return layout("Attempt Detail", func(w io.Writer) {
		fmt.Fprintf(w, `<section class="page-head"><div><p class="eyebrow">%s %s</p><h1>Attempt Detail</h1><p>%s/%s</p></div><a class="button" href="/tickets/%s">Ticket</a></section>`,
			esc(attempt.Status),
			esc(uuidText(attempt.ID)),
			esc(attempt.AgentID),
			esc(attempt.Model),
			esc(uuidText(attempt.TicketID)),
		)
		fmt.Fprint(w, `<section class="detail-grid"><article class="panel"><h2>Context</h2>`)
		writeMeta(w, "Attempt ID", uuidText(attempt.ID))
		writeMeta(w, "Ticket", "/tickets/"+uuidText(attempt.TicketID))
		if attempt.CurrentSummary.Valid {
			writeMeta(w, "Summary", attempt.CurrentSummary.String)
		}
		if attempt.NextStep.Valid {
			writeMeta(w, "Next", attempt.NextStep.String)
		}
		fmt.Fprint(w, `</article><article class="panel"><h2>Artifacts</h2>`)
		if len(artifacts) == 0 {
			fmt.Fprint(w, `<p class="empty-text">No artifacts recorded for this attempt.</p>`)
		}
		for _, artifact := range artifacts {
			fmt.Fprintf(w, `<div class="timeline-item"><strong>%s</strong><p><a class="copy-link" href="/artifacts/%s">/artifacts/%s</a></p></div>`,
				esc(artifact.Name),
				esc(uuidText(artifact.ID)),
				esc(uuidText(artifact.ID)),
			)
		}
		fmt.Fprint(w, `</article></section>`)
	})
}

func artifactDetailPage(artifact db.Artifact) templ.Component {
	return layout("Artifact Detail", func(w io.Writer) {
		fmt.Fprintf(w, `<section class="page-head"><div><p class="eyebrow">%s %s</p><h1>Artifact Detail</h1><p>%s</p></div><a class="button" href="/tickets/%s">Ticket</a></section>`,
			esc(artifact.Role),
			esc(artifact.Type),
			esc(artifact.Name),
			esc(uuidText(artifact.TicketID)),
		)
		fmt.Fprint(w, `<section class="panel"><h2>Links</h2>`)
		writeMeta(w, "Artifact", "/artifacts/"+uuidText(artifact.ID))
		writeMeta(w, "Ticket", "/tickets/"+uuidText(artifact.TicketID))
		if artifact.AttemptID.Valid {
			writeMeta(w, "Attempt", "/attempts/"+uuidText(artifact.AttemptID))
		}
		if artifactURL, ok := safeArtifactURL(artifact.Url); ok {
			fmt.Fprintf(w, `<p><a href="%s">%s</a></p>`, esc(artifactURL), esc(artifactURL))
		} else if artifact.Url != "" {
			fmt.Fprint(w, `<p class="empty-text">Artifact link hidden because its URL scheme is not supported.</p>`)
		}
		fmt.Fprint(w, `</section>`)
	})
}

func proposedDetailPage(ticket db.Ticket) templ.Component {
	return layout("Proposed Follow-up", func(w io.Writer) {
		fmt.Fprintf(w, `<section class="page-head"><div><p class="eyebrow">%s %s</p><h1>Proposed Follow-up</h1><p>%s</p></div><a class="button" href="/tickets/%s">Ticket detail</a></section>`,
			esc(ticket.Status),
			esc(ticket.Type),
			esc(ticket.Title),
			esc(uuidText(ticket.ID)),
		)
		fmt.Fprint(w, `<section class="panel"><h2>Context</h2>`)
		writeMeta(w, "Proposed link", "/proposed/"+uuidText(ticket.ID))
		writeMeta(w, "Ticket link", "/tickets/"+uuidText(ticket.ID))
		writeMeta(w, "Source", ticket.CreatedBy+"/"+textValue(ticket.CreatedByID))
		if ticket.CreationReason.Valid {
			writeMeta(w, "Reason", ticket.CreationReason.String)
		}
		writeList(w, "Acceptance", ticket.AcceptanceCriteria, "")
		writeList(w, "Paths", ticket.RelevantPaths, "")
		fmt.Fprint(w, `</section>`)
	})
}

func workspaceIndexPage(view workspaceIndexView) templ.Component {
	return layout("Forge Workspaces", func(w io.Writer) {
		fmt.Fprint(w, `<section class="page-head"><div><h1>Workspaces</h1><p>Minimal setup and inspection for Forge scopes.</p></div><a class="button" href="/tickets">Tickets</a></section>`)
		fmt.Fprint(w, `<section class="filters panel"><form method="post" action="/workspaces">`)
		input(w, "name", "")
		fmt.Fprint(w, `<button type="submit">Create workspace</button></form></section>`)
		if view.Message != "" {
			fmt.Fprintf(w, `<section class="panel warning"><h2>Workspace action failed</h2><p>%s</p></section>`, esc(view.Message))
		}
		if len(view.Workspaces) == 0 {
			fmt.Fprint(w, `<section class="panel empty"><h2>No workspaces yet</h2><p>Create the first workspace to scope tickets and projects.</p></section>`)
			return
		}
		fmt.Fprint(w, `<section class="ticket-list" aria-label="Workspaces">`)
		for _, workspace := range view.Workspaces {
			fmt.Fprintf(w, `<article class="ticket-card"><a href="/workspaces/%s"><span class="title">%s</span><span class="summary">%s</span></a></article>`,
				esc(uuidText(workspace.ID)),
				esc(workspace.Name),
				esc(uuidText(workspace.ID)),
			)
		}
		fmt.Fprint(w, `</section>`)
	})
}

func workspaceDetailPage(view workspaceDetailView) templ.Component {
	workspace := view.Workspace
	return layout(workspace.Name, func(w io.Writer) {
		fmt.Fprintf(w, `<section class="page-head"><div><p class="eyebrow">workspace %s</p><h1>%s</h1><p>Projects define the ticket scopes agents claim from.</p></div><a class="button" href="/workspaces">Workspaces</a></section>`,
			esc(uuidText(workspace.ID)),
			esc(workspace.Name),
		)
		fmt.Fprintf(w, `<section class="filters panel"><form method="post" action="/workspaces/%s/projects">`, esc(uuidText(workspace.ID)))
		input(w, "name", "")
		fmt.Fprint(w, `<button type="submit">Create project</button></form></section>`)
		fmt.Fprint(w, `<section class="panel"><h2>Projects</h2>`)
		if len(view.Projects) == 0 {
			fmt.Fprint(w, `<p class="empty-text">No projects in this workspace yet.</p>`)
		}
		for _, project := range view.Projects {
			fmt.Fprintf(w, `<div class="timeline-item"><strong>%s</strong><p>%s</p><p><a class="copy-link" href="/tickets?workspace_id=%s&project_id=%s">Open ticket queue</a></p></div>`,
				esc(project.Name),
				esc(uuidText(project.ID)),
				esc(uuidText(workspace.ID)),
				esc(uuidText(project.ID)),
			)
		}
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

func writeSearchResult(w io.Writer, result services.SearchResult) {
	ticket := result.Ticket
	fmt.Fprintf(w, `<article class="ticket-card"><a href="/tickets/%s"><span class="title">%s</span><span class="summary">%s %s P%d</span></a>`,
		esc(uuidText(ticket.ID)),
		esc(ticket.Title),
		esc(ticket.Status),
		esc(ticket.Type),
		ticket.Priority,
	)
	if ticket.Description != "" {
		fmt.Fprintf(w, `<p>%s</p>`, esc(ticket.Description))
	}
	if result.Snippet != "" {
		fmt.Fprintf(w, `<p class="match-snippet">%s</p>`, esc(result.Snippet))
	}
	writeList(w, "Matched", result.MatchSources, "")
	fmt.Fprint(w, `</article>`)
}

func writeShareLinks(w io.Writer, view ticketDetailView) {
	ticket := view.Ticket
	fmt.Fprint(w, `<div class="list"><h3>Share links</h3><ul>`)
	fmt.Fprintf(w, `<li><a class="copy-link" href="/tickets/%s">/tickets/%s</a></li>`, esc(uuidText(ticket.ID)), esc(uuidText(ticket.ID)))
	if isProposedTicket(ticket) {
		fmt.Fprintf(w, `<li><a class="copy-link" href="/proposed/%s">/proposed/%s</a></li>`, esc(uuidText(ticket.ID)), esc(uuidText(ticket.ID)))
	}
	for _, attempt := range view.Timeline.Attempts {
		fmt.Fprintf(w, `<li><a class="copy-link" href="/attempts/%s">/attempts/%s</a></li>`, esc(uuidText(attempt.ID)), esc(uuidText(attempt.ID)))
	}
	for _, artifact := range view.Timeline.Artifacts {
		fmt.Fprintf(w, `<li><a class="copy-link" href="/artifacts/%s">/artifacts/%s</a></li>`, esc(uuidText(artifact.ID)), esc(uuidText(artifact.ID)))
	}
	fmt.Fprint(w, `</ul></div>`)
}

func isProposedTicket(ticket db.Ticket) bool {
	return ticket.CreatedBy == services.ActorAgent && ticket.Status == services.TicketStatusBacklog
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
		if artifactURL, ok := safeArtifactURL(artifact.Url); ok {
			fmt.Fprintf(w, `<p><a href="%s">%s</a></p>`, esc(artifactURL), esc(artifactURL))
		} else if artifact.Url != "" {
			fmt.Fprint(w, `<p class="empty-text">Artifact link hidden because its URL scheme is not supported.</p>`)
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

func safeArtifactURL(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return "", false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return value, true
	default:
		return "", false
	}
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
	return `:root{font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;color:#202124;background:#f7f7f4}body{margin:0}main{max-width:1120px;margin:0 auto;padding:32px 20px 56px}.page-head{display:flex;justify-content:space-between;gap:24px;align-items:flex-start;margin-bottom:18px}.page-head h1{margin:4px 0 8px;font-size:32px;line-height:1.1;letter-spacing:0}.page-head p{margin:0;color:#5c625d;max-width:720px}.eyebrow{text-transform:uppercase;font-size:12px;font-weight:700;color:#5b6b5b}.actions{display:flex;gap:8px;align-items:center}.button,button{border:1px solid #202124;background:#202124;color:#fff;text-decoration:none;padding:9px 12px;border-radius:6px;font-weight:700;white-space:nowrap}.panel{background:#fff;border:1px solid #d9ddd5;border-radius:8px;padding:18px}.filters form{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:12px;align-items:end}.filters span,.auth-panel span{display:block;font-size:12px;font-weight:700;color:#5c625d;margin-bottom:5px;text-transform:capitalize}.filters input,.auth-panel input{width:100%;box-sizing:border-box;border:1px solid #c7ccc3;border-radius:6px;padding:9px;background:#fff}.auth-panel{max-width:360px;margin:12vh auto 0}.auth-panel form{display:grid;gap:14px}.auth-panel h1{margin-top:0}.auth-error{color:#8c2f1a;font-weight:700}.ticket-list{display:grid;gap:10px;margin-top:16px}.ticket-card{background:#fff;border:1px solid #d9ddd5;border-radius:8px;padding:14px}.ticket-card a{display:flex;justify-content:space-between;gap:16px;color:inherit;text-decoration:none}.title{font-weight:800}.summary,.meta span,.empty-text{color:#61665f}.match-snippet{color:#3d473f;background:#f2f4ef;border-left:3px solid #8aa074;padding:8px 10px}.meta{display:flex;gap:10px;align-items:baseline;margin:8px 0}.meta span{min-width:84px;font-size:12px;font-weight:700;text-transform:uppercase}.list h3{font-size:13px;margin:16px 0 6px;color:#3c463e}.list ul{margin:0;padding-left:20px}.detail-grid{display:grid;grid-template-columns:minmax(0,1fr) minmax(320px,.85fr);gap:16px}.timeline-item{border-top:1px solid #e5e8e1;padding:12px 0}.timeline-item strong{display:block}.timeline-item span{color:#61665f;font-size:13px}.warning{border-color:#d8b45f;background:#fffaf0}@media(max-width:760px){main{padding:20px 14px}.page-head,.ticket-card a{display:block}.actions{margin-top:12px}.filters form,.detail-grid{grid-template-columns:1fr}.button{display:inline-block;margin-top:12px}}`
}
