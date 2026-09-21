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
	"sync"
	"time"

	"github.com/a-h/templ"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/services"
	"github.com/vivek/agent-task-tracker/internal/storage"
)

const defaultTicketListLimit int32 = 50
const defaultSessionCookieName = "forge_admin_session"

type Runtime interface {
	ListTickets(context.Context, services.ListTicketsRequest) ([]db.Ticket, error)
	ListProposedTickets(context.Context, services.ListProposedTicketsRequest) ([]services.ProposedTicketTriageItem, error)
	SearchTickets(context.Context, services.SearchTicketsRequest) ([]services.SearchResult, error)
	ListEvents(context.Context, services.ListEventsRequest) (services.ListEventsResult, error)
	MarkReady(context.Context, services.TicketTransitionRequest) (db.Ticket, error)
	Reopen(context.Context, services.TicketTransitionRequest) (db.Ticket, error)
	Unblock(context.Context, services.TicketTransitionRequest) (db.Ticket, error)
	RequestReview(context.Context, services.TicketTransitionRequest) (db.Ticket, error)
	Archive(context.Context, services.TicketTransitionRequest) (db.Ticket, error)
	ReadyProposedTicket(context.Context, services.ProposedTicketTriageRequest) (db.Ticket, error)
	EnqueueProposedTicket(context.Context, services.ProposedTicketTriageRequest) (db.Ticket, error)
	RejectProposedTicket(context.Context, services.ProposedTicketTriageRequest) (db.Ticket, error)
	ArchiveProposedTicket(context.Context, services.ProposedTicketTriageRequest) (db.Ticket, error)
	GetTicket(context.Context, pgtype.UUID) (db.Ticket, error)
	GetAttempt(context.Context, pgtype.UUID) (db.Attempt, error)
	GetAttemptMetrics(context.Context, pgtype.UUID) (db.AttemptMetric, error)
	ListAttemptsByTicket(context.Context, pgtype.UUID) ([]db.Attempt, error)
	ListAttemptCheckpointsByTicket(context.Context, pgtype.UUID) ([]db.AttemptCheckpoint, error)
	ListTicketEventsByTicket(context.Context, pgtype.UUID) ([]db.TicketEvent, error)
	ListArtifactsByTicket(context.Context, pgtype.UUID) ([]db.Artifact, error)
	ListArtifactsByAttempt(context.Context, pgtype.UUID) ([]db.Artifact, error)
	ListArtifacts(context.Context, services.ListArtifactsRequest) ([]db.Artifact, error)
	GetArtifact(context.Context, pgtype.UUID) (db.Artifact, error)
	OpenArtifact(context.Context, db.Artifact) (storage.ArtifactContent, error)
	ArtifactContentOpenable(db.Artifact) bool
	DeleteLocalArtifact(context.Context, pgtype.UUID) (db.Artifact, error)
	ListWorkspaces(context.Context) ([]db.Workspace, error)
	GetWorkspace(context.Context, pgtype.UUID) (db.Workspace, error)
	CreateWorkspace(context.Context, string) (db.Workspace, error)
	ListProjectsByWorkspace(context.Context, pgtype.UUID) ([]db.Project, error)
	CreateProject(context.Context, pgtype.UUID, string) (db.Project, error)
}

type Handler struct {
	runtime  Runtime
	auth     AuthOptions
	throttle *loginThrottle
}

func NewHandler(runtime Runtime) http.Handler {
	return Handler{runtime: runtime, throttle: &loginThrottle{}}
}

func NewHandlerWithAuth(runtime Runtime, auth AuthOptions) http.Handler {
	return Handler{runtime: runtime, auth: auth.normalized(), throttle: &loginThrottle{}}
}

// loginThrottle is a process-wide fixed-window failure counter for the human
// login form. It is keyed globally (not per-IP) because the server commonly
// sits behind a reverse proxy where an unauthenticated X-Forwarded-For would be
// spoofable; a global limit is safe for a single-operator product.
type loginThrottle struct {
	mu          sync.Mutex
	windowStart time.Time
	failures    int
}

const (
	loginFailureLimit  = 10
	loginFailureWindow = time.Minute
)

// overLimit reports whether the current window has already reached the failure
// limit, rolling the window forward when it has expired. It never counts an
// attempt itself — only recordFailure does.
func (t *loginThrottle) overLimit(now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rollWindow(now)
	return t.failures >= loginFailureLimit
}

func (t *loginThrottle) recordFailure(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.rollWindow(now)
	t.failures++
}

func (t *loginThrottle) rollWindow(now time.Time) {
	if now.Sub(t.windowStart) >= loginFailureWindow {
		t.windowStart = now
		t.failures = 0
	}
}

type AuthOptions struct {
	AdminToken   string
	CookieName   string
	SecureCookie bool
	SessionTTL   time.Duration
	Now          func() time.Time
}

