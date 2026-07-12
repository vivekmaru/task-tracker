package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/contracts"
	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/services"
	"github.com/vivek/agent-task-tracker/internal/web"
)

const (
	basePath           = "/api/v1"
	maxAPIRequestBytes = 1 << 20
)

func NewRouter() http.Handler {
	return NewRouterWithRuntime(nil)
}

func NewRouterWithRuntime(rt web.Runtime) http.Handler {
	return NewRouterWithRuntimeAndAuth(rt, web.AuthOptions{})
}

func NewRouterWithRuntimeAndAuth(rt web.Runtime, auth web.AuthOptions) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/livez", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if ready, ok := rt.(interface{ Ready(context.Context) error }); ok {
			if err := ready.Ready(r.Context()); err != nil {
				http.Error(w, "not ready", http.StatusServiceUnavailable)
				return
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	})
	apiMux := http.NewServeMux()
	api := humago.NewWithPrefix(apiMux, basePath, huma.DefaultConfig("Forge API", "0.1.0"))
	RegisterPhaseOneRoutes(api, rt)
	mux.Handle(basePath+"/", web.RequireAdminToken(auth, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxAPIRequestBytes)
		}
		apiMux.ServeHTTP(w, r)
	})))
	webHandler := web.NewHandlerWithAuth(rt, auth)
	mux.Handle("/login", webHandler)
	mux.Handle("/assets/", webHandler)
	mux.Handle("/tickets", webHandler)
	mux.Handle("/tickets/", webHandler)
	mux.Handle("/search", webHandler)
	mux.Handle("/events", webHandler)
	mux.Handle("/artifacts", webHandler)
	mux.Handle("/attempts/", webHandler)
	mux.Handle("/artifacts/", webHandler)
	mux.Handle("/proposed", webHandler)
	mux.Handle("/proposed/", webHandler)
	mux.Handle("/workspaces", webHandler)
	mux.Handle("/workspaces/", webHandler)
	return requestIDMiddleware(mux)
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			var bytes [16]byte
			if _, err := rand.Read(bytes[:]); err == nil {
				id = hex.EncodeToString(bytes[:])
			} else {
				id = "forge-request"
			}
		}
		w.Header().Set("X-Request-ID", id)
		started := time.Now()
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)
		slog.Info("http request", "request_id", id, "method", r.Method, "path", r.URL.Path, "status", recorder.status, "duration_ms", time.Since(started).Milliseconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func RegisterPhaseOneRoutes(api huma.API, rt web.Runtime) {
	registerResourceRoutes(api, rt)
	register[idBodyInput](api, http.MethodPost, "/tickets/{id}/ready", contracts.RESTMarkTicketReady, "Move ticket to todo")
	register[idBodyInput](api, http.MethodPost, "/tickets/{id}/reopen", contracts.RESTReopenTicket, "Reopen ticket")
	register[idBodyInput](api, http.MethodPost, "/tickets/{id}/unblock", contracts.RESTUnblockTicket, "Unblock ticket")
	register[idBodyInput](api, http.MethodPost, "/tickets/{id}/request-review", contracts.RESTRequestReview, "Request ticket review")
	register[idBodyInput](api, http.MethodPost, "/tickets/{id}/review", contracts.RESTReviewTicket, "Review ticket")
	register[idBodyInput](api, http.MethodPost, "/tickets/{id}/archive", contracts.RESTArchiveTicket, "Archive ticket")

	registerLifecycleRoutes(api, rt)
	registerEventRoutes(api, rt)

	registerAnalyticsRoutes(api, rt)
	registerObservabilityRoutes(api, rt)
}

func register[I any](api huma.API, method, path, operationID, summary string) {
	huma.Register[I, placeholderOutput](api, huma.Operation{
		OperationID: operationID,
		Method:      method,
		Path:        path,
		Summary:     summary,
		Tags:        []string{"Phase 1"},
	}, func(context.Context, *I) (*placeholderOutput, error) {
		return nil, huma.Error501NotImplemented("route is registered; handler wiring is not implemented yet")
	})
}

type bodyInput struct {
	Body map[string]any `json:"body,omitempty"`
}

type idInput struct {
	ID string `path:"id" doc:"Resource ID"`
}

type idBodyInput struct {
	ID   string         `path:"id" doc:"Resource ID"`
	Body map[string]any `json:"body,omitempty"`
}

type listTicketsInput struct {
	WorkspaceID string `query:"workspace_id,omitempty"`
	ProjectID   string `query:"project_id,omitempty"`
	Status      string `query:"status,omitempty"`
	Type        string `query:"type,omitempty"`
	Offset      int32  `query:"offset,omitempty"`
	Limit       int32  `query:"limit,omitempty"`
}

type placeholderOutput struct {
	Body map[string]string `json:"body"`
}

type analyticsRuntime interface {
	AnalyticsSummary(context.Context, services.AnalyticsFilter) (services.AnalyticsSummary, error)
	AnalyticsByModel(context.Context, services.AnalyticsFilter) ([]services.AnalyticsGroup, error)
	AnalyticsByHarness(context.Context, services.AnalyticsFilter) ([]services.AnalyticsGroup, error)
	AnalyticsByStatus(context.Context, services.AnalyticsFilter) ([]services.AnalyticsGroup, error)
	AnalyticsByAgent(context.Context, services.AnalyticsFilter) ([]services.AnalyticsGroup, error)
}

type eventRuntime interface {
	ListEvents(context.Context, services.ListEventsRequest) (services.ListEventsResult, error)
}

type observabilityRuntime interface {
	CreateWebhookSubscription(context.Context, db.CreateWebhookSubscriptionParams) (db.WebhookSubscription, error)
	ListWebhookSubscriptions(context.Context, db.ListWebhookSubscriptionsParams) ([]db.WebhookSubscription, error)
}

type eventsInput struct {
	WorkspaceID string `query:"workspace_id,omitempty"`
	ProjectID   string `query:"project_id,omitempty"`
	TicketID    string `query:"ticket_id,omitempty"`
	AttemptID   string `query:"attempt_id,omitempty"`
	Cursor      string `query:"cursor,omitempty"`
	Limit       int32  `query:"limit,omitempty"`
}

type eventsOutput struct {
	Body services.ListEventsResult `json:"body"`
}

func registerEventRoutes(api huma.API, rt web.Runtime) {
	events, _ := rt.(eventRuntime)
	huma.Register[eventsInput, eventsOutput](api, huma.Operation{
		OperationID: "list-events",
		Method:      http.MethodGet,
		Path:        "/events",
		Summary:     "List ticket ledger events",
		Tags:        []string{"Phase 5"},
	}, func(ctx context.Context, input *eventsInput) (*eventsOutput, error) {
		if events == nil {
			return nil, huma.Error501NotImplemented("route is registered; handler wiring is not implemented yet")
		}
		req, err := listEventsRequest(input)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		result, err := events.ListEvents(ctx, req)
		if err != nil {
			var validationErr services.ValidationError
			if errors.As(err, &validationErr) {
				return nil, huma.Error400BadRequest(validationErr.Error())
			}
			return nil, huma.Error500InternalServerError("event feed failed", err)
		}
		return &eventsOutput{Body: result}, nil
	})
}

func listEventsRequest(input *eventsInput) (services.ListEventsRequest, error) {
	workspaceID, err := parseOptionalUUID(input.WorkspaceID)
	if err != nil {
		return services.ListEventsRequest{}, err
	}
	projectID, err := parseOptionalUUID(input.ProjectID)
	if err != nil {
		return services.ListEventsRequest{}, err
	}
	ticketID, err := parseOptionalUUID(input.TicketID)
	if err != nil {
		return services.ListEventsRequest{}, err
	}
	attemptID, err := parseOptionalUUID(input.AttemptID)
	if err != nil {
		return services.ListEventsRequest{}, err
	}
	return services.ListEventsRequest{
		WorkspaceID: workspaceID,
		ProjectID:   projectID,
		TicketID:    ticketID,
		AttemptID:   attemptID,
		Cursor:      input.Cursor,
		Limit:       input.Limit,
	}, nil
}

type analyticsInput struct {
	WorkspaceID string `query:"workspace_id,omitempty"`
	ProjectID   string `query:"project_id,omitempty"`
}

type analyticsSummaryOutput struct {
	Body struct {
		Summary services.AnalyticsSummary `json:"summary"`
	} `json:"body"`
}

type analyticsGroupsOutput struct {
	Body struct {
		Groups []services.AnalyticsGroup `json:"groups"`
	} `json:"body"`
}

func registerAnalyticsRoutes(api huma.API, rt web.Runtime) {
	analytics, _ := rt.(analyticsRuntime)
	huma.Register[analyticsInput, analyticsSummaryOutput](api, huma.Operation{
		OperationID: contracts.RESTAnalyticsSummary,
		Method:      http.MethodGet,
		Path:        "/analytics/summary",
		Summary:     "Analytics summary",
		Tags:        []string{"Phase 4"},
	}, func(ctx context.Context, input *analyticsInput) (*analyticsSummaryOutput, error) {
		if analytics == nil {
			return nil, huma.Error501NotImplemented("route is registered; handler wiring is not implemented yet")
		}
		filter, err := analyticsFilter(input.WorkspaceID, input.ProjectID)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		summary, err := analytics.AnalyticsSummary(ctx, filter)
		if err != nil {
			return nil, huma.Error500InternalServerError("analytics summary failed", err)
		}
		out := &analyticsSummaryOutput{}
		out.Body.Summary = summary
		return out, nil
	})
	huma.Register[analyticsInput, analyticsGroupsOutput](api, huma.Operation{
		OperationID: contracts.RESTAnalyticsByModel,
		Method:      http.MethodGet,
		Path:        "/analytics/by-model",
		Summary:     "Analytics by model",
		Tags:        []string{"Phase 4"},
	}, func(ctx context.Context, input *analyticsInput) (*analyticsGroupsOutput, error) {
		if analytics == nil {
			return nil, huma.Error501NotImplemented("route is registered; handler wiring is not implemented yet")
		}
		filter, err := analyticsFilter(input.WorkspaceID, input.ProjectID)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		groups, err := analytics.AnalyticsByModel(ctx, filter)
		if err != nil {
			return nil, huma.Error500InternalServerError("analytics by model failed", err)
		}
		out := &analyticsGroupsOutput{}
		out.Body.Groups = groups
		return out, nil
	})
	huma.Register[analyticsInput, analyticsGroupsOutput](api, huma.Operation{
		OperationID: contracts.RESTAnalyticsByHarness,
		Method:      http.MethodGet,
		Path:        "/analytics/by-harness",
		Summary:     "Analytics by harness",
		Tags:        []string{"Phase 4"},
	}, func(ctx context.Context, input *analyticsInput) (*analyticsGroupsOutput, error) {
		if analytics == nil {
			return nil, huma.Error501NotImplemented("route is registered; handler wiring is not implemented yet")
		}
		filter, err := analyticsFilter(input.WorkspaceID, input.ProjectID)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		groups, err := analytics.AnalyticsByHarness(ctx, filter)
		if err != nil {
			return nil, huma.Error500InternalServerError("analytics by harness failed", err)
		}
		out := &analyticsGroupsOutput{}
		out.Body.Groups = groups
		return out, nil
	})
	huma.Register[analyticsInput, analyticsGroupsOutput](api, huma.Operation{
		OperationID: contracts.RESTAnalyticsByStatus,
		Method:      http.MethodGet,
		Path:        "/analytics/by-status",
		Summary:     "Analytics by status",
		Tags:        []string{"Phase 4"},
	}, func(ctx context.Context, input *analyticsInput) (*analyticsGroupsOutput, error) {
		if analytics == nil {
			return nil, huma.Error501NotImplemented("route is registered; handler wiring is not implemented yet")
		}
		filter, err := analyticsFilter(input.WorkspaceID, input.ProjectID)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		groups, err := analytics.AnalyticsByStatus(ctx, filter)
		if err != nil {
			return nil, huma.Error500InternalServerError("analytics by status failed", err)
		}
		out := &analyticsGroupsOutput{}
		out.Body.Groups = groups
		return out, nil
	})
	huma.Register[analyticsInput, analyticsGroupsOutput](api, huma.Operation{
		OperationID: contracts.RESTAnalyticsByAgent,
		Method:      http.MethodGet,
		Path:        "/analytics/by-agent",
		Summary:     "Analytics by agent",
		Tags:        []string{"Phase 4"},
	}, func(ctx context.Context, input *analyticsInput) (*analyticsGroupsOutput, error) {
		if analytics == nil {
			return nil, huma.Error501NotImplemented("route is registered; handler wiring is not implemented yet")
		}
		filter, err := analyticsFilter(input.WorkspaceID, input.ProjectID)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		groups, err := analytics.AnalyticsByAgent(ctx, filter)
		if err != nil {
			return nil, huma.Error500InternalServerError("analytics by agent failed", err)
		}
		out := &analyticsGroupsOutput{}
		out.Body.Groups = groups
		return out, nil
	})
}

type observabilitySubscriptionsInput struct {
	WorkspaceID string `query:"workspace_id" doc:"Workspace ID"`
	ProjectID   string `query:"project_id" doc:"Project ID"`
	All         bool   `query:"all,omitempty" doc:"Include inactive subscriptions"`
}

type createObservabilitySubscriptionInput struct {
	Body createObservabilitySubscriptionBody `json:"body"`
}

type createObservabilitySubscriptionBody struct {
	WorkspaceID string   `json:"workspace_id"`
	ProjectID   string   `json:"project_id"`
	EndpointURL string   `json:"endpoint_url"`
	Secret      string   `json:"secret,omitempty"`
	EventTypes  []string `json:"event_types,omitempty"`
	Active      *bool    `json:"active,omitempty"`
	MaxAttempts *int32   `json:"max_attempts,omitempty"`
	Description string   `json:"description,omitempty"`
}

type observabilitySubscriptionsOutput struct {
	Body struct {
		Subscriptions []observabilitySubscriptionPayload `json:"subscriptions"`
	} `json:"body"`
}

type observabilitySubscriptionOutput struct {
	Body struct {
		Subscription observabilitySubscriptionPayload `json:"subscription"`
	} `json:"body"`
}

type observabilitySubscriptionPayload struct {
	ID          string   `json:"id"`
	WorkspaceID string   `json:"workspace_id"`
	ProjectID   string   `json:"project_id"`
	EndpointURL string   `json:"endpoint_url"`
	SecretSet   bool     `json:"secret_set"`
	EventTypes  []string `json:"event_types"`
	Active      bool     `json:"active"`
	MaxAttempts int32    `json:"max_attempts"`
	Description string   `json:"description"`
}

var supportedObservabilityEventTypes = map[string]struct{}{
	"created":          {},
	"proposed":         {},
	"claimed":          {},
	"heartbeat":        {},
	"checkpointed":     {},
	"updated":          {},
	"completed":        {},
	"failed":           {},
	"blocked":          {},
	"expired":          {},
	"cancelled":        {},
	"ready":            {},
	"reopened":         {},
	"unblocked":        {},
	"review_requested": {},
	"reviewed":         {},
	"archived":         {},
}

func registerObservabilityRoutes(api huma.API, rt web.Runtime) {
	observability, _ := rt.(observabilityRuntime)
	huma.Register[observabilitySubscriptionsInput, observabilitySubscriptionsOutput](api, huma.Operation{
		OperationID: "list-observability-subscriptions",
		Method:      http.MethodGet,
		Path:        "/observability/subscriptions",
		Summary:     "List observability subscriptions",
		Tags:        []string{"Phase 5"},
	}, func(ctx context.Context, input *observabilitySubscriptionsInput) (*observabilitySubscriptionsOutput, error) {
		if observability == nil {
			return nil, huma.Error501NotImplemented("route is registered; handler wiring is not implemented yet")
		}
		workspaceID, projectID, err := observabilityScope(input.WorkspaceID, input.ProjectID)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		subscriptions, err := observability.ListWebhookSubscriptions(ctx, db.ListWebhookSubscriptionsParams{
			WorkspaceID: workspaceID,
			ProjectID:   projectID,
			ActiveOnly:  !input.All,
		})
		if err != nil {
			return nil, huma.Error500InternalServerError("observability subscription list failed", err)
		}
		out := &observabilitySubscriptionsOutput{}
		out.Body.Subscriptions = observabilitySubscriptionPayloads(subscriptions)
		return out, nil
	})

	huma.Register[createObservabilitySubscriptionInput, observabilitySubscriptionOutput](api, huma.Operation{
		OperationID: "create-observability-subscription",
		Method:      http.MethodPost,
		Path:        "/observability/subscriptions",
		Summary:     "Create observability subscription",
		Tags:        []string{"Phase 5"},
	}, func(ctx context.Context, input *createObservabilitySubscriptionInput) (*observabilitySubscriptionOutput, error) {
		if observability == nil {
			return nil, huma.Error501NotImplemented("route is registered; handler wiring is not implemented yet")
		}
		req, err := createObservabilitySubscriptionParams(input.Body)
		if err != nil {
			return nil, huma.Error400BadRequest(err.Error())
		}
		subscription, err := observability.CreateWebhookSubscription(ctx, req)
		if err != nil {
			return nil, observabilitySubscriptionCreateError(err)
		}
		out := &observabilitySubscriptionOutput{}
		out.Body.Subscription = makeObservabilitySubscriptionPayload(subscription)
		return out, nil
	})
}

func observabilityScope(workspaceIDText, projectIDText string) (pgtype.UUID, pgtype.UUID, error) {
	workspaceID, err := parseRequiredUUID("workspace_id", workspaceIDText)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	projectID, err := parseRequiredUUID("project_id", projectIDText)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	return workspaceID, projectID, nil
}

func createObservabilitySubscriptionParams(body createObservabilitySubscriptionBody) (db.CreateWebhookSubscriptionParams, error) {
	workspaceID, projectID, err := observabilityScope(body.WorkspaceID, body.ProjectID)
	if err != nil {
		return db.CreateWebhookSubscriptionParams{}, err
	}
	if strings.TrimSpace(body.EndpointURL) == "" {
		return db.CreateWebhookSubscriptionParams{}, errors.New("endpoint_url is required")
	}
	if err := validateWebhookEndpointURL(body.EndpointURL); err != nil {
		return db.CreateWebhookSubscriptionParams{}, err
	}
	maxAttempts := int32(3)
	if body.MaxAttempts != nil {
		maxAttempts = *body.MaxAttempts
	}
	if maxAttempts <= 0 {
		return db.CreateWebhookSubscriptionParams{}, errors.New("max_attempts must be greater than zero")
	}
	active := true
	if body.Active != nil {
		active = *body.Active
	}
	eventTypes, err := normalizeObservabilityEventTypes(body.EventTypes)
	if err != nil {
		return db.CreateWebhookSubscriptionParams{}, err
	}
	return db.CreateWebhookSubscriptionParams{
		WorkspaceID: workspaceID,
		ProjectID:   projectID,
		EndpointUrl: strings.TrimSpace(body.EndpointURL),
		Secret:      pgtype.Text{String: strings.TrimSpace(body.Secret), Valid: strings.TrimSpace(body.Secret) != ""},
		EventTypes:  eventTypes,
		Active:      active,
		MaxAttempts: maxAttempts,
		Description: strings.TrimSpace(body.Description),
	}, nil
}

func normalizeObservabilityEventTypes(values []string) ([]string, error) {
	if values == nil {
		return []string{}, nil
	}
	eventTypes := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := supportedObservabilityEventTypes[value]; !ok {
			return nil, errors.New("unsupported event_type " + value)
		}
		eventTypes = append(eventTypes, value)
	}
	return eventTypes, nil
}

func observabilitySubscriptionCreateError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23503":
			return huma.Error404NotFound("referenced workspace or project does not exist", err)
		case "23505":
			return huma.Error409Conflict("observability subscription already exists", err)
		case "23502", "23514":
			return huma.Error400BadRequest("invalid observability subscription", err)
		}
	}
	return huma.Error500InternalServerError("observability subscription create failed", err)
}

func validateWebhookEndpointURL(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("endpoint_url must use http or https")
	}
	if parsed.Host == "" {
		return errors.New("endpoint_url must include a host")
	}
	if ip, err := netip.ParseAddr(parsed.Hostname()); err == nil {
		ip = ip.Unmap()
		if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() || ip.IsPrivate() {
			return errors.New("endpoint_url must not use a private or local IP address")
		}
	}
	return nil
}

func makeObservabilitySubscriptionPayload(subscription db.WebhookSubscription) observabilitySubscriptionPayload {
	return observabilitySubscriptionPayload{
		ID:          uuidText(subscription.ID),
		WorkspaceID: uuidText(subscription.WorkspaceID),
		ProjectID:   uuidText(subscription.ProjectID),
		EndpointURL: subscription.EndpointUrl,
		SecretSet:   subscription.Secret.Valid && subscription.Secret.String != "",
		EventTypes:  subscription.EventTypes,
		Active:      subscription.Active,
		MaxAttempts: subscription.MaxAttempts,
		Description: subscription.Description,
	}
}