// RequireAdminToken protects machine-facing routes with the configured operator token.
// Browser session cookies are deliberately not accepted on this boundary.
func RequireAdminToken(auth AuthOptions, next http.Handler) http.Handler {
	auth = auth.normalized()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !auth.enabled() {
			next.ServeHTTP(w, r)
			return
		}
		if auth.ValidAdminToken(bearerToken(r.Header.Get("Authorization"))) || auth.ValidAdminToken(r.Header.Get("X-Forge-Admin-Token")) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="Forge API"`)
		http.Error(w, "authentication required", http.StatusUnauthorized)
	})
}

func (h Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/assets/htmx-2.0.4.min.js" {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		_, _ = w.Write(htmxAsset)
		return
	}
	if r.URL.Path == "/favicon.ico" {
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		_, _ = w.Write(faviconAsset)
		return
	}
	if r.URL.Path == "/" {
		// Send the bare root to the workspace index, which itself bounces to
		// /login when the request is unauthenticated.
		http.Redirect(w, r, "/workspaces", http.StatusSeeOther)
		return
	}
	if h.auth.enabled() {
		switch r.URL.Path {
		case "/login":
			h.handleLogin(w, r)
			return
		case "/logout":
			h.handleLogout(w, r)
			return
		}
		if !h.isAuthorized(r) {
			h.requireLogin(w, r)
			return
		}
		if h.isCookieAuthorized(r) && unsafeMethod(r.Method) && !sameOrigin(r) {
			renderStatus(r.Context(), w, http.StatusForbidden, "Request rejected", "Cookie-authenticated changes require a same-origin request.")
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), signedInContextKey{}, true))
	}
	switch {
	case r.URL.Path == "/tickets/new":
		h.renderNewTicket(w, r)
	case r.URL.Path == "/tickets":
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		h.renderTicketList(w, r)
	case r.URL.Path == "/search":
		h.renderSearch(w, r)
	case r.URL.Path == "/events":
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		h.renderEventLedger(w, r)
	case r.URL.Path == "/artifacts":
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		h.renderArtifactList(w, r)
	case r.URL.Path == "/proposed" || r.URL.Path == "/proposed/":
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		h.renderProposedList(w, r)
	case strings.HasPrefix(r.URL.Path, "/tickets/"):
		h.renderTicketRoute(w, r)
	case strings.HasPrefix(r.URL.Path, "/attempts/"):
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		h.renderAttemptDetail(w, r)
	case strings.HasPrefix(r.URL.Path, "/artifacts/"):
		h.renderArtifactRoute(w, r)
	case strings.HasPrefix(r.URL.Path, "/proposed/"):
		h.renderProposedRoute(w, r)
	case r.URL.Path == "/workspaces":
		h.renderWorkspaceIndex(w, r)
	case strings.HasPrefix(r.URL.Path, "/workspaces/"):
		h.renderWorkspaceRoute(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		renderStatus(r.Context(), w, http.StatusMethodNotAllowed, "Method not allowed", "Logout accepts POST requests.")
		return
	}
	if !h.isAuthorized(r) {
		h.requireLogin(w, r)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: h.auth.cookieName(), Value: "", Path: "/", HttpOnly: true, Secure: h.auth.SecureCookie, MaxAge: -1, Expires: time.Unix(1, 0)})
	http.Redirect(w, r, "/login", http.StatusSeeOther)
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
		now := h.auth.now()
		if h.throttle != nil && h.throttle.overLimit(now) {
			renderComponent(r.Context(), w, http.StatusTooManyRequests, loginPage(next, "Too many failed login attempts. Try again shortly."))
			return
		}
		if !constantTimeTokenEqual(r.FormValue("admin_token"), h.auth.AdminToken) {
			if h.throttle != nil {
				h.throttle.recordFailure(now)
			}
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
	if h.auth.ValidAdminToken(bearerToken(r.Header.Get("Authorization"))) {
		return true
	}
	if h.auth.ValidAdminToken(r.Header.Get("X-Forge-Admin-Token")) {
		return true
	}
	return h.isCookieAuthorized(r)
}

func (h Handler) isCookieAuthorized(r *http.Request) bool {
	cookie, err := r.Cookie(h.auth.cookieName())
	return err == nil && h.auth.validSessionValue(cookie.Value)
}

func unsafeMethod(method string) bool {
	return method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions
}

func sameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return false
	}
	parsed, err := url.Parse(origin)
	return err == nil && parsed.Host != "" && strings.EqualFold(parsed.Host, r.Host)
}

// ValidAdminToken compares a supplied operator token in constant time.
func (a AuthOptions) ValidAdminToken(token string) bool {
	return constantTimeTokenEqual(token, a.normalized().AdminToken)
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
		return "/workspaces"
	}
	return value
}

func (h Handler) renderTicketList(w http.ResponseWriter, r *http.Request) {
	if h.runtime == nil {
		renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Runtime unavailable", "runtime is not configured")
		return
	}
	scope, err := h.loadScopeOptions(r.Context(), r.URL.Query().Get("workspace_id"))
	if err != nil {
		renderStatus(r.Context(), w, http.StatusInternalServerError, "Unable to load workspaces", "Refresh to try again.")
		return
	}
	req, err := listTicketsRequestFromQuery(r)
	if err != nil {
		query := r.URL.Query()
		workspaceID, _ := parseOptionalUUID(query.Get("workspace_id"))
		projectID, _ := parseOptionalUUID(query.Get("project_id"))
		status, message := scopeFormError(r, err)
		renderComponent(r.Context(), w, status, ticketListPage(ticketListView{
			Scope:       scope,
			WorkspaceID: workspaceID,
			ProjectID:   projectID,
			Status:      strings.TrimSpace(query.Get("status")),
			Type:        strings.TrimSpace(query.Get("type")),
			Message:     message,
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
		Scope:       scope,
		Tickets:     tickets,
		WorkspaceID: req.WorkspaceID,
		ProjectID:   req.ProjectID,
		Status:      req.Status,
		Type:        req.Type,
		Offset:      req.Offset,
		Limit:       req.Limit,
	}))
}

func (h Handler) renderSearch(w http.ResponseWriter, r *http.Request) {
	if h.runtime == nil {
		renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Runtime unavailable", "runtime is not configured")
		return
	}
	scope, err := h.loadScopeOptions(r.Context(), r.URL.Query().Get("workspace_id"))
	if err != nil {
		renderStatus(r.Context(), w, http.StatusInternalServerError, "Unable to load workspaces", "Refresh to try again.")
		return
	}
	req, err := searchTicketsRequestFromQuery(r)
	if err != nil {
		query := r.URL.Query()
		status, message := scopeFormError(r, err)
		renderComponent(r.Context(), w, status, searchPage(searchView{
			Scope:           scope,
			WorkspaceIDText: strings.TrimSpace(query.Get("workspace_id")),
			ProjectIDText:   strings.TrimSpace(query.Get("project_id")),
			Query:           strings.TrimSpace(query.Get("q")),
			Message:         message,
		}))
		return
	}
	results, err := h.runtime.SearchTickets(r.Context(), req)
	if err != nil {
		var validationErr services.ValidationError
		if errors.As(err, &validationErr) {
			renderComponent(r.Context(), w, http.StatusBadRequest, searchPage(searchView{
				Scope:           scope,
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
		Scope:           scope,
		Results:         results,
		WorkspaceIDText: uuidText(req.WorkspaceID),
		ProjectIDText:   uuidText(req.ProjectID),
		Query:           req.Query,
	}))
}

func (h Handler) renderEventLedger(w http.ResponseWriter, r *http.Request) {
	if h.runtime == nil {
		renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Runtime unavailable", "runtime is not configured")
		return
	}
	scope, err := h.loadScopeOptions(r.Context(), r.URL.Query().Get("workspace_id"))
	if err != nil {
		renderStatus(r.Context(), w, http.StatusInternalServerError, "Unable to load workspaces", "Refresh to try again.")
		return
	}
	req, err := listEventsRequestFromQuery(r)
	if err != nil {
		view := eventLedgerViewFromQuery(r, err.Error())
		view.Scope = scope
		renderComponent(r.Context(), w, http.StatusBadRequest, eventLedgerPage(view))
		return
	}
	result, err := h.runtime.ListEvents(r.Context(), req)
	if err != nil {
		var validationErr services.ValidationError
		if errors.As(err, &validationErr) {
			renderComponent(r.Context(), w, http.StatusBadRequest, eventLedgerPage(eventLedgerView{
				Scope:           scope,
				WorkspaceIDText: uuidText(req.WorkspaceID),
				ProjectIDText:   uuidText(req.ProjectID),
				TicketIDText:    uuidText(req.TicketID),
				AttemptIDText:   uuidText(req.AttemptID),
				Cursor:          req.Cursor,
				LimitText:       limitText(req.Limit),
				Message:         validationErr.Error(),
			}))
			return
		}
		renderStatus(r.Context(), w, http.StatusInternalServerError, "Unable to load event ledger", err.Error())
		return
	}
	// Display newest-first to match the "Recent" framing. The service keeps its
	// ascending order and cursor semantics; we only reverse a copy for display.
	events := make([]db.TicketEvent, len(result.Events))
	for i, event := range result.Events {
		events[len(result.Events)-1-i] = event
	}
	renderComponent(r.Context(), w, http.StatusOK, eventLedgerPage(eventLedgerView{
		Scope:           scope,
		Events:          events,
		TicketTitles:    h.eventTicketTitles(r.Context(), events),
		NextCursor:      result.NextCursor,
		WorkspaceIDText: uuidText(req.WorkspaceID),
		ProjectIDText:   uuidText(req.ProjectID),
		TicketIDText:    uuidText(req.TicketID),
		AttemptIDText:   uuidText(req.AttemptID),
		Cursor:          req.Cursor,
		LimitText:       limitText(req.Limit),
	}))
}

// eventTicketTitles resolves each distinct ticket referenced by the events to
// its title so the ledger can attribute entries by name. A failed lookup is
// skipped (the card falls back to a plain "Ticket" link).
func (h Handler) eventTicketTitles(ctx context.Context, events []db.TicketEvent) map[string]string {
	titles := make(map[string]string)
	for _, event := range events {
		if !event.TicketID.Valid {
			continue
		}
		key := uuidText(event.TicketID)
		if _, seen := titles[key]; seen {
			continue
		}
		ticket, err := h.runtime.GetTicket(ctx, event.TicketID)
		if err != nil {
			continue
		}
		titles[key] = ticket.Title
	}
	return titles
}

func (h Handler) renderArtifactList(w http.ResponseWriter, r *http.Request) {
	if h.runtime == nil {
		renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Runtime unavailable", "runtime is not configured")
		return
	}
	scope, err := h.loadScopeOptions(r.Context(), r.URL.Query().Get("workspace_id"))
	if err != nil {
		renderStatus(r.Context(), w, http.StatusInternalServerError, "Unable to load workspaces", "Refresh to try again.")
		return
	}
	req, err := listArtifactsRequestFromQuery(r)
	if err != nil {
		query := r.URL.Query()
		status, message := scopeFormError(r, err)
		renderComponent(r.Context(), w, status, artifactListPage(artifactListView{
			Scope:           scope,
			WorkspaceIDText: strings.TrimSpace(query.Get("workspace_id")),
			ProjectIDText:   strings.TrimSpace(query.Get("project_id")),
			TicketIDText:    strings.TrimSpace(query.Get("ticket_id")),
			Message:         message,
		}))
		return
	}
	artifacts, err := h.runtime.ListArtifacts(r.Context(), req)
	if err != nil {
		var validationErr services.ValidationError
		if errors.As(err, &validationErr) {
			renderComponent(r.Context(), w, http.StatusBadRequest, artifactListPage(artifactListView{
				Scope:           scope,
				WorkspaceIDText: uuidText(req.WorkspaceID),
				ProjectIDText:   uuidText(req.ProjectID),
				TicketIDText:    uuidText(req.TicketID),
				Message:         validationErr.Error(),
			}))
			return
		}
		renderStatus(r.Context(), w, http.StatusInternalServerError, "Unable to load artifacts", err.Error())
		return
	}
	renderComponent(r.Context(), w, http.StatusOK, artifactListPage(artifactListView{
		Scope:           scope,
		Artifacts:       artifacts,
		WorkspaceIDText: uuidText(req.WorkspaceID),
		ProjectIDText:   uuidText(req.ProjectID),
		TicketIDText:    uuidText(req.TicketID),
		Offset:          req.Offset,
		Limit:           req.Limit,
	}))
}

func (h Handler) renderProposedList(w http.ResponseWriter, r *http.Request) {
	if h.runtime == nil {
		renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Runtime unavailable", "runtime is not configured")
		return
	}
	scope, err := h.loadScopeOptions(r.Context(), r.URL.Query().Get("workspace_id"))
	if err != nil {
		renderStatus(r.Context(), w, http.StatusInternalServerError, "Unable to load workspaces", "Refresh to try again.")
		return
	}
	req, err := listProposedRequestFromQuery(r)
	if err != nil {
		query := r.URL.Query()
		status, message := scopeFormError(r, err)
		renderComponent(r.Context(), w, status, proposedListPage(proposedListView{
			Scope:           scope,
			WorkspaceIDText: strings.TrimSpace(query.Get("workspace_id")),
			ProjectIDText:   strings.TrimSpace(query.Get("project_id")),
			Type:            strings.TrimSpace(query.Get("type")),
			Message:         message,
		}))
		return
	}
	items, err := h.runtime.ListProposedTickets(r.Context(), req)
	if err != nil {
		var validationErr services.ValidationError
		if errors.As(err, &validationErr) {
			renderComponent(r.Context(), w, http.StatusBadRequest, proposedListPage(proposedListView{
				Scope:           scope,
				WorkspaceIDText: uuidText(req.WorkspaceID),
				ProjectIDText:   uuidText(req.ProjectID),
				Type:            req.Type,
				Message:         validationErr.Error(),
			}))
			return
		}
		renderStatus(r.Context(), w, http.StatusInternalServerError, "Unable to load proposed work", err.Error())
		return
	}
	renderComponent(r.Context(), w, http.StatusOK, proposedListPage(proposedListView{
		Scope:           scope,
		Items:           items,
		WorkspaceIDText: uuidText(req.WorkspaceID),
		ProjectIDText:   uuidText(req.ProjectID),
		Type:            req.Type,
	}))
}

func (h Handler) renderTicketRoute(w http.ResponseWriter, r *http.Request) {
	ticketID, action, err := parseTicketRoute(r.URL.Path)
	if err != nil {
		renderStatus(r.Context(), w, http.StatusBadRequest, "Invalid ticket id", "ticket id must be a UUID")
		return
	}
	if action == "" {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		h.renderTicketDetail(w, r, ticketID)
		return
	}
	if !isTicketAction(action) {
		http.NotFound(w, r)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	h.transitionTicket(w, r, ticketID, action)
}

func (h Handler) renderTicketDetail(w http.ResponseWriter, r *http.Request, ticketID pgtype.UUID) {
	if h.runtime == nil {
		renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Runtime unavailable", "runtime is not configured")
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
		Ticket:                  ticket,
		Timeline:                timeline,
		TimelineErr:             timelineErr,
		ArtifactContentOpenable: artifactContentOpenability(h.runtime, timeline.Artifacts),
	}))
}

func (h Handler) transitionTicket(w http.ResponseWriter, r *http.Request, ticketID pgtype.UUID, action string) {
	if h.runtime == nil {
		renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Runtime unavailable", "runtime is not configured")
		return
	}
	if err := r.ParseForm(); err != nil {
		renderStatus(r.Context(), w, http.StatusBadRequest, "Unable to read ticket action form", err.Error())
		return
	}
	req := services.TicketTransitionRequest{
		TicketID:  ticketID,
		ActorType: services.ActorHuman,
		ActorID:   "web",
		Reason:    strings.TrimSpace(r.FormValue("reason")),
	}
	var (
		ticket db.Ticket
		err    error
	)
	switch action {
	case "ready":
		ticket, err = h.runtime.MarkReady(r.Context(), req)
	case "reopen":
		ticket, err = h.runtime.Reopen(r.Context(), req)
	case "unblock":
		ticket, err = h.runtime.Unblock(r.Context(), req)
	case "request-review":
		ticket, err = h.runtime.RequestReview(r.Context(), req)
	case "approve", "request-changes":
		reviewer, ok := h.runtime.(interface {
			Review(context.Context, services.ReviewTicketRequest) (db.Ticket, error)
		})
		if !ok {
			renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Review unavailable", "The review runtime is not configured.")
			return
		}
		decision := services.ReviewDecisionApprove
		if action == "request-changes" {
			decision = services.ReviewDecisionReject
		}
		ticket, err = reviewer.Review(r.Context(), services.ReviewTicketRequest{TicketID: ticketID, ActorType: req.ActorType, ActorID: req.ActorID, Reason: req.Reason, Decision: decision})
	case "archive":
		ticket, err = h.runtime.Archive(r.Context(), req)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		renderTicketServiceError(r.Context(), w, err, "Unable to update ticket")
		return
	}
	http.Redirect(w, r, "/tickets/"+uuidText(ticket.ID), http.StatusSeeOther)
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
	ticketCheckpoints, err := h.runtime.ListAttemptCheckpointsByTicket(r.Context(), attempt.TicketID)
	if err != nil {
		renderStatus(r.Context(), w, http.StatusInternalServerError, "Unable to load attempt checkpoints", err.Error())
		return
	}
	checkpoints := make([]db.AttemptCheckpoint, 0, len(ticketCheckpoints))
	for _, checkpoint := range ticketCheckpoints {
		if checkpoint.AttemptID == attemptID {
			checkpoints = append(checkpoints, checkpoint)
		}
	}
	// Metrics are optional: an attempt that never reported them has no row.
	metrics, err := h.runtime.GetAttemptMetrics(r.Context(), attemptID)
	hasMetrics := true
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			renderStatus(r.Context(), w, http.StatusInternalServerError, "Unable to load attempt metrics", err.Error())
			return
		}
		hasMetrics = false
	}
	renderComponent(r.Context(), w, http.StatusOK, attemptDetailPage(attempt, artifacts, checkpoints, metrics, hasMetrics))
}

func (h Handler) renderArtifactRoute(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/artifacts/")
	parts := strings.Split(rest, "/")
	if len(parts) == 2 && parts[1] == "content" {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		h.renderArtifactContent(w, r, parts[0])
		return
	}
	if len(parts) == 2 && parts[1] == "delete" {
		if !requireMethod(w, r, http.MethodPost) {
			return
		}
		h.deleteArtifact(w, r, parts[0])
		return
	}
	if len(parts) != 1 {
		http.NotFound(w, r)
		return
	}
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	h.renderArtifactDetail(w, r, parts[0])
}

func (h Handler) renderArtifactDetail(w http.ResponseWriter, r *http.Request, idText string) {
	if h.runtime == nil {
		renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Runtime unavailable", "runtime is not configured")
		return
	}
	artifactID, err := parseUUID(idText)
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
	renderComponent(r.Context(), w, http.StatusOK, artifactDetailPage(artifact, h.runtime.ArtifactContentOpenable(artifact)))
}

func (h Handler) renderArtifactContent(w http.ResponseWriter, r *http.Request, idText string) {
	if h.runtime == nil {
		renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Runtime unavailable", "runtime is not configured")
		return
	}
	artifactID, err := parseUUID(idText)
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
	content, err := h.runtime.OpenArtifact(r.Context(), artifact)
	if err != nil {
		renderStatus(r.Context(), w, http.StatusNotFound, "Artifact content unavailable", err.Error())
		return
	}
	defer content.Reader.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, headerFilename(content.Name)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if content.Size >= 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(content.Size, 10))
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, content.Reader)
}

func (h Handler) deleteArtifact(w http.ResponseWriter, r *http.Request, idText string) {
	if h.runtime == nil {
		renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Runtime unavailable", "runtime is not configured")
		return
	}
	artifactID, err := parseUUID(idText)
	if err != nil {
		renderStatus(r.Context(), w, http.StatusBadRequest, "Invalid artifact id", "artifact id must be a UUID")
		return
	}
	artifact, err := h.runtime.DeleteLocalArtifact(r.Context(), artifactID)
	if err != nil {
		if errors.Is(err, services.ErrArtifactDeleteUnsupported) {
			renderStatus(r.Context(), w, http.StatusConflict, "Only local artifacts can be deleted", "Remote artifact metadata is retained because Forge cannot safely clean up the backing object yet.")
			return
		}
		if errors.Is(err, pgx.ErrNoRows) {
			renderStatus(r.Context(), w, http.StatusNotFound, "Artifact not found", "No artifact exists for that id.")
			return
		}
		renderStatus(r.Context(), w, http.StatusInternalServerError, "Unable to delete artifact", err.Error())
		return
	}
	http.Redirect(w, r, artifactListPath(artifact), http.StatusSeeOther)
}

func (h Handler) renderProposedRoute(w http.ResponseWriter, r *http.Request) {
	ticketID, action, err := parseProposedRoute(r.URL.Path)
	if err != nil {
		renderStatus(r.Context(), w, http.StatusBadRequest, "Invalid proposed ticket route", "proposed routes must include a ticket UUID")
		return
	}
	if action == "" {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		h.renderProposedDetail(w, r, ticketID)
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	h.triageProposedTicket(w, r, ticketID, action)
}

func (h Handler) renderProposedDetail(w http.ResponseWriter, r *http.Request, ticketID pgtype.UUID) {
	if h.runtime == nil {
		renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Runtime unavailable", "runtime is not configured")
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

func (h Handler) triageProposedTicket(w http.ResponseWriter, r *http.Request, ticketID pgtype.UUID, action string) {
	if h.runtime == nil {
		renderStatus(r.Context(), w, http.StatusServiceUnavailable, "Runtime unavailable", "runtime is not configured")
		return
	}
	if err := r.ParseForm(); err != nil {
		renderStatus(r.Context(), w, http.StatusBadRequest, "Unable to read triage form", err.Error())
		return
	}
	req := services.ProposedTicketTriageRequest{
		TicketID:  ticketID,
		ActorType: defaultString(r.FormValue("actor_type"), services.ActorHuman),
		ActorID:   strings.TrimSpace(r.FormValue("actor_id")),
		Reason:    strings.TrimSpace(r.FormValue("reason")),
	}
	var (
		ticket db.Ticket
		err    error
	)
	switch action {
	case "ready":
		ticket, err = h.runtime.ReadyProposedTicket(r.Context(), req)
	case "enqueue":
		ticket, err = h.runtime.EnqueueProposedTicket(r.Context(), req)
	case "reject":
		ticket, err = h.runtime.RejectProposedTicket(r.Context(), req)
	case "archive":
		ticket, err = h.runtime.ArchiveProposedTicket(r.Context(), req)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		renderTicketServiceError(r.Context(), w, err, "Unable to triage proposed work")
		return
	}
	http.Redirect(w, r, "/tickets/"+uuidText(ticket.ID), http.StatusSeeOther)
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

func renderTicketServiceError(ctx context.Context, w http.ResponseWriter, err error, title string) {
	var validationErr services.ValidationError
	switch {
	case errors.As(err, &validationErr):
		renderStatus(ctx, w, http.StatusBadRequest, title, validationErr.Error())
	case errors.Is(err, services.ErrTicketIsNotProposed), errors.Is(err, services.ErrTicketNotFound), errors.Is(err, pgx.ErrNoRows):
		renderStatus(ctx, w, http.StatusNotFound, title, err.Error())
	case errors.Is(err, services.ErrTicketTransitionNotAllowed):
		renderStatus(ctx, w, http.StatusConflict, title, err.Error())
	case errors.Is(err, services.ErrEnqueuePermissionRequired):
		renderStatus(ctx, w, http.StatusForbidden, title, err.Error())
	default:
		renderStatus(ctx, w, http.StatusInternalServerError, title, err.Error())
	}
}

type ticketListView struct {
	Scope       scopeOptions
	Tickets     []db.Ticket
	WorkspaceID pgtype.UUID
	ProjectID   pgtype.UUID
	Status      string
	Type        string
	Message     string
	Offset      int32
	Limit       int32
}

type ticketDetailView struct {
	Ticket                  db.Ticket
	Timeline                ticketTimeline
	TimelineErr             error
	ArtifactContentOpenable map[string]bool
}

type searchView struct {
	Scope           scopeOptions
	Results         []services.SearchResult
	WorkspaceIDText string
	ProjectIDText   string
	Query           string
	Message         string
	Offset          int32
	Limit           int32
}

type eventLedgerView struct {
	Scope           scopeOptions
	Events          []db.TicketEvent
	TicketTitles    map[string]string
	NextCursor      string
	WorkspaceIDText string
	ProjectIDText   string
	TicketIDText    string
	AttemptIDText   string
	Cursor          string
	LimitText       string
	Message         string
}

type artifactListView struct {
	Scope           scopeOptions
	Artifacts       []db.Artifact
	WorkspaceIDText string
	ProjectIDText   string
	TicketIDText    string
	Message         string
	Offset          int32
	Limit           int32
}

type proposedListView struct {
	Scope           scopeOptions
	Items           []services.ProposedTicketTriageItem
	WorkspaceIDText string
	ProjectIDText   string
	Type            string
	Message         string
}

type ticketTimeline struct {
	Attempts    []db.Attempt
	Checkpoints []db.AttemptCheckpoint
	Events      []db.TicketEvent
	Artifacts   []db.Artifact
	Errors      []string
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
	timeline := ticketTimeline{}
	attempts, err := runtime.ListAttemptsByTicket(ctx, ticketID)
	if err != nil {
		timeline.Errors = append(timeline.Errors, "Attempts could not be loaded. Refresh this page to retry.")
	} else {
		timeline.Attempts = attempts
	}
	events, err := runtime.ListTicketEventsByTicket(ctx, ticketID)
	if err != nil {
		timeline.Errors = append(timeline.Errors, "Event history could not be loaded. Refresh this page to retry.")
	} else {
		timeline.Events = events
	}
	artifacts, err := runtime.ListArtifactsByTicket(ctx, ticketID)
	if err != nil {
		timeline.Errors = append(timeline.Errors, "Proof artifacts could not be loaded. Refresh this page to retry.")
	} else {
		timeline.Artifacts = artifacts
	}
	checkpoints, err := runtime.ListAttemptCheckpointsByTicket(ctx, ticketID)
	if err != nil {
		timeline.Errors = append(timeline.Errors, "Checkpoints could not be loaded. Refresh this page to retry.")
	} else {
		timeline.Checkpoints = checkpoints
	}
	return timeline, nil
}

func listEventsRequestFromQuery(r *http.Request) (services.ListEventsRequest, error) {
	query := r.URL.Query()
	workspaceID, err := parseOptionalUUID(query.Get("workspace_id"))
	if err != nil {
		return services.ListEventsRequest{}, errors.New("workspace_id must be a UUID")
	}
	projectID, err := parseOptionalUUID(query.Get("project_id"))
	if err != nil {
		return services.ListEventsRequest{}, errors.New("project_id must be a UUID")
	}
	ticketID, err := parseOptionalUUID(query.Get("ticket_id"))
	if err != nil {
		return services.ListEventsRequest{}, errors.New("ticket_id must be a UUID")
	}
	attemptID, err := parseOptionalUUID(query.Get("attempt_id"))
	if err != nil {
		return services.ListEventsRequest{}, errors.New("attempt_id must be a UUID")
	}
	limit := int32(0)
	if value := strings.TrimSpace(query.Get("limit")); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 32)
		if err != nil || parsed < 0 {
			return services.ListEventsRequest{}, errors.New("limit must be a non-negative integer")
		}
		limit = int32(parsed)
	}
	return services.ListEventsRequest{
		WorkspaceID: workspaceID,
		ProjectID:   projectID,
		TicketID:    ticketID,
		AttemptID:   attemptID,
		Cursor:      strings.TrimSpace(query.Get("cursor")),
		Limit:       limit,
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

func listArtifactsRequestFromQuery(r *http.Request) (services.ListArtifactsRequest, error) {
	query := r.URL.Query()
	workspaceID, err := parseUUID(query.Get("workspace_id"))
	if err != nil {
		return services.ListArtifactsRequest{}, errors.New("workspace_id and project_id are required")
	}
	projectID, err := parseUUID(query.Get("project_id"))
	if err != nil {
		return services.ListArtifactsRequest{}, errors.New("workspace_id and project_id are required")
	}
	var ticketID pgtype.UUID
	if value := strings.TrimSpace(query.Get("ticket_id")); value != "" {
		ticketID, err = parseUUID(value)
		if err != nil {
			return services.ListArtifactsRequest{}, errors.New("ticket_id must be a UUID")
		}
	}
	limit := int32(0)
	if value := strings.TrimSpace(query.Get("limit")); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 32)
		if err != nil || parsed < 0 {
			return services.ListArtifactsRequest{}, errors.New("limit must be a non-negative integer")
		}
		limit = int32(parsed)
	}
	offset := int32(0)
	if value := strings.TrimSpace(query.Get("offset")); value != "" {
		parsed, err := strconv.ParseInt(value, 10, 32)
		if err != nil || parsed < 0 {
			return services.ListArtifactsRequest{}, errors.New("offset must be a non-negative integer")
		}
		offset = int32(parsed)
	}
	return services.ListArtifactsRequest{
		WorkspaceID: workspaceID,
		ProjectID:   projectID,
		TicketID:    ticketID,
		Limit:       limit,
		Offset:      offset,
	}, nil
}

func listProposedRequestFromQuery(r *http.Request) (services.ListProposedTicketsRequest, error) {
	query := r.URL.Query()
	workspaceID, err := parseUUID(query.Get("workspace_id"))
	if err != nil {
		return services.ListProposedTicketsRequest{}, fmt.Errorf("workspace_id is required")
	}
	projectID, err := parseUUID(query.Get("project_id"))
	if err != nil {
		return services.ListProposedTicketsRequest{}, fmt.Errorf("project_id is required")
	}
	limit := int32(50)
	if raw := strings.TrimSpace(query.Get("limit")); raw != "" {
		value, err := strconv.ParseInt(raw, 10, 32)
		if err != nil {
			return services.ListProposedTicketsRequest{}, fmt.Errorf("limit must be an integer")
		}
		limit = int32(value)
	}
	return services.ListProposedTicketsRequest{
		WorkspaceID: workspaceID,
		ProjectID:   projectID,
		Type:        strings.TrimSpace(query.Get("type")),
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

func parseOptionalUUID(value string) (pgtype.UUID, error) {
	if strings.TrimSpace(value) == "" {
		return pgtype.UUID{}, nil
	}
	return parseUUID(value)
}

func parseIDFromPath(path string, prefix string) (pgtype.UUID, error) {
	idText := strings.TrimPrefix(path, prefix)
	if strings.Contains(idText, "/") || strings.TrimSpace(idText) == "" {
		return pgtype.UUID{}, errors.New("invalid route id")
	}
	return parseUUID(idText)
}

func parseTicketRoute(path string) (pgtype.UUID, string, error) {
	rest := strings.TrimPrefix(path, "/tickets/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || len(parts) > 2 || strings.TrimSpace(parts[0]) == "" {
		return pgtype.UUID{}, "", errors.New("invalid ticket route")
	}
	id, err := parseUUID(parts[0])
	if err != nil {
		return pgtype.UUID{}, "", err
	}
	if len(parts) == 1 {
		return id, "", nil
	}
	action := strings.TrimSpace(parts[1])
	if action == "" {
		return pgtype.UUID{}, "", errors.New("invalid ticket action")
	}
	return id, action, nil
}

func parseProposedRoute(path string) (pgtype.UUID, string, error) {
	rest := strings.TrimPrefix(path, "/proposed/")
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || len(parts) > 2 || strings.TrimSpace(parts[0]) == "" {
		return pgtype.UUID{}, "", errors.New("invalid proposed route")
	}
	id, err := parseUUID(parts[0])
	if err != nil {
		return pgtype.UUID{}, "", err
	}
	if len(parts) == 1 {
		return id, "", nil
	}
	action := strings.TrimSpace(parts[1])
	if action == "" {
		return pgtype.UUID{}, "", errors.New("invalid proposed action")
	}
	return id, action, nil
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
		fmt.Fprintf(w, `<section class="panel" role="alert" aria-live="assertive"><h1>%s</h1><p>%s</p><p><a href="/tickets">Back to tickets</a></p></section>`, esc(title), esc(message))
	})
}

func loginPage(next string, message string) templ.Component {
	return layout("Forge Login", func(w io.Writer) {
		fmt.Fprint(w, `<section class="auth-panel panel"><p class="eyebrow">Your agent work, in one place</p><h1>Forge Login</h1><p class="empty-text">Sign in to follow progress, resolve blockers, and review the evidence.</p>`)
		if message != "" {
			fmt.Fprintf(w, `<p class="auth-error" role="alert">%s</p>`, esc(message))
		}
		fmt.Fprint(w, `<form method="post" action="/login" hx-boost="false">`)
		fmt.Fprintf(w, `<input type="hidden" name="next" value="%s">`, esc(sanitizeNext(next)))
		fmt.Fprint(w, `<label><span>Admin token</span><input type="password" name="admin_token" autocomplete="current-password" autofocus required aria-required="true"></label>`)
		fmt.Fprint(w, `<button type="submit">Sign in</button></form></section>`)
	})
}

func ticketListPage(view ticketListView) templ.Component {
	return layoutWithPage(pageContext{Title: "Forge Tickets", ActiveRoute: "tickets", WorkspaceID: uuidText(view.WorkspaceID), ProjectID: uuidText(view.ProjectID)}, func(w io.Writer) {
		workspaceID, projectID := uuidText(view.WorkspaceID), uuidText(view.ProjectID)
		fmt.Fprint(w, `<section class="page-head"><div><p class="eyebrow">Your execution workspace</p><h1>Tickets</h1><p>Move work forward. Know what needs your attention.</p></div>`)
		fmt.Fprintf(w, `<div class="actions"><a class="button secondary" href="%s">Refresh</a><a class="button" href="%s">New ticket</a></div></section>`, esc(scopedPagePath("/tickets", workspaceID, projectID)), esc(scopedPagePath("/tickets/new", workspaceID, projectID)))
		fmt.Fprint(w, `<section class="filters panel"><form method="get" action="/tickets">`)
		writeScopeFields(w, view.Scope, workspaceID, projectID)
		writeChoiceField(w, "status", view.Status, "All statuses", ticketStatuses)
		writeChoiceField(w, "type", view.Type, "All types", ticketTypes)
		fmt.Fprint(w, `<button class="secondary" type="submit">Apply</button></form></section>`)
		if view.Message != "" {
			fmt.Fprintf(w, `<section class="panel empty"><p class="eyebrow">Start here</p><h2>Choose your project</h2><p>%s</p><a class="button secondary" href="/workspaces">Manage workspaces</a></section>`, esc(view.Message))
			return
		}
		writeQueueTabs(w, view)
		if len(view.Tickets) == 0 {
			if view.Status != "" || view.Type != "" || view.Offset > 0 {
				fmt.Fprintf(w, `<section class="panel empty"><h2>No tickets match</h2><p>Try a different status or type, or return to the full queue.</p><a class="button secondary" href="%s">Clear filters</a></section>`, esc(scopedPagePath("/tickets", workspaceID, projectID)))
			} else {
				fmt.Fprintf(w, `<section class="panel empty"><p class="eyebrow">Ready when you are</p><h2>Your first ticket starts here</h2><p>Describe a small, verifiable piece of work. An agent can claim it, record progress, and leave evidence for you to review.</p><a class="button" href="%s">Create your first ticket</a></section>`, esc(scopedPagePath("/tickets/new", workspaceID, projectID)))
			}
			return
		}
		writeAgentSetup(w, workspaceID, projectID)
		fmt.Fprint(w, `<section class="ticket-list" aria-label="Tickets">`)
		for _, ticket := range view.Tickets {
			writeTicketCard(w, ticket)
		}
		fmt.Fprint(w, `</section>`)
		limit := view.Limit
		if limit <= 0 {
			limit = defaultTicketListLimit
		}
		writeOffsetPager(w, ticketListPagePath(view, view.Offset-limit), ticketListPagePath(view, view.Offset+limit), view.Offset, len(view.Tickets), limit)
	})
}

func searchPage(view searchView) templ.Component {
	return layoutWithPage(pageContext{Title: "Forge Search", ActiveRoute: "search", WorkspaceID: view.WorkspaceIDText, ProjectID: view.ProjectIDText}, func(w io.Writer) {
		fmt.Fprint(w, `<section class="page-head"><div><h1>Forge Search</h1><p>Find tickets through titles, descriptions, attempts, events, and proof artifacts.</p></div>`)
		fmt.Fprintf(w, `<a class="button" href="/tickets?workspace_id=%s&project_id=%s">Tickets</a></section>`, esc(view.WorkspaceIDText), esc(view.ProjectIDText))
		fmt.Fprint(w, `<section class="filters panel"><form method="get" action="/search">`)
		writeScopeFields(w, view.Scope, view.WorkspaceIDText, view.ProjectIDText)
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

func eventLedgerPage(view eventLedgerView) templ.Component {
	return layoutWithPage(pageContext{Title: "Forge Activity", ActiveRoute: "events", WorkspaceID: view.WorkspaceIDText, ProjectID: view.ProjectIDText}, func(w io.Writer) {
		fmt.Fprint(w, `<section class="page-head"><div><p class="eyebrow">Execution Ledger</p><h1>Activity</h1><p>Recent ticket events, agent checkpoints, proof handoffs, and status transitions in one calm inspection stream.</p></div>`)
		fmt.Fprint(w, `<div class="actions">`)
		if view.WorkspaceIDText != "" && view.ProjectIDText != "" {
			fmt.Fprintf(w, `<a class="button secondary" href="/tickets?workspace_id=%s&project_id=%s">Tickets</a>`, esc(view.WorkspaceIDText), esc(view.ProjectIDText))
		} else {
			fmt.Fprint(w, `<a class="button secondary" href="/tickets">Tickets</a>`)
		}
		fmt.Fprint(w, `</div></section>`)
		fmt.Fprint(w, `<section class="filters panel"><form method="get" action="/events">`)
		writeScopeFields(w, view.Scope, view.WorkspaceIDText, view.ProjectIDText)
		input(w, "ticket_id", view.TicketIDText)
		input(w, "attempt_id", view.AttemptIDText)
		input(w, "limit", view.LimitText)
		fmt.Fprint(w, `<button type="submit">Filter</button></form></section>`)
		if view.Message != "" {
			fmt.Fprintf(w, `<section class="panel empty"><h2>Event ledger needs valid filters</h2><p>%s</p></section>`, esc(view.Message))
			return
		}
		if len(view.Events) == 0 {
			fmt.Fprint(w, `<section class="panel empty"><h2>No ledger events match</h2><p>Change the scope or wait for agents to write more ticket activity.</p></section>`)
			return
		}
		fmt.Fprint(w, `<section class="event-list" aria-label="Execution ledger events">`)
		for _, event := range view.Events {
			writeEventCard(w, event, view.TicketTitles[uuidText(event.TicketID)])
		}
		fmt.Fprint(w, `</section>`)
		if view.NextCursor != "" {
			next := eventLedgerPath(view, view.NextCursor)
			fmt.Fprintf(w, `<p class="pager"><a class="button secondary" href="%s">Poll after this cursor</a><code>%s</code></p>`, esc(next), esc(view.NextCursor))
		}
	})
}

func artifactListPage(view artifactListView) templ.Component {
	return layoutWithPage(pageContext{Title: "Forge Artifacts", ActiveRoute: "artifacts", WorkspaceID: view.WorkspaceIDText, ProjectID: view.ProjectIDText}, func(w io.Writer) {
		fmt.Fprint(w, `<section class="page-head"><div><h1>Artifacts</h1><p>Browse proof files and handoff outputs by workspace, project, or ticket.</p></div>`)
		fmt.Fprintf(w, `<a class="button" href="/tickets?workspace_id=%s&project_id=%s">Tickets</a></section>`, esc(view.WorkspaceIDText), esc(view.ProjectIDText))
		fmt.Fprint(w, `<section class="filters panel"><form method="get" action="/artifacts">`)
		writeScopeFields(w, view.Scope, view.WorkspaceIDText, view.ProjectIDText)
		input(w, "ticket_id", view.TicketIDText)
		fmt.Fprint(w, `<button type="submit">Apply</button></form></section>`)
		if view.Message != "" {
			fmt.Fprintf(w, `<section class="panel empty"><h2>Artifact browser needs a scope</h2><p>%s</p></section>`, esc(view.Message))
			return
		}
		if len(view.Artifacts) == 0 {
			fmt.Fprint(w, `<section class="panel empty"><h2>No artifacts match</h2><p>Change the scope to inspect a different workspace, project, or ticket.</p></section>`)
			return
		}
		fmt.Fprint(w, `<section class="ticket-list" aria-label="Artifacts">`)
		for _, artifact := range view.Artifacts {
			writeArtifactCard(w, artifact)
		}
		fmt.Fprint(w, `</section>`)
		limit := view.Limit
		if limit <= 0 {
			limit = 100
		}
		writeOffsetPager(w, artifactListPagePath(view, view.Offset-limit), artifactListPagePath(view, view.Offset+limit), view.Offset, len(view.Artifacts), limit)
	})
}

func proposedListPage(view proposedListView) templ.Component {
	return layoutWithPage(pageContext{Title: "Proposed Work", ActiveRoute: "proposed", WorkspaceID: view.WorkspaceIDText, ProjectID: view.ProjectIDText}, func(w io.Writer) {
		fmt.Fprintf(w, `<section class="page-head"><div><p class="eyebrow">Agent-created queue</p><h1>Proposed Work</h1><p>Review follow-up work agents discovered while executing tickets.</p></div><a class="button secondary" href="%s">Tickets</a></section>`, esc(scopedPagePath("/tickets", view.WorkspaceIDText, view.ProjectIDText)))
		fmt.Fprint(w, `<section class="filters panel"><form method="get" action="/proposed">`)
		writeScopeFields(w, view.Scope, view.WorkspaceIDText, view.ProjectIDText)
		writeChoiceField(w, "type", view.Type, "All types", ticketTypes)
		fmt.Fprint(w, `<button type="submit">Apply</button></form></section>`)
		if view.Message != "" {
			fmt.Fprintf(w, `<section class="panel empty"><h2>Proposed work needs a scope</h2><p>%s</p></section>`, esc(view.Message))
			return
		}
		if len(view.Items) == 0 {
			fmt.Fprint(w, `<section class="panel empty"><h2>No proposed work matches</h2><p>Agents have not suggested follow-up work for this scope yet.</p></section>`)
			return
		}
		fmt.Fprint(w, `<section class="ticket-list" aria-label="Proposed work">`)
		for _, item := range view.Items {
			writeProposedCard(w, item)
		}
		fmt.Fprint(w, `</section>`)
	})
}

func ticketDetailPage(view ticketDetailView) templ.Component {
	ticket := view.Ticket
	return layoutWithPage(pageContext{Title: ticket.Title, ActiveRoute: "tickets", WorkspaceID: uuidText(ticket.WorkspaceID), ProjectID: uuidText(ticket.ProjectID)}, func(w io.Writer) {
		fmt.Fprintf(w, `<section class="page-head"><div><p class="eyebrow">%s %s P%d</p><h1>%s</h1><p>%s</p></div><a class="button" href="/tickets?workspace_id=%s&project_id=%s">Back to list</a></section>`,
			esc(ticket.Status),
			esc(ticket.Type),
			ticket.Priority,
			esc(ticket.Title),
			esc(ticket.Description),
			esc(uuidText(ticket.WorkspaceID)),
			esc(uuidText(ticket.ProjectID)),
		)
		writeTrustSummary(w, view)
		fmt.Fprint(w, `<section class="detail-grid">`)
		fmt.Fprint(w, `<article class="panel"><h2>Context</h2>`)
		writeMeta(w, "Ticket ID", uuidText(ticket.ID))
		writeMeta(w, "Created by", actorLabel(ticket.CreatedBy, textOrEmpty(ticket.CreatedByID)))
		writeList(w, "Tags", ticket.Tags, "")
		writeList(w, "Acceptance", ticket.AcceptanceCriteria, "")
		writeList(w, "Verification", decodeStringArray(ticket.VerificationCommands), "$ ")
		writeList(w, "Paths", ticket.RelevantPaths, "")
		writeShareLinks(w, view)
		fmt.Fprint(w, `</article>`)
		fmt.Fprint(w, `<div>`)
		writeTicketActions(w, ticket)
		writeTimeline(w, view)
		fmt.Fprint(w, `</div>`)
		fmt.Fprint(w, `</section>`)
	})
}

func attemptDetailPage(attempt db.Attempt, artifacts []db.Artifact, checkpoints []db.AttemptCheckpoint, metrics db.AttemptMetric, hasMetrics bool) templ.Component {
	return layoutWithPage(pageContext{Title: "Attempt Detail", ActiveRoute: "tickets", WorkspaceID: uuidText(attempt.WorkspaceID), ProjectID: uuidText(attempt.ProjectID)}, func(w io.Writer) {
		fmt.Fprintf(w, `<section class="page-head"><div><p class="eyebrow">%s %s</p><h1>Attempt Detail</h1><p>%s</p></div><a class="button" href="/tickets/%s">Ticket</a></section>`,
			esc(attempt.Status),
			esc(uuidText(attempt.ID)),
			esc(actorLabel(attempt.AgentID, attempt.Model)),
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
		fmt.Fprint(w, `</article><article class="panel"><h2>Metrics</h2>`)
		if !hasMetrics {
			fmt.Fprint(w, `<p class="empty-text">No metrics reported for this attempt.</p>`)
		} else {
			writeMeta(w, "Tokens in", strconv.FormatInt(metrics.TokensIn, 10))
			writeMeta(w, "Tokens out", strconv.FormatInt(metrics.TokensOut, 10))
			if cost, ok := numericFloat(metrics.CostUsd); ok {
				writeMeta(w, "Cost", fmt.Sprintf("$%.4f", cost))
			}
			if duration, ok := numericFloat(metrics.DurationSeconds); ok {
				writeMeta(w, "Duration", fmt.Sprintf("%.3fs", duration))
			}
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
		fmt.Fprint(w, `</article><article class="panel"><h2>Checkpoints</h2>`)
		if len(checkpoints) == 0 {
			fmt.Fprint(w, `<p class="empty-text">No checkpoints recorded for this attempt.</p>`)
		}
		for _, checkpoint := range checkpoints {
			fmt.Fprintf(w, `<div class="timeline-item"><strong>%s</strong><span>%s</span>`, esc(checkpoint.Summary), esc(createdAtText(checkpoint.CreatedAt)))
			writeList(w, "Files touched", checkpoint.FilesTouched, "")
			writeList(w, "Commands", checkpoint.CommandsRun, "$ ")
			if checkpoint.NextStep.Valid {
				fmt.Fprintf(w, `<p>Next: %s</p>`, esc(checkpoint.NextStep.String))
			}
			if checkpoint.Risk.Valid {
				fmt.Fprintf(w, `<p>Risk: %s</p>`, esc(checkpoint.Risk.String))
			}
			fmt.Fprint(w, `</div>`)
		}
		fmt.Fprint(w, `</article></section>`)
	})
}

func artifactDetailPage(artifact db.Artifact, contentOpenable bool) templ.Component {
	return layoutWithPage(pageContext{Title: "Artifact Detail", ActiveRoute: "artifacts", WorkspaceID: uuidText(artifact.WorkspaceID), ProjectID: uuidText(artifact.ProjectID)}, func(w io.Writer) {
		fmt.Fprintf(w, `<section class="page-head"><div><p class="eyebrow">%s %s</p><h1>Artifact Detail</h1><p>%s</p></div><div class="actions"><a class="button" href="%s">Artifacts</a><a class="button" href="/tickets/%s">Ticket</a></div></section>`,
			esc(artifact.Role),
			esc(artifact.Type),
			esc(artifact.Name),
			esc(artifactListPath(artifact)),
			esc(uuidText(artifact.TicketID)),
		)
		fmt.Fprint(w, `<section class="detail-grid"><article class="panel"><h2>Metadata</h2>`)
		writeMeta(w, "Artifact", "/artifacts/"+uuidText(artifact.ID))
		writeMeta(w, "Name", artifact.Name)
		writeMeta(w, "Type", artifact.Type)
		writeMeta(w, "Role", artifact.Role)
		writeMeta(w, "Storage", artifact.StorageBackend)
		writeMeta(w, "Size", byteCount(artifact.SizeBytes))
		writeMeta(w, "MIME", artifact.MimeType)
		writeMeta(w, "URL", artifact.Url)
		writeMeta(w, "Ticket", "/tickets/"+uuidText(artifact.TicketID))
		if artifact.AttemptID.Valid {
			writeMeta(w, "Attempt", "/attempts/"+uuidText(artifact.AttemptID))
		}
		if metadata := formattedMetadata(artifact.Metadata); metadata != "" {
			fmt.Fprintf(w, `<div class="list"><h3>Metadata JSON</h3><pre>%s</pre></div>`, esc(metadata))
		}
		fmt.Fprint(w, `</article><article class="panel"><h2>Actions</h2>`)
		if contentOpenable {
			fmt.Fprintf(w, `<p><a href="/artifacts/%s/content">Open artifact</a></p>`, esc(uuidText(artifact.ID)))
		}
		if storage.IsLocalArtifactURL(artifact.Url) {
			fmt.Fprintf(w, `<form method="post" action="/artifacts/%s/delete" hx-boost="false" onsubmit="return confirm('Are you sure you want to delete this artifact? This action cannot be undone.');"><button type="submit">Delete local artifact</button></form>`, esc(uuidText(artifact.ID)))
		} else if storage.IsS3ArtifactURL(artifact.Url) {
			fmt.Fprint(w, `<p class="empty-text">Delete is constrained to local artifacts because Forge cannot safely clean remote objects yet.</p>`)
		} else if artifactURL, ok := safeArtifactURL(artifact.Url); ok {
			fmt.Fprintf(w, `<p><a href="%s">%s</a></p>`, esc(artifactURL), esc(artifactURL))
			fmt.Fprint(w, `<p class="empty-text">Delete is constrained to local artifacts because Forge cannot safely clean remote objects yet.</p>`)
		} else if artifact.Url != "" {
			fmt.Fprint(w, `<p class="empty-text">Artifact link hidden because its URL scheme is not supported.</p>`)
			fmt.Fprint(w, `<p class="empty-text">Delete is constrained to local artifacts.</p>`)
		}
		fmt.Fprint(w, `</article></section>`)
	})
}

func proposedDetailPage(ticket db.Ticket) templ.Component {
	return layoutWithPage(pageContext{Title: "Proposed Follow-up", ActiveRoute: "proposed", WorkspaceID: uuidText(ticket.WorkspaceID), ProjectID: uuidText(ticket.ProjectID)}, func(w io.Writer) {
		fmt.Fprintf(w, `<section class="page-head"><div><p class="eyebrow">%s %s</p><h1>Proposed Follow-up</h1><p>%s</p></div><a class="button" href="/tickets/%s">Ticket detail</a></section>`,
			esc(ticket.Status),
			esc(ticket.Type),
			esc(ticket.Title),
			esc(uuidText(ticket.ID)),
		)
		fmt.Fprint(w, `<section class="panel"><h2>Context</h2>`)
		writeMeta(w, "Proposed link", "/proposed/"+uuidText(ticket.ID))
		writeMeta(w, "Ticket link", "/tickets/"+uuidText(ticket.ID))
		writeMeta(w, "Source", actorLabel(ticket.CreatedBy, textOrEmpty(ticket.CreatedByID)))
		if ticket.CreationReason.Valid {
			writeMeta(w, "Reason", ticket.CreationReason.String)
		}
		writeList(w, "Acceptance", ticket.AcceptanceCriteria, "")
		writeList(w, "Paths", ticket.RelevantPaths, "")
		fmt.Fprint(w, `</section>`)
		fmt.Fprint(w, `<section class="panel proposed-actions"><h2>Triage actions</h2><p class="empty-text">Approve useful agent-created work, enqueue trusted work immediately, or close out proposals that should not enter the queue.</p><div class="action-grid">`)
		writeProposedActionForm(w, ticket.ID, "ready", "Move to todo", "Accepted for the scoped queue")
		writeProposedActionForm(w, ticket.ID, "enqueue", "Approve and enqueue", "Trusted enough for immediate agent claim")
		writeProposedActionForm(w, ticket.ID, "reject", "Reject", "Not useful right now")
		writeProposedActionForm(w, ticket.ID, "archive", "Archive", "Keep the record but remove from triage")
		fmt.Fprint(w, `</div></section>`)
	})
}

func workspaceIndexPage(view workspaceIndexView) templ.Component {
	return layout("Forge Workspaces", func(w io.Writer) {
		fmt.Fprint(w, `<section class="page-head"><div><p class="eyebrow">Welcome to Forge</p><h1>Workspaces</h1><p>A clear home for your projects, agents, and the work ahead.</p></div><a class="button secondary" href="/tickets">Open queue</a></section>`)
		fmt.Fprint(w, `<section class="filters panel"><form method="post" action="/workspaces">`)
		input(w, "name", "")
		fmt.Fprint(w, `<button type="submit">Create workspace</button></form></section>`)
		if view.Message != "" {
			fmt.Fprintf(w, `<section class="panel warning"><h2>Workspace action failed</h2><p>%s</p></section>`, esc(view.Message))
		}
		if len(view.Workspaces) == 0 {
			fmt.Fprint(w, `<section class="panel empty"><p class="eyebrow">A simple place to start</p><h2>No workspaces yet</h2><p>Create a workspace above, add a project, then describe your first ticket. Agents claim the work; you keep the full record.</p><p class="empty-text">Start with a small task whose result you can verify.</p></section>`)
			return
		}
		fmt.Fprint(w, `<section class="workspace-grid" aria-label="Workspaces">`)
		for _, workspace := range view.Workspaces {
			fmt.Fprintf(w, `<article class="ticket-card workspace-card"><a href="/workspaces/%s"><span class="title">%s</span><span class="summary">Open workspace &rarr;</span></a></article>`,
				esc(uuidText(workspace.ID)),
				esc(workspace.Name),
			)
		}
		fmt.Fprint(w, `</section>`)
	})
}

func workspaceDetailPage(view workspaceDetailView) templ.Component {
	workspace := view.Workspace
	return layoutWithPage(pageContext{Title: workspace.Name, ActiveRoute: "workspaces", WorkspaceID: uuidText(workspace.ID)}, func(w io.Writer) {
		fmt.Fprintf(w, `<section class="page-head"><div><p class="eyebrow">Workspace</p><h1>%s</h1><p>Choose a project to open its queue, or create a new home for your work.</p></div><a class="button secondary" href="/workspaces">Workspaces</a></section>`,
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
			fmt.Fprintf(w, `<div class="timeline-item"><strong>%s</strong><p><a class="button secondary" href="/tickets?workspace_id=%s&project_id=%s">Open ticket queue</a></p></div>`,
				esc(project.Name),
				esc(uuidText(workspace.ID)),
				esc(uuidText(project.ID)),
			)
		}
		fmt.Fprint(w, `</section>`)
	})
}

func layout(title string, body func(io.Writer)) templ.Component {
	return layoutWithPage(pageContext{Title: title}, body)
}

type pageContext struct {
	Title       string
	ActiveRoute string
	WorkspaceID string
	ProjectID   string
	ReturnURL   string
}

type signedInContextKey struct{}

func layoutWithPage(page pageContext, body func(io.Writer)) templ.Component {
	return templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><link rel="icon" href="/favicon.ico" type="image/svg+xml"><title>%s</title><script src="/assets/htmx-2.0.4.min.js" defer></script><style>%s</style></head><body hx-boost="true"><a class="skip-link" href="#main-content">Skip to content</a><div class="app-shell"><aside class="sidebar"><a class="brand" href="/workspaces"><span>F</span><strong>Forge</strong></a>`, esc(page.Title), pageCSS()+accessibilityCSS())
		fmt.Fprint(w, `<p class="sidebar-note">Agent work, made inspectable</p>`)
		if err := scopedNavigation(scopedNavigationItems(page)).Render(ctx, w); err != nil {
			return err
		}
		fmt.Fprint(w, `<div class="sidebar-footer">A durable record of the work.`)
		if signedIn, _ := ctx.Value(signedInContextKey{}).(bool); signedIn {
			fmt.Fprint(w, `<form method="post" action="/logout" hx-boost="false"><button class="secondary" type="submit">Sign out</button></form>`)
		}
		fmt.Fprint(w, `</div></aside><main id="main-content" class="content" tabindex="-1">`)
		body(w)
		fmt.Fprint(w, `</main></div></body></html>`)
		return nil
	})
}