func observabilitySubscriptionPayloads(subscriptions []db.WebhookSubscription) []observabilitySubscriptionPayload {
	out := make([]observabilitySubscriptionPayload, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		out = append(out, makeObservabilitySubscriptionPayload(subscription))
	}
	return out
}

func analyticsFilter(workspaceID, projectID string) (services.AnalyticsFilter, error) {
	workspaceUUID, err := parseOptionalUUID(workspaceID)
	if err != nil {
		return services.AnalyticsFilter{}, err
	}
	projectUUID, err := parseOptionalUUID(projectID)
	if err != nil {
		return services.AnalyticsFilter{}, err
	}
	return services.AnalyticsFilter{WorkspaceID: workspaceUUID, ProjectID: projectUUID}, nil
}

func parseRequiredUUID(name, value string) (pgtype.UUID, error) {
	id, err := parseOptionalUUID(value)
	if err != nil {
		return pgtype.UUID{}, err
	}
	if !id.Valid {
		return pgtype.UUID{}, errors.New(name + " is required")
	}
	return id, nil
}

func uuidText(id pgtype.UUID) string {
	value, err := id.Value()
	if err != nil {
		return ""
	}
	text, _ := value.(string)
	return text
}

func parseOptionalUUID(value string) (pgtype.UUID, error) {
	var id pgtype.UUID
	value = strings.TrimSpace(value)
	if value == "" {
		return id, nil
	}
	if err := id.Scan(value); err != nil {
		return pgtype.UUID{}, err
	}
	return id, nil
}