func scopedNavigationItems(page pageContext) []navigationItem {
	paths := []struct{ label, route, path string }{{"Workspaces", "workspaces", "/workspaces"}, {"Tickets", "tickets", "/tickets"}, {"Proposed", "proposed", "/proposed"}, {"Activity", "events", "/events"}, {"Search", "search", "/search"}, {"Artifacts", "artifacts", "/artifacts"}}
	items := make([]navigationItem, 0, len(paths))
	for _, item := range paths {
		items = append(items, navigationItem{Label: item.label, Href: scopedPagePath(item.path, page.WorkspaceID, page.ProjectID), Active: item.route == page.ActiveRoute})
	}
	return items
}
func scopedPagePath(path, workspaceID, projectID string) string {
	if workspaceID == "" {
		return path
	}
	values := url.Values{}
	values.Set("workspace_id", workspaceID)
	if projectID != "" {
		values.Set("project_id", projectID)
	}
	return path + "?" + values.Encode()
}

func input(w io.Writer, name string, value string) {
	fmt.Fprintf(w, `<label><span>%s</span><input name="%s" value="%s"></label>`, esc(displayLabel(name)), esc(name), esc(value))
}

func writeTicketCard(w io.Writer, ticket db.Ticket) {
	fmt.Fprintf(w, `<article class="ticket-card"><a href="/tickets/%s"><span class="title">%s</span><span class="summary"><span class="badge" data-status="%s">%s</span><span class="badge badge-type">%s</span><span class="badge badge-prio" data-prio="%d">P%d</span></span></a>`,
		esc(uuidText(ticket.ID)),
		esc(ticket.Title),
		esc(ticket.Status),
		esc(displayLabel(ticket.Status)),
		esc(displayLabel(ticket.Type)),
		ticket.Priority,
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
	fmt.Fprintf(w, `<article class="ticket-card"><a href="/tickets/%s"><span class="title">%s</span><span class="summary"><span class="badge" data-status="%s">%s</span><span class="badge badge-type">%s</span><span class="badge badge-prio" data-prio="%d">P%d</span></span></a>`,
		esc(uuidText(ticket.ID)),
		esc(ticket.Title),
		esc(ticket.Status),
		esc(displayLabel(ticket.Status)),
		esc(displayLabel(ticket.Type)),
		ticket.Priority,
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

func writeArtifactCard(w io.Writer, artifact db.Artifact) {
	fmt.Fprintf(w, `<article class="ticket-card"><a href="/artifacts/%s"><span class="title">%s</span><span class="summary"><span class="badge badge-type">%s</span><span class="badge badge-type">%s</span><span class="badge badge-type">%s</span></span></a>`,
		esc(uuidText(artifact.ID)),
		esc(artifact.Name),
		esc(artifact.Role),
		esc(artifact.Type),
		esc(artifact.StorageBackend),
	)
	writeMeta(w, "Size", byteCount(artifact.SizeBytes))
	writeMeta(w, "MIME", artifact.MimeType)
	writeMeta(w, "Ticket", "/tickets/"+uuidText(artifact.TicketID))
	if artifact.AttemptID.Valid {
		writeMeta(w, "Attempt", "/attempts/"+uuidText(artifact.AttemptID))
	}
	fmt.Fprint(w, `</article>`)
}

func writeProposedCard(w io.Writer, item services.ProposedTicketTriageItem) {
	ticket := item.Ticket
	fmt.Fprintf(w, `<article class="ticket-card"><a href="/proposed/%s"><span class="title">%s</span><span class="summary"><span class="badge badge-type">%s</span><span class="badge badge-prio" data-prio="%d">P%d</span></span></a>`,
		esc(uuidText(ticket.ID)),
		esc(ticket.Title),
		esc(ticket.Type),
		ticket.Priority,
		ticket.Priority,
	)
	if item.CreationReason != "" {
		fmt.Fprintf(w, `<p>%s</p>`, esc(item.CreationReason))
	}
	writeMeta(w, "Source", actorLabel(ticket.CreatedBy, item.CreatedByID))
	if item.SourceAttemptID.Valid {
		writeMeta(w, "Attempt", "/attempts/"+uuidText(item.SourceAttemptID))
	}
	if item.SourceArtifactID.Valid {
		writeMeta(w, "Artifact", "/artifacts/"+uuidText(item.SourceArtifactID))
	}
	writeList(w, "Acceptance", item.AcceptanceCriteria, "")
	writeList(w, "Verification", item.VerificationCommands, "$ ")
	writeList(w, "Paths", item.RelevantPaths, "")
	fmt.Fprint(w, `</article>`)
}

func writeTicketActions(w io.Writer, ticket db.Ticket) {
	actions := ticketActionsForStatus(ticket.Status)
	if len(actions) == 0 {
		fmt.Fprint(w, `<article class="panel ticket-actions"><h2>Ticket actions</h2><p class="empty-text">No direct lifecycle actions are available for this ticket status.</p></article>`)
		return
	}
	fmt.Fprint(w, `<article class="panel ticket-actions"><h2>Ticket actions</h2><p class="empty-text">Choose the next step. Your decision and reason stay in the activity history.</p><div class="action-grid compact">`)
	for _, action := range actions {
		writeTicketActionForm(w, ticket.ID, action.Action, action.Label, action.Placeholder)
	}
	fmt.Fprint(w, `</div></article>`)
}

func writeTicketActionForm(w io.Writer, ticketID pgtype.UUID, action string, label string, placeholder string) {
	class, confirmation, required := "", "", ""
	if action == "archive" {
		class = ` class="destructive"`
		confirmation = ` onsubmit="return confirm('Archive this ticket? This removes it from the active queue.');"`
		required = ` required`
	}
	fmt.Fprintf(w, `<form method="post" action="/tickets/%s/%s" hx-boost="false"%s><label><span>Reason</span><input name="reason" placeholder="%s"%s></label><button type="submit"%s>%s</button></form>`,
		esc(uuidText(ticketID)),
		esc(action),
		confirmation,
		esc(placeholder),
		required,
		class,
		esc(label),
	)
}

type ticketActionSpec struct {
	Action      string
	Label       string
	Placeholder string
}

func ticketActionsForStatus(status string) []ticketActionSpec {
	var actions []ticketActionSpec
	switch status {
	case services.TicketStatusBacklog:
		actions = append(actions, ticketActionSpec{"ready", "Mark ready", "Ready for an agent to claim"})
	case services.TicketStatusDone, services.TicketStatusFailed:
		actions = append(actions, ticketActionSpec{"reopen", "Reopen", "Return to todo for another attempt"})
	case services.TicketStatusBlocked:
		actions = append(actions, ticketActionSpec{"unblock", "Unblock", "Blocker cleared"})
	case services.TicketStatusNeedsReview:
		actions = append(actions, ticketActionSpec{"approve", "Approve", "Evidence meets the acceptance criteria"}, ticketActionSpec{"request-changes", "Request changes", "What should the next attempt address?"})
	}
	switch status {
	case services.TicketStatusBlocked, services.TicketStatusTodo, services.TicketStatusFailed, services.TicketStatusDone:
		actions = append(actions, ticketActionSpec{"request-review", "Request review", "Ready for human review"})
	}
	switch status {
	case services.TicketStatusBacklog, services.TicketStatusTodo, services.TicketStatusBlocked, services.TicketStatusNeedsReview, services.TicketStatusDone, services.TicketStatusFailed:
		actions = append(actions, ticketActionSpec{"archive", "Archive", "No longer needed"})
	}
	return actions
}

func isTicketAction(action string) bool {
	switch action {
	case "ready", "reopen", "unblock", "request-review", "approve", "request-changes", "archive":
		return true
	default:
		return false
	}
}

func writeEventCard(w io.Writer, event db.TicketEvent, ticketTitle string) {
	fmt.Fprintf(w, `<article class="event-card"><div class="event-marker"><span>%d</span></div><div class="event-body"><div class="event-topline"><strong>%s</strong><span>%s</span></div>`,
		event.EventSequence,
		esc(event.Type),
		esc(createdAtText(event.CreatedAt)),
	)
	fmt.Fprintf(w, `<p class="event-meta">%s</p>`, esc(actorLabel(event.ActorType, textOrEmpty(event.ActorID))))
	if ticketTitle != "" {
		fmt.Fprintf(w, `<p class="event-ticket">%s</p>`, esc(ticketTitle))
	}
	if summary := timelineReason(event.Data); summary != "" {
		fmt.Fprintf(w, `<p>%s</p>`, esc(summary))
	}
	fmt.Fprint(w, `<div class="event-links">`)
	if event.TicketID.Valid {
		linkText := "Ticket"
		if ticketTitle != "" {
			linkText = ticketTitle
		}
		fmt.Fprintf(w, `<a class="copy-link" href="/tickets/%s">%s</a>`, esc(uuidText(event.TicketID)), esc(linkText))
	}
	if event.AttemptID.Valid {
		fmt.Fprintf(w, `<a class="copy-link" href="/attempts/%s">Attempt</a>`, esc(uuidText(event.AttemptID)))
	}
	fmt.Fprint(w, `</div></div></article>`)
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

func writeTrustSummary(w io.Writer, view ticketDetailView) {
	if view.TimelineErr != nil {
		return
	}
	ticket := view.Ticket
	fmt.Fprint(w, `<section class="trust-strip" aria-label="Trust summary"><div class="trust-card"><span>Trust summary</span><strong>Shared proof page</strong><p>Ticket, activity, attempts, and artifacts in one inspectable place.</p></div>`)
	writeTrustMetric(w, len(view.Timeline.Attempts), "attempt", "")
	writeTrustMetric(w, len(view.Timeline.Events), "event", ticketActivityPath(ticket))
	writeTrustMetric(w, len(view.Timeline.Artifacts), "proof artifact", artifactListPathForTicket(ticket))
	fmt.Fprint(w, `</section>`)
}

func writeTrustMetric(w io.Writer, count int, noun string, href string) {
	label := pluralize(count, noun)
	if href == "" {
		fmt.Fprintf(w, `<div class="trust-card metric"><strong>%s</strong><span>%d</span></div>`, esc(label), count)
		return
	}
	fmt.Fprintf(w, `<a class="trust-card metric" href="%s"><strong>%s</strong><span>%d</span></a>`, esc(href), esc(label), count)
}

func writeProposedActionForm(w io.Writer, ticketID pgtype.UUID, action string, label string, placeholder string) {
	fmt.Fprintf(w, `<form method="post" action="/proposed/%s/%s" hx-boost="false"><input type="hidden" name="actor_type" value="%s"><input type="hidden" name="actor_id" value="web"><label><span>Reason</span><input name="reason" placeholder="%s"></label><button type="submit">%s</button></form>`,
		esc(uuidText(ticketID)),
		esc(action),
		esc(services.ActorHuman),
		esc(placeholder),
		esc(label),
	)
}

func isProposedTicket(ticket db.Ticket) bool {
	return ticket.CreatedBy == services.ActorAgent && ticket.Status == services.TicketStatusBacklog
}

func writeTimeline(w io.Writer, view ticketDetailView) {
	for _, message := range view.Timeline.Errors {
		fmt.Fprintf(w, `<article class="panel warning"><h2>Inspection section unavailable</h2><p>%s</p></article>`, esc(message))
	}
	fmt.Fprint(w, `<article class="panel"><h2>Current attempt</h2>`)
	current := db.Attempt{}
	for _, attempt := range view.Timeline.Attempts {
		if attempt.Status == services.AttemptStatusRunning {
			current = attempt
			break
		}
	}
	if current.ID.Valid {
		fmt.Fprintf(w, `<div class="timeline-item"><strong>%s</strong><span>%s</span><p>Lease expires %s</p>`, esc(current.AgentID), esc(actorLabel(current.Harness, current.Model)), esc(createdAtText(current.LeaseExpiresAt)))
		if current.CurrentSummary.Valid {
			fmt.Fprintf(w, `<p>%s</p>`, esc(current.CurrentSummary.String))
		}
		if reason := blockerReason(current.Blocker); reason != "" {
			fmt.Fprintf(w, `<p class="warning">Blocker: %s</p>`, esc(reason))
		}
		fmt.Fprint(w, `</div>`)
	} else {
		fmt.Fprint(w, `<p class="empty-text">No running attempt currently owns this ticket.</p>`)
	}
	fmt.Fprint(w, `<h2>Latest checkpoint</h2>`)
	if len(view.Timeline.Checkpoints) == 0 {
		fmt.Fprint(w, `<p class="empty-text">No checkpoints recorded yet.</p>`)
	} else {
		checkpoint := view.Timeline.Checkpoints[len(view.Timeline.Checkpoints)-1]
		fmt.Fprintf(w, `<div class="timeline-item"><strong>%s</strong><span>%s</span>`, esc(checkpoint.Summary), esc(createdAtText(checkpoint.CreatedAt)))
		writeList(w, "Files touched", checkpoint.FilesTouched, "")
		writeList(w, "Commands", checkpoint.CommandsRun, "$ ")
		if checkpoint.NextStep.Valid {
			fmt.Fprintf(w, `<p>Next: %s</p>`, esc(checkpoint.NextStep.String))
		}
		if checkpoint.Risk.Valid {
			fmt.Fprintf(w, `<p>Risk: %s</p>`, esc(checkpoint.Risk.String))
		}
		fmt.Fprint(w, `</div>`)
	}
	fmt.Fprint(w, `<h2>Attempts</h2>`)
	if len(view.Timeline.Attempts) == 0 {
		fmt.Fprint(w, `<p class="empty-text">No attempts recorded yet.</p>`)
	}
	for _, attempt := range view.Timeline.Attempts {
		fmt.Fprintf(w, `<div class="timeline-item"><strong>%s</strong><span>%s</span>`, esc(attempt.Status), esc(actorLabel(attempt.AgentID, attempt.Model)))
		if attempt.CurrentSummary.Valid {
			fmt.Fprintf(w, `<p>%s</p>`, esc(attempt.CurrentSummary.String))
		}
		writeAttemptFailureReason(w, attempt)
		fmt.Fprint(w, `</div>`)
	}
	fmt.Fprint(w, `<h2>Events</h2>`)
	if len(view.Timeline.Events) == 0 {
		fmt.Fprint(w, `<p class="empty-text">No ticket events recorded.</p>`)
	}
	for _, event := range view.Timeline.Events {
		fmt.Fprintf(w, `<div class="timeline-item"><strong>%s</strong><span>%s</span>`, esc(event.Type), esc(actorLabel(event.ActorType, textOrEmpty(event.ActorID))))
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
		if view.ArtifactContentOpenable[uuidText(artifact.ID)] {
			fmt.Fprintf(w, `<p><a href="/artifacts/%s">Open artifact</a></p>`, esc(uuidText(artifact.ID)))
		} else if artifactURL, ok := safeArtifactURL(artifact.Url); ok {
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

// writeAttemptFailureReason renders the human-readable failure category and
// blocker/failure reason for a blocked or failed attempt, mirroring the TUI
// detail view's "Failure: <reason> (<category>)" and "Blocker: <reason>" lines
// (see internal/tui/detail.go writeAttemptNotes). The data already rides on the
// db.Attempt rows in the ticket-detail view model.
func writeAttemptFailureReason(w io.Writer, attempt db.Attempt) {
	if attempt.Status != services.AttemptStatusBlocked && attempt.Status != services.AttemptStatusFailed {
		return
	}
	if attempt.FailureReason.Valid {
		if attempt.FailureCategory.Valid {
			fmt.Fprintf(w, `<p class="warning">Failure: %s (%s)</p>`, esc(attempt.FailureReason.String), esc(attempt.FailureCategory.String))
		} else {
			fmt.Fprintf(w, `<p class="warning">Failure: %s</p>`, esc(attempt.FailureReason.String))
		}
	} else if attempt.FailureCategory.Valid {
		fmt.Fprintf(w, `<p class="warning">Failure category: %s</p>`, esc(attempt.FailureCategory.String))
	}
	if reason := blockerReason(attempt.Blocker); reason != "" {
		fmt.Fprintf(w, `<p class="warning">Blocker: %s</p>`, esc(reason))
	}
}

// blockerReason extracts the human-readable reason from an attempt's blocker
// JSON, returning "" for an empty or "{}" blocker so callers never surface raw
// braces.
func blockerReason(raw []byte) string {
	if len(raw) == 0 || string(raw) == "{}" {
		return ""
	}
	return timelineReason(raw)
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

func artifactContentOpenability(runtime Runtime, artifacts []db.Artifact) map[string]bool {
	openable := make(map[string]bool, len(artifacts))
	if runtime == nil {
		return openable
	}
	for _, artifact := range artifacts {
		openable[uuidText(artifact.ID)] = runtime.ArtifactContentOpenable(artifact)
	}
	return openable
}

func headerFilename(value string) string {
	value = strings.ReplaceAll(value, `"`, "")
	value = strings.ReplaceAll(value, "\r", "")
	value = strings.ReplaceAll(value, "\n", "")
	if strings.TrimSpace(value) == "" {
		return "artifact"
	}
	return value
}

func byteCount(size int64) string {
	if size < 0 {
		return ""
	}
	if size == 1 {
		return "1 byte"
	}
	return fmt.Sprintf("%d bytes", size)
}

func formattedMetadata(raw []byte) string {
	if len(raw) == 0 || string(raw) == "{}" {
		return ""
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return string(raw)
	}
	formatted, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(formatted)
}

func artifactListPath(artifact db.Artifact) string {
	return fmt.Sprintf("/artifacts?workspace_id=%s&project_id=%s", url.QueryEscape(uuidText(artifact.WorkspaceID)), url.QueryEscape(uuidText(artifact.ProjectID)))
}

func textValue(value pgtype.Text) string {
	if !value.Valid || value.String == "" {
		return "-"
	}
	return value.String
}

func textOrEmpty(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func numericFloat(value pgtype.Numeric) (float64, bool) {
	if !value.Valid {
		return 0, false
	}
	float, err := value.Float64Value()
	if err != nil || !float.Valid {
		return 0, false
	}
	return float.Float64, true
}

// actorLabel joins an actor/agent pair (e.g. type + id, or agent + model) with
// " / ", dropping an empty half so labels never render a dangling slash like
// "human/-" or "codex/".
func actorLabel(primary, secondary string) string {
	primary = strings.TrimSpace(primary)
	secondary = strings.TrimSpace(secondary)
	switch {
	case primary != "" && secondary != "":
		return primary + " / " + secondary
	case primary != "":
		return primary
	default:
		return secondary
	}
}

func createdAtText(value pgtype.Timestamptz) string {
	if !value.Valid {
		return ""
	}
	return value.Time.UTC().Format("2006-01-02 15:04:05 UTC")
}

func limitText(value int32) string {
	if value <= 0 {
		return ""
	}
	return strconv.FormatInt(int64(value), 10)
}

func eventLedgerViewFromQuery(r *http.Request, message string) eventLedgerView {
	query := r.URL.Query()
	return eventLedgerView{
		WorkspaceIDText: strings.TrimSpace(query.Get("workspace_id")),
		ProjectIDText:   strings.TrimSpace(query.Get("project_id")),
		TicketIDText:    strings.TrimSpace(query.Get("ticket_id")),
		AttemptIDText:   strings.TrimSpace(query.Get("attempt_id")),
		Cursor:          strings.TrimSpace(query.Get("cursor")),
		LimitText:       strings.TrimSpace(query.Get("limit")),
		Message:         message,
	}
}

func eventLedgerPath(view eventLedgerView, cursor string) string {
	query := url.Values{}
	if view.WorkspaceIDText != "" {
		query.Set("workspace_id", view.WorkspaceIDText)
	}
	if view.ProjectIDText != "" {
		query.Set("project_id", view.ProjectIDText)
	}
	if view.TicketIDText != "" {
		query.Set("ticket_id", view.TicketIDText)
	}
	if view.AttemptIDText != "" {
		query.Set("attempt_id", view.AttemptIDText)
	}
	if view.LimitText != "" {
		query.Set("limit", view.LimitText)
	}
	if cursor != "" {
		query.Set("cursor", cursor)
	}
	if encoded := query.Encode(); encoded != "" {
		return "/events?" + encoded
	}
	return "/events"
}

func ticketActivityPath(ticket db.Ticket) string {
	query := url.Values{}
	query.Set("workspace_id", uuidText(ticket.WorkspaceID))
	query.Set("project_id", uuidText(ticket.ProjectID))
	query.Set("ticket_id", uuidText(ticket.ID))
	return "/events?" + query.Encode()
}

func artifactListPathForTicket(ticket db.Ticket) string {
	query := url.Values{}
	query.Set("workspace_id", uuidText(ticket.WorkspaceID))
	query.Set("project_id", uuidText(ticket.ProjectID))
	query.Set("ticket_id", uuidText(ticket.ID))
	return "/artifacts?" + query.Encode()
}

func ticketListPagePath(view ticketListView, offset int32) string {
	if offset < 0 {
		offset = 0
	}
	values := url.Values{"workspace_id": {uuidText(view.WorkspaceID)}, "project_id": {uuidText(view.ProjectID)}}
	if view.Status != "" {
		values.Set("status", view.Status)
	}
	if view.Type != "" {
		values.Set("type", view.Type)
	}
	if view.Limit > 0 {
		values.Set("limit", strconv.FormatInt(int64(view.Limit), 10))
	}
	if offset > 0 {
		values.Set("offset", strconv.FormatInt(int64(offset), 10))
	}
	return "/tickets?" + values.Encode()
}

func artifactListPagePath(view artifactListView, offset int32) string {
	if offset < 0 {
		offset = 0
	}
	values := url.Values{"workspace_id": {view.WorkspaceIDText}, "project_id": {view.ProjectIDText}}
	if view.TicketIDText != "" {
		values.Set("ticket_id", view.TicketIDText)
	}
	if view.Limit > 0 {
		values.Set("limit", strconv.FormatInt(int64(view.Limit), 10))
	}
	if offset > 0 {
		values.Set("offset", strconv.FormatInt(int64(offset), 10))
	}
	return "/artifacts?" + values.Encode()
}

func writeOffsetPager(w io.Writer, previous, next string, offset int32, returned int, limit int32) {
	if offset <= 0 && int32(returned) < limit {
		return
	}
	fmt.Fprint(w, `<p class="pager" aria-label="Pagination">`)
	if offset > 0 {
		fmt.Fprintf(w, `<a class="button secondary" href="%s">Previous</a>`, esc(previous))
	}
	if int32(returned) >= limit {
		fmt.Fprintf(w, `<a class="button secondary" href="%s">Next</a>`, esc(next))
	}
	fmt.Fprint(w, `</p>`)
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

func defaultString(value string, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func pluralize(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", count, noun)
}

func pageCSS() string {
	return `
/* ===== Design tokens (light) ===== */
:root{
--background:oklch(0.9551 0 0);--surface:#ffffff;--surface-2:#f3f4f3;--card:#ffffff;--card-foreground:#24312e;
--foreground:#24312e;--muted:#eef0ee;--muted-foreground:#59655f;--border:#dce1dd;--border-strong:#b9c7bf;--input:#aebcb4;
--primary:#14634f;--primary-hover:#0c4d3c;--primary-foreground:#ffffff;--secondary:#ffffff;
--accent:#e2f0e9;--accent-foreground:#14513f;--ring:#21715b;--focus-glow:rgba(33,113,91,.18);
--success:#15803d;--success-subtle:#dcfce7;--warning:#93400a;--warning-subtle:#fdf0d5;--destructive:#dc2626;--destructive-hover:#b91c1c;--destructive-subtle:#fde8e8;--destructive-foreground:#ffffff;
--link:#14634f;
--shadow-sm:0 1px 2px rgba(20,40,30,.035);
--shadow-md:0 3px 10px rgba(20,40,30,.07);
--radius-sm:6px;--radius:10px;--radius-lg:12px;--radius-pill:999px;
--sidebar-bg:rgba(255,255,255,.85);
--text-xs:12px;--text-sm:13px;--text-base:14px;--text-lg:16px;--text-xl:20px;--text-2xl:30px;
--space-1:4px;--space-2:8px;--space-3:12px;--space-4:16px;--space-5:20px;--space-6:24px;--space-8:32px;
font-family:ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;
color-scheme:light;color:var(--foreground);background:var(--background);
}
/* ===== Base ===== */
*{box-sizing:border-box}
body{margin:0;font-size:var(--text-base);line-height:1.55;-webkit-font-smoothing:antialiased;text-rendering:optimizeLegibility}
h2{font-size:var(--text-lg);font-weight:600;letter-spacing:-.01em;margin:0 0 var(--space-3)}
h3{font-weight:600}
/* ===== Layout ===== */
.app-shell{display:grid;grid-template-columns:236px minmax(0,1fr);min-height:100vh}
.content{max-width:1180px;width:100%;padding:var(--space-8) var(--space-6) 64px}
/* ===== Sidebar & nav ===== */
.sidebar{position:sticky;top:0;height:100vh;border-right:1px solid var(--border);background:var(--sidebar-bg);backdrop-filter:blur(14px);-webkit-backdrop-filter:blur(14px);padding:var(--space-5) var(--space-3);display:flex;flex-direction:column}
.brand{display:flex;gap:10px;align-items:center;color:inherit;text-decoration:none;margin:2px 8px 26px}
.brand span{display:grid;place-items:center;width:30px;height:30px;border-radius:9px;background:var(--primary);color:var(--primary-foreground);font-weight:700;box-shadow:var(--shadow-sm)}
.brand strong{font-size:15px;font-weight:600;letter-spacing:-.01em}
.sidebar nav{display:grid;gap:2px}
.sidebar nav a{display:flex;align-items:center;color:var(--muted-foreground);text-decoration:none;padding:9px 11px;border-radius:8px;font-size:var(--text-base);font-weight:500;transition:background .15s,color .15s}
.sidebar nav a:hover{background:var(--muted);color:var(--foreground)}
/* ===== Page header ===== */
.page-head{display:flex;justify-content:space-between;gap:24px;align-items:flex-start;margin-bottom:var(--space-6);padding-bottom:var(--space-5);border-bottom:1px solid var(--border)}
.page-head h1{margin:6px 0 8px;font-size:var(--text-2xl);line-height:1.1;font-weight:700;letter-spacing:-.02em}
.page-head p{margin:0;color:var(--muted-foreground);max-width:720px;font-size:var(--text-lg)}
.eyebrow{text-transform:uppercase;font-size:var(--text-xs);font-weight:600;color:var(--primary);letter-spacing:.08em}
.actions{display:flex;gap:var(--space-2);align-items:center;flex-shrink:0}
/* ===== Buttons ===== */
.button,button{display:inline-flex;align-items:center;justify-content:center;gap:6px;border:1px solid var(--primary);background:var(--primary);color:var(--primary-foreground);text-decoration:none;padding:8px 14px;border-radius:8px;font-weight:600;font-size:var(--text-base);white-space:nowrap;cursor:pointer;box-shadow:var(--shadow-sm);transition:background .15s,border-color .15s,color .15s,box-shadow .15s,opacity .15s,transform .08s}
.button.secondary,button.secondary{background:var(--surface);color:var(--foreground);border-color:var(--border-strong);box-shadow:none}
.button:hover:not(:disabled),button:hover:not(:disabled){background:var(--primary-hover);border-color:var(--primary-hover);box-shadow:var(--shadow-md)}
.button.secondary:hover:not(:disabled),button.secondary:hover:not(:disabled){background:var(--muted);border-color:var(--input);box-shadow:none}
.button:active:not(:disabled),button:active:not(:disabled){transform:translateY(1px)}
.button:disabled,button:disabled{opacity:.55;cursor:not-allowed;box-shadow:none}
a:focus-visible,button:focus-visible,input:focus-visible{outline:2px solid var(--ring);outline-offset:2px;border-radius:6px}
/* ===== Panels & forms ===== */
.panel{background:var(--card);border:1px solid var(--border);border-radius:var(--radius-lg);padding:var(--space-5);box-shadow:var(--shadow-sm)}
.panel h2{margin-top:0}
.filters form{display:grid;grid-template-columns:repeat(auto-fit,minmax(150px,1fr));gap:var(--space-3);align-items:end}
label{min-width:0}label>span{display:block;font-size:var(--text-sm);font-weight:600;color:var(--foreground);margin-bottom:6px}
input:not([type=hidden]),select,textarea{width:100%;min-width:0;min-height:44px;border:1px solid var(--input);border-radius:7px;padding:9px 11px;background:var(--surface);color:var(--foreground);font:inherit;font-size:16px;transition:border-color .15s,box-shadow .15s}
textarea{resize:vertical}input:focus,select:focus,textarea:focus{border-color:var(--ring);box-shadow:0 0 0 3px var(--focus-glow);outline:2px solid var(--ring);outline-offset:1px}
small{color:var(--muted-foreground);font-size:var(--text-xs)}label>small{display:block;margin-top:5px}
.auth-panel{max-width:400px;margin:12vh auto 0;background:var(--card);border:1px solid var(--border);border-radius:var(--radius-lg);padding:var(--space-8);box-shadow:var(--shadow-md)}
.auth-panel form{display:grid;gap:var(--space-4)}
.auth-panel h1{margin-top:0}
.auth-error{color:var(--destructive);font-weight:600}
/* ===== Cards & lists ===== */
.ticket-list,.event-list{display:grid;gap:var(--space-3);margin-top:var(--space-4)}
.ticket-card,.event-card{background:var(--card);border:1px solid var(--border);border-radius:var(--radius);padding:var(--space-4);box-shadow:var(--shadow-sm);transition:border-color .15s,box-shadow .15s,transform .1s}
.ticket-card:hover{border-color:var(--border-strong);background:var(--surface-2)}
.ticket-card a{display:flex;justify-content:space-between;gap:16px;align-items:flex-start;color:inherit;text-decoration:none}
.title{font-weight:600;font-size:var(--text-lg);letter-spacing:-.01em}
.summary{display:flex;flex-wrap:wrap;gap:6px;align-items:center;justify-content:flex-end}
.empty-text,.event-meta{color:var(--muted-foreground)}
.meta{display:flex;gap:10px;align-items:baseline;margin:10px 0 0}
.meta span{min-width:84px;font-size:var(--text-xs);font-weight:600;text-transform:uppercase;letter-spacing:.04em;color:var(--muted-foreground)}
.meta strong{font-weight:600;color:var(--foreground)}
.list h3{font-size:var(--text-sm);margin:16px 0 6px;color:var(--foreground);font-weight:600}
.list ul{margin:0;padding-left:20px}
.list li{margin:2px 0}
/* ===== Badges ===== */
.badge{display:inline-flex;align-items:center;gap:4px;padding:2px 9px;border-radius:var(--radius-pill);font-size:11.5px;font-weight:600;letter-spacing:.02em;text-transform:capitalize;background:var(--muted);color:var(--muted-foreground);border:1px solid transparent;line-height:1.7;white-space:nowrap}
.badge[data-status="in_progress"],.badge[data-status="claimed"]{background:var(--accent);color:var(--accent-foreground)}
.badge[data-status="done"],.badge[data-status="completed"],.badge[data-status="verified"],.badge[data-status="resolved"]{background:var(--success-subtle);color:var(--success)}
.badge[data-status="blocked"],.badge[data-status="needs_human"],.badge[data-status="failed"],.badge[data-status="cancelled"],.badge[data-status="archived"]{background:var(--destructive-subtle);color:var(--destructive)}
.badge[data-status="needs_review"]{background:var(--warning-subtle);color:var(--warning)}
.badge-type{background:var(--surface-2);color:var(--foreground);text-transform:none}
.badge-prio{font-variant-numeric:tabular-nums;text-transform:uppercase}
.badge-prio[data-prio="0"],.badge-prio[data-prio="1"]{background:var(--destructive-subtle);color:var(--destructive)}
.badge-prio[data-prio="2"]{background:var(--warning-subtle);color:var(--warning)}
/* ===== Detail & trust strip ===== */
.detail-grid{display:grid;grid-template-columns:minmax(0,1fr) minmax(320px,.85fr);gap:var(--space-4);align-items:start}.detail-grid>div{display:grid;gap:var(--space-4)}.detail-grid>*{min-width:0}
.trust-strip{display:grid;grid-template-columns:1.3fr repeat(3,minmax(120px,.55fr));gap:var(--space-3);margin-bottom:var(--space-4)}
.trust-card{display:block;background:var(--card);border:1px solid var(--border);border-radius:var(--radius);padding:var(--space-4);color:inherit;text-decoration:none;box-shadow:var(--shadow-sm);transition:border-color .15s,box-shadow .15s,transform .1s}
.trust-card span{display:block;color:var(--muted-foreground);font-size:var(--text-xs);font-weight:600;text-transform:uppercase;letter-spacing:.04em}
.trust-card strong{display:block;margin-top:4px;font-weight:600}
.trust-card p{margin:6px 0 0;color:var(--muted-foreground)}
.trust-card.metric span{font-size:var(--text-2xl);color:var(--foreground);font-weight:700;text-transform:none;font-variant-numeric:tabular-nums;letter-spacing:-.02em}
a.trust-card:hover,.trust-card.metric:hover{border-color:var(--border-strong);box-shadow:var(--shadow-md);transform:translateY(-1px)}
/* ===== Action grid ===== */
.action-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:var(--space-3);margin-top:var(--space-3)}
.action-grid form{display:grid;gap:10px;border:1px solid var(--border);border-radius:var(--radius);padding:var(--space-3);background:var(--surface-2);min-width:0}
.action-grid form input,.action-grid form button{width:100%;min-width:0}
.action-grid form button{white-space:normal;overflow-wrap:anywhere}
/* ===== Timeline & ledger ===== */
.timeline-item{border-top:1px solid var(--border);padding:12px 0}
.timeline-item strong{display:block;font-weight:600}
.timeline-item span{color:var(--muted-foreground);font-size:var(--text-sm)}
.event-card{display:grid;grid-template-columns:52px minmax(0,1fr);gap:12px}
.event-marker span{display:grid;place-items:center;min-width:36px;height:28px;border-radius:var(--radius-pill);background:var(--accent);color:var(--accent-foreground);font-size:var(--text-xs);font-weight:600;font-variant-numeric:tabular-nums}
.event-topline{display:flex;justify-content:space-between;gap:12px;align-items:baseline}
.event-topline strong{font-weight:600}
.event-topline span{color:var(--muted-foreground);font-size:var(--text-xs)}
.event-body p{margin:6px 0 0}
.event-ticket{font-weight:600;color:var(--foreground)}
.event-links{display:flex;gap:10px;margin-top:8px}
.copy-link{font-size:var(--text-sm);color:var(--link);text-decoration:none}
.copy-link:hover{text-decoration:underline}
.match-snippet{background:var(--muted);border-radius:8px;padding:10px;color:var(--muted-foreground)}
.queue-tabs{display:flex;gap:5px;flex-wrap:wrap;border-bottom:1px solid var(--border);margin:22px 0 16px;padding-bottom:8px}.queue-tabs a{padding:9px 12px;min-height:44px;border-radius:7px;color:var(--muted-foreground);text-decoration:none;font-weight:500}.queue-tabs a:hover{background:var(--muted)}.queue-tabs a[aria-current]{background:var(--accent);color:var(--accent-foreground);font-weight:650}
.agent-setup{margin:12px 0;padding:12px 16px;border:1px dashed var(--border-strong);border-radius:var(--radius);color:var(--muted-foreground);font-size:var(--text-sm)}summary{cursor:pointer;min-height:24px;font-weight:600;color:var(--foreground)}summary:focus-visible{outline:2px solid var(--ring);outline-offset:4px}
pre{padding:14px;border-radius:var(--radius-sm);background:var(--muted);white-space:pre-wrap;overflow-wrap:anywhere}code{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.9em}.meta strong,.copy-link,.event-body,.page-head{overflow-wrap:anywhere}.meta strong{min-width:0}
.create-grid{display:grid;grid-template-columns:minmax(0,1fr) 280px;gap:24px;margin-top:24px;align-items:start}.ticket-create{display:grid;gap:22px}.form-row{display:grid;grid-template-columns:1fr 1fr;gap:16px}.onboarding{background:var(--surface-2)}.onboarding ol{padding-left:22px;margin-bottom:24px}.onboarding li{padding-left:6px;margin:22px 0}.onboarding p{color:var(--muted-foreground);margin:6px 0}
.workspace-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(260px,1fr));gap:16px;margin-top:24px}.workspace-card{padding:24px}.workspace-card a{display:grid;gap:20px}.workspace-card .summary{justify-content:flex-start;font-size:var(--text-sm);color:var(--muted-foreground)}.workspace-card .title{font-size:var(--text-xl)}
.sidebar-note{margin:0 10px 20px;font-size:var(--text-xs);color:var(--muted-foreground)}.sidebar-footer{margin-top:auto;padding:24px 10px 4px;color:var(--muted-foreground);font-size:var(--text-xs)}.sidebar-footer form{margin-top:10px}.sidebar-footer button{font-size:var(--text-sm);width:100%}
.empty{padding:40px;max-width:760px}.empty p{max-width:560px;margin-bottom:22px}.empty .eyebrow{margin-bottom:10px}.empty h2{font-size:var(--text-xl);color:var(--foreground)}.actions{flex-wrap:wrap}
@media(max-width:860px){.create-grid,.form-row,.workspace-grid{grid-template-columns:1fr}.sidebar-note{display:none}.sidebar-footer{padding:8px 10px;display:flex;gap:12px;align-items:center;justify-content:space-between}.sidebar-footer form{margin:0}.empty{padding:24px}.trust-strip{grid-template-columns:1fr 1fr}.trust-card:first-child{grid-column:1/-1}.create-grid{gap:16px}.page-head .actions{margin-top:16px}}
/* ===== Utilities ===== */
.warning{border-color:var(--warning);background:var(--warning-subtle);color:var(--foreground)}
.empty{margin-top:16px;color:var(--muted-foreground)}
.pager{display:flex;gap:10px;align-items:center;margin:16px 0 0}
.pager code{font-size:var(--text-xs);color:var(--muted-foreground);overflow-wrap:anywhere}
/* ===== Responsive ===== */
@media(max-width:860px){.app-shell{display:block}.sidebar{position:static;height:auto;border-right:0;border-bottom:1px solid var(--border)}.sidebar nav{grid-template-columns:repeat(5,minmax(0,1fr))}.content{padding:20px 14px}.page-head,.ticket-card a,.event-topline{display:block}.summary{justify-content:flex-start;margin-top:8px}.filters form,.detail-grid,.trust-strip,.action-grid{grid-template-columns:1fr}.actions .button{margin-top:12px}.event-card{grid-template-columns:1fr}}
/* ===== Design tokens (dark) ===== */
@media(prefers-color-scheme:dark){:root{
color-scheme:dark;
--background:oklch(0.2178 0 0);--surface:#232725;--surface-2:#262c29;--card:#232725;--card-foreground:#e8eee9;
--foreground:#e8eee9;--muted:#303833;--muted-foreground:#acb9b0;--border:#38423c;--border-strong:#526258;--input:#617267;
--primary:#9ee0bf;--primary-hover:#b9efd3;--primary-foreground:#123c2d;--secondary:#232725;
--accent:#2c4436;--accent-foreground:#b9efd3;--ring:#9ee0bf;--focus-glow:rgba(158,224,191,.2);
--success:#4ade80;--success-subtle:#0f2a1b;--warning:#fbbf24;--warning-subtle:#2c2410;--destructive:#f87171;--destructive-hover:#ef4444;--destructive-subtle:#2c1618;--destructive-foreground:#2b0e0e;
--link:#9ee0bf;
--shadow-sm:0 1px 2px rgba(0,0,0,.1);--shadow-md:0 3px 10px rgba(0,0,0,.15);
--sidebar-bg:#1e2320;
}}`
}

func accessibilityCSS() string {
	return `
.skip-link{position:absolute;left:12px;top:-56px;z-index:10;padding:10px 14px;background:var(--primary);color:var(--primary-foreground);border-radius:7px}.skip-link:focus{top:12px}.sidebar nav a.active{background:var(--accent);color:var(--accent-foreground);font-weight:600}button,.button,.sidebar nav a{min-height:44px;touch-action:manipulation}.filters input,.auth-panel input,.proposed-actions input{min-height:44px;font-size:16px}.destructive{border-color:var(--destructive)!important;background:var(--destructive)!important;color:var(--destructive-foreground)!important}.destructive:hover{background:var(--destructive-hover)!important}@media(max-width:860px){.sidebar nav{grid-template-columns:repeat(3,minmax(0,1fr))}.sidebar nav a{overflow-wrap:anywhere}}@media(prefers-reduced-motion:reduce){*,*:before,*:after{scroll-behavior:auto!important;transition-duration:.01ms!important;animation-duration:.01ms!important;animation-iteration-count:1!important}}
`
}
