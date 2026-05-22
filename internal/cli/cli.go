package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/api"
	"github.com/vivek/agent-task-tracker/internal/config"
	"github.com/vivek/agent-task-tracker/internal/contracts"
	"github.com/vivek/agent-task-tracker/internal/db"
	forgemcp "github.com/vivek/agent-task-tracker/internal/mcp"
	forgeruntime "github.com/vivek/agent-task-tracker/internal/runtime"
	"github.com/vivek/agent-task-tracker/internal/services"
	forgetui "github.com/vivek/agent-task-tracker/internal/tui"
	"github.com/vivek/agent-task-tracker/internal/web"
)

type command struct {
	name        string
	description string
}

var commands = []command{
	{"server", "Start the Forge API server."},
	{"worker", "Run Forge background workers."},
	{"mcp", "Start the Forge MCP server."},
	{"tui", "Open the Forge terminal UI."},
	{"create", "Create a ticket."},
	{"propose", "Propose agent-discovered work."},
	{"claim-next", "Atomically claim the next eligible ticket."},
	{"heartbeat", "Extend an attempt lease."},
	{"checkpoint", "Record resumable attempt progress."},
	{"complete", "Complete an attempt."},
	{"fail", "Fail an attempt."},
	{"block", "Mark an attempt blocked."},
	{"cancel", "Cancel an attempt."},
	{"attach", "Attach or register proof artifacts."},
	{"list", "List tickets."},
	{"get", "Show a ticket or attempt."},
	{"analytics", "Show basic attempt metrics and analytics."},
	{"codex", "Codex harness convenience commands."},
}

type RuntimeHandle interface {
	Close()
	CreateTicket(context.Context, services.CreateTicketRequest) (db.Ticket, error)
	ProposeTicket(context.Context, services.CreateTicketRequest) (db.Ticket, error)
	CreateTicketFromAttempt(context.Context, services.CreateTicketFromAttemptRequest) (db.Ticket, error)
	UpdateTicket(context.Context, services.UpdateTicketRequest) (db.Ticket, error)
	MarkReady(context.Context, services.TicketTransitionRequest) (db.Ticket, error)
	Reopen(context.Context, services.TicketTransitionRequest) (db.Ticket, error)
	Unblock(context.Context, services.TicketTransitionRequest) (db.Ticket, error)
	RequestReview(context.Context, services.TicketTransitionRequest) (db.Ticket, error)
	Review(context.Context, services.ReviewTicketRequest) (db.Ticket, error)
	Archive(context.Context, services.TicketTransitionRequest) (db.Ticket, error)
	ClaimNext(context.Context, services.ClaimNextRequest) (services.ClaimNextResult, error)
	Heartbeat(context.Context, services.HeartbeatRequest) (db.Attempt, error)
	Checkpoint(context.Context, services.CheckpointRequest) (services.CheckpointResult, error)
	Complete(context.Context, services.CompleteAttemptRequest) (services.AttemptTransitionResult, error)
	CompleteWithArtifacts(context.Context, services.CompleteAttemptRequest, []services.RegisterArtifactRequest) (services.AttemptTransitionResult, []db.Artifact, error)
	Fail(context.Context, services.FailAttemptRequest) (services.AttemptTransitionResult, error)
	Block(context.Context, services.BlockAttemptRequest) (services.AttemptTransitionResult, error)
	BlockWithArtifacts(context.Context, services.BlockAttemptRequest, []services.RegisterArtifactRequest) (services.AttemptTransitionResult, []db.Artifact, error)
	Cancel(context.Context, services.CancelAttemptRequest) (services.AttemptTransitionResult, error)
	ListTickets(context.Context, services.ListTicketsRequest) ([]db.Ticket, error)
	SearchTickets(context.Context, services.SearchTicketsRequest) ([]services.SearchResult, error)
	GetTicket(context.Context, pgtype.UUID) (db.Ticket, error)
	GetAttempt(context.Context, pgtype.UUID) (db.Attempt, error)
	ListAttemptsByTicket(context.Context, pgtype.UUID) ([]db.Attempt, error)
	ListAttemptCheckpointsByTicket(context.Context, pgtype.UUID) ([]db.AttemptCheckpoint, error)
	ListTicketEventsByTicket(context.Context, pgtype.UUID) ([]db.TicketEvent, error)
	ListArtifactsByTicket(context.Context, pgtype.UUID) ([]db.Artifact, error)
	RegisterArtifact(context.Context, services.RegisterArtifactRequest) (db.Artifact, error)
	ListArtifactsByAttempt(context.Context, pgtype.UUID) ([]db.Artifact, error)
	GetArtifact(context.Context, pgtype.UUID) (db.Artifact, error)
	ListWorkspaces(context.Context) ([]db.Workspace, error)
	GetWorkspace(context.Context, pgtype.UUID) (db.Workspace, error)
	CreateWorkspace(context.Context, string) (db.Workspace, error)
	ListProjectsByWorkspace(context.Context, pgtype.UUID) ([]db.Project, error)
	CreateProject(context.Context, pgtype.UUID, string) (db.Project, error)
	DecomposeTicket(context.Context, services.DecomposeTicketRequest) (services.DecomposeTicketResult, error)
	RegisterCapabilities(context.Context, services.RegisterCapabilitiesRequest) (db.AgentCapability, error)
	AnalyticsSummary(context.Context, services.AnalyticsFilter) (services.AnalyticsSummary, error)
	AnalyticsByModel(context.Context, services.AnalyticsFilter) ([]services.AnalyticsGroup, error)
	AnalyticsByHarness(context.Context, services.AnalyticsFilter) ([]services.AnalyticsGroup, error)
}

type Dependencies struct {
	OpenRuntime func(context.Context, config.Config) (RuntimeHandle, error)
	RunTUI      func(context.Context, io.Writer, RuntimeHandle, forgetui.Options) error
	ServeHTTP   func(context.Context, string, http.Handler) error
}

// Run executes the Forge CLI and returns a process-style exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	return RunWithDependencies(args, stdout, stderr, Dependencies{
		OpenRuntime: openRuntime,
	})
}

func RunWithDependencies(args []string, stdout, stderr io.Writer, deps Dependencies) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printHelp(stdout)
		return 0
	}

	name := args[0]
	if !isKnownCommand(name) {
		fmt.Fprintf(stderr, "unknown command %q\n\n", name)
		printHelp(stderr)
		return 2
	}

	if len(args) > 1 && (args[1] == "help" || args[1] == "--help" || args[1] == "-h") {
		printCommandHelp(stdout, name)
		return 0
	}

	if name == "server" || name == "worker" || name == "mcp" || name == "tui" {
		return runProcess(name, args[1:], stdout, stderr, deps)
	}
	if name == "codex" {
		if deps.OpenRuntime == nil {
			deps.OpenRuntime = openRuntime
		}
		return runCodexCommand(context.Background(), args[1:], stdout, stderr, deps)
	}
	if isRuntimeCommand(name) {
		return runRuntimeCommand(name, args[1:], stdout, stderr, deps)
	}

	fmt.Fprintf(stderr, "command %q is not implemented yet\n", name)
	return 1
}

func runRuntimeCommand(name string, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	if deps.OpenRuntime == nil {
		deps.OpenRuntime = openRuntime
	}
	ctx := context.Background()
	switch name {
	case "create", "propose":
		return runCreateCommand(ctx, name, args, stdout, stderr, deps)
	case "claim-next":
		return runClaimNextCommand(ctx, args, stdout, stderr, deps)
	case "list":
		return runListCommand(ctx, args, stdout, stderr, deps)
	case "get":
		return runGetCommand(ctx, args, stdout, stderr, deps)
	case "heartbeat":
		return runHeartbeatCommand(ctx, args, stdout, stderr, deps)
	case "checkpoint":
		return runCheckpointCommand(ctx, args, stdout, stderr, deps)
	case "complete":
		return runCompleteCommand(ctx, args, stdout, stderr, deps)
	case "fail":
		return runFailCommand(ctx, args, stdout, stderr, deps)
	case "block":
		return runBlockCommand(ctx, args, stdout, stderr, deps)
	case "cancel":
		return runCancelCommand(ctx, args, stdout, stderr, deps)
	case "attach":
		return runAttachCommand(ctx, args, stdout, stderr, deps)
	case "analytics":
		return runAnalyticsCommand(ctx, args, stdout, stderr, deps)
	}
	fmt.Fprintf(stderr, "command %q is not implemented yet\n", name)
	return 1
}

func runProcess(name string, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	opts, ok := parseProcessOptions(name, args, stderr)
	if !ok {
		return 2
	}

	cfg, err := config.Load(opts.Options)
	if err != nil {
		fmt.Fprintf(stderr, "%s configuration error: %v\n", name, err)
		return 2
	}
	if deps.OpenRuntime == nil {
		deps.OpenRuntime = openRuntime
	}

	switch name {
	case "tui":
		if err := cfg.ValidateRuntime(); err != nil {
			fmt.Fprintf(stderr, "tui configuration error: %v\n", err)
			return 2
		}
		workspaceID, err := optionalUUID(opts.TUIWorkspaceID)
		if err != nil {
			fmt.Fprintf(stderr, "tui argument error: --workspace-id must be a UUID: %v\n", err)
			return 2
		}
		projectID, err := optionalUUID(opts.TUIProjectID)
		if err != nil {
			fmt.Fprintf(stderr, "tui argument error: --project-id must be a UUID: %v\n", err)
			return 2
		}
		rt, err := deps.OpenRuntime(context.Background(), cfg)
		if err != nil {
			fmt.Fprintf(stderr, "tui runtime error: %v\n", err)
			return 1
		}
		defer rt.Close()
		if deps.RunTUI == nil {
			deps.RunTUI = func(ctx context.Context, output io.Writer, rt RuntimeHandle, opts forgetui.Options) error {
				return forgetui.Run(ctx, output, rt, opts)
			}
		}
		tuiOpts := forgetui.Options{
			WorkspaceID: workspaceID,
			ProjectID:   projectID,
			Status:      opts.TUIStatus,
			Type:        opts.TUIType,
			Limit:       int32(opts.TUILimit),
		}
		if err := deps.RunTUI(context.Background(), stdout, rt, tuiOpts); err != nil {
			fmt.Fprintf(stderr, "tui error: %v\n", err)
			return 1
		}
	case "server":
		if err := cfg.ValidateServer(); err != nil {
			fmt.Fprintf(stderr, "server configuration error: %v\n", err)
			return 2
		}
		rt, err := deps.OpenRuntime(context.Background(), cfg)
		if err != nil {
			fmt.Fprintf(stderr, "server runtime error: %v\n", err)
			return 1
		}
		defer rt.Close()
		if deps.ServeHTTP == nil {
			deps.ServeHTTP = serveHTTP
		}
		fmt.Fprintf(stdout, "server listening on %s\n", cfg.HTTPAddr)
		if err := deps.ServeHTTP(context.Background(), cfg.HTTPAddr, api.NewRouterWithRuntimeAndAuth(rt, webAuthOptions(cfg))); err != nil {
			fmt.Fprintf(stderr, "server HTTP error: %v\n", err)
			return 1
		}
	case "mcp":
		if err := cfg.ValidateRuntime(); err != nil {
			fmt.Fprintf(stderr, "mcp configuration error: %v\n", err)
			return 2
		}
		rt, err := deps.OpenRuntime(context.Background(), cfg)
		if err != nil {
			fmt.Fprintf(stderr, "mcp runtime error: %v\n", err)
			return 1
		}
		defer rt.Close()
		server, err := forgemcp.NewServer(rt, contracts.AllOperations())
		if err != nil {
			fmt.Fprintf(stderr, "mcp startup error: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "mcp runtime configuration ok; registered %d tools; protocol serving not implemented yet\n", len(server.Tools()))
	case "worker":
		if err := cfg.ValidateWorker(); err != nil {
			fmt.Fprintf(stderr, "worker configuration error: %v\n", err)
			return 2
		}
		rt, err := deps.OpenRuntime(context.Background(), cfg)
		if err != nil {
			fmt.Fprintf(stderr, "worker runtime error: %v\n", err)
			return 1
		}
		defer rt.Close()
		fmt.Fprintln(stdout, "worker runtime configuration ok; River worker loop not implemented yet")
	}
	return 0
}

func webAuthOptions(cfg config.Config) web.AuthOptions {
	return web.AuthOptions{
		AdminToken:   cfg.AdminToken,
		SecureCookie: cfg.AuthCookieSecure,
	}
}

func openRuntime(ctx context.Context, cfg config.Config) (RuntimeHandle, error) {
	return forgeruntime.Open(ctx, cfg)
}

func serveHTTP(ctx context.Context, addr string, handler http.Handler) error {
	server := newHTTPServer(addr, handler)
	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()
	err := server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
}

func runCreateCommand(ctx context.Context, name string, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	flags := newFlagSet(name, stderr)
	var opts commandOptions
	opts.bind(flags)
	var req createTicketFlags
	req.bind(flags)
	if !parseFlags(flags, args) {
		return 2
	}

	rt, ok := openCommandRuntime(ctx, flags.Name(), opts, stderr, deps)
	if !ok {
		return 1
	}
	defer rt.Close()

	ticketReq, err := req.request()
	if err != nil {
		fmt.Fprintf(stderr, "%s argument error: %v\n", name, err)
		return 2
	}
	var ticket db.Ticket
	if name == "propose" {
		ticketReq.CreatedBy = firstNonEmpty(ticketReq.CreatedBy, services.ActorAgent)
		ticket, err = rt.ProposeTicket(ctx, ticketReq)
	} else {
		ticketReq.CreatedBy = firstNonEmpty(ticketReq.CreatedBy, services.ActorHuman)
		ticket, err = rt.CreateTicket(ctx, ticketReq)
	}
	if err != nil {
		fmt.Fprintf(stderr, "%s error: %v\n", name, err)
		return 1
	}
	return writeJSON(stdout, stderr, ticketPayload(ticket))
}

func runClaimNextCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	flags := newFlagSet("claim-next", stderr)
	var opts commandOptions
	opts.bind(flags)
	var workspaceID, projectID, ticketType, harness, agentID, model, lease, idempotencyKey string
	var tags, capabilities stringList
	flags.StringVar(&workspaceID, "workspace-id", "", "workspace id")
	flags.StringVar(&projectID, "project-id", "", "project id")
	flags.StringVar(&ticketType, "type", "", "ticket type filter")
	flags.Var(&tags, "tag", "ticket tag filter")
	flags.StringVar(&harness, "harness", "", "agent harness")
	flags.Var(&capabilities, "capability", "agent capability")
	flags.StringVar(&agentID, "agent-id", "", "agent id")
	flags.StringVar(&model, "model", "", "model")
	flags.StringVar(&lease, "lease", "30m", "attempt lease duration")
	flags.StringVar(&idempotencyKey, "idempotency-key", "", "idempotency key")
	if !parseFlags(flags, args) {
		return 2
	}
	rt, ok := openCommandRuntime(ctx, flags.Name(), opts, stderr, deps)
	if !ok {
		return 1
	}
	defer rt.Close()

	duration, err := time.ParseDuration(lease)
	if err != nil {
		fmt.Fprintf(stderr, "claim-next argument error: lease must be a duration: %v\n", err)
		return 2
	}
	result, err := rt.ClaimNext(ctx, services.ClaimNextRequest{
		WorkspaceID:    mustUUID(workspaceID),
		ProjectID:      mustUUID(projectID),
		Type:           ticketType,
		Tags:           tags,
		Harness:        harness,
		Capabilities:   capabilities,
		AgentID:        agentID,
		Model:          model,
		Lease:          duration,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		fmt.Fprintf(stderr, "claim-next error: %v\n", err)
		return 1
	}
	return writeJSON(stdout, stderr, claimPayload(result))
}

func runListCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	flags := newFlagSet("list", stderr)
	var opts commandOptions
	opts.bind(flags)
	var workspaceID, projectID, status, ticketType string
	var offset, limit int
	flags.StringVar(&workspaceID, "workspace-id", "", "workspace id")
	flags.StringVar(&projectID, "project-id", "", "project id")
	flags.StringVar(&status, "status", "", "status filter")
	flags.StringVar(&ticketType, "type", "", "type filter")
	flags.IntVar(&offset, "offset", 0, "offset")
	flags.IntVar(&limit, "limit", 50, "limit")
	if !parseFlags(flags, args) {
		return 2
	}
	rt, ok := openCommandRuntime(ctx, flags.Name(), opts, stderr, deps)
	if !ok {
		return 1
	}
	defer rt.Close()
	tickets, err := rt.ListTickets(ctx, services.ListTicketsRequest{
		WorkspaceID: mustUUID(workspaceID),
		ProjectID:   mustUUID(projectID),
		Status:      status,
		Type:        ticketType,
		Offset:      int32(offset),
		Limit:       int32(limit),
	})
	if err != nil {
		fmt.Fprintf(stderr, "list error: %v\n", err)
		return 1
	}
	out := make([]map[string]any, 0, len(tickets))
	for _, ticket := range tickets {
		out = append(out, ticketPayload(ticket))
	}
	return writeJSON(stdout, stderr, map[string]any{"tickets": out})
}

func runGetCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	flags := newFlagSet("get", stderr)
	var opts commandOptions
	opts.bind(flags)
	var id, kind string
	flags.StringVar(&id, "id", "", "resource id")
	flags.StringVar(&kind, "kind", "ticket", "ticket or attempt")
	if !parseFlags(flags, args) {
		return 2
	}
	if id == "" && flags.NArg() > 0 {
		id = flags.Arg(0)
	}
	rt, ok := openCommandRuntime(ctx, flags.Name(), opts, stderr, deps)
	if !ok {
		return 1
	}
	defer rt.Close()
	parsedID := mustUUID(id)
	switch kind {
	case "ticket":
		ticket, err := rt.GetTicket(ctx, parsedID)
		if err != nil {
			fmt.Fprintf(stderr, "get error: %v\n", err)
			return 1
		}
		return writeJSON(stdout, stderr, ticketPayload(ticket))
	case "attempt":
		attempt, err := rt.GetAttempt(ctx, parsedID)
		if err != nil {
			fmt.Fprintf(stderr, "get error: %v\n", err)
			return 1
		}
		return writeJSON(stdout, stderr, attemptPayload(attempt))
	default:
		fmt.Fprintf(stderr, "get argument error: kind must be ticket or attempt\n")
		return 2
	}
}

func runHeartbeatCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	flags := newFlagSet("heartbeat", stderr)
	var opts commandOptions
	opts.bind(flags)
	var attemptID, lease string
	flags.StringVar(&attemptID, "attempt-id", "", "attempt id")
	flags.StringVar(&lease, "lease", "30m", "lease duration")
	if !parseFlags(flags, args) {
		return 2
	}
	if attemptID == "" && flags.NArg() > 0 {
		attemptID = flags.Arg(0)
	}
	duration, err := time.ParseDuration(lease)
	if err != nil {
		fmt.Fprintf(stderr, "heartbeat argument error: lease must be a duration: %v\n", err)
		return 2
	}
	rt, ok := openCommandRuntime(ctx, flags.Name(), opts, stderr, deps)
	if !ok {
		return 1
	}
	defer rt.Close()
	attempt, err := rt.Heartbeat(ctx, services.HeartbeatRequest{AttemptID: mustUUID(attemptID), Lease: duration})
	if err != nil {
		fmt.Fprintf(stderr, "heartbeat error: %v\n", err)
		return 1
	}
	return writeJSON(stdout, stderr, attemptPayload(attempt))
}

func runCheckpointCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	flags := newFlagSet("checkpoint", stderr)
	var opts commandOptions
	opts.bind(flags)
	var attemptID, summary, nextStep, risk string
	var progress int
	var files, commands stringList
	flags.StringVar(&attemptID, "attempt-id", "", "attempt id")
	flags.StringVar(&summary, "summary", "", "checkpoint summary")
	flags.IntVar(&progress, "progress", 0, "progress percent")
	flags.Var(&files, "file", "file touched")
	flags.Var(&commands, "command", "command run")
	flags.StringVar(&nextStep, "next", "", "next step")
	flags.StringVar(&risk, "risk", "", "risk")
	if !parseFlags(flags, args) {
		return 2
	}
	if attemptID == "" && flags.NArg() > 0 {
		attemptID = flags.Arg(0)
	}
	rt, ok := openCommandRuntime(ctx, flags.Name(), opts, stderr, deps)
	if !ok {
		return 1
	}
	defer rt.Close()
	result, err := rt.Checkpoint(ctx, services.CheckpointRequest{
		AttemptID:       mustUUID(attemptID),
		Summary:         summary,
		ProgressPercent: int32(progress),
		FilesTouched:    files,
		CommandsRun:     commands,
		NextStep:        nextStep,
		Risk:            risk,
	})
	if err != nil {
		fmt.Fprintf(stderr, "checkpoint error: %v\n", err)
		return 1
	}
	return writeJSON(stdout, stderr, checkpointPayload(result))
}

func runCompleteCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	flags := newFlagSet("complete", stderr)
	var opts commandOptions
	opts.bind(flags)
	var metrics attemptMetricsFlags
	metrics.bind(flags)
	var attemptID, summary string
	flags.StringVar(&attemptID, "attempt-id", "", "attempt id")
	flags.StringVar(&summary, "summary", "", "output summary")
	if !parseFlags(flags, args) {
		return 2
	}
	if attemptID == "" && flags.NArg() > 0 {
		attemptID = flags.Arg(0)
	}
	rt, ok := openCommandRuntime(ctx, flags.Name(), opts, stderr, deps)
	if !ok {
		return 1
	}
	defer rt.Close()
	metricsReq, err := metrics.request(flags)
	if err != nil {
		fmt.Fprintf(stderr, "complete argument error: %v\n", err)
		return 2
	}
	result, err := rt.Complete(ctx, services.CompleteAttemptRequest{AttemptID: mustUUID(attemptID), Output: map[string]any{"summary": summary}, Metrics: metricsReq})
	if err != nil {
		fmt.Fprintf(stderr, "complete error: %v\n", err)
		return 1
	}
	return writeJSON(stdout, stderr, transitionPayload(result))
}

func runFailCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	flags := newFlagSet("fail", stderr)
	var opts commandOptions
	opts.bind(flags)
	var metrics attemptMetricsFlags
	metrics.bind(flags)
	var attemptID, reason, category string
	flags.StringVar(&attemptID, "attempt-id", "", "attempt id")
	flags.StringVar(&reason, "reason", "", "failure reason")
	flags.StringVar(&category, "category", "", "failure category")
	if !parseFlags(flags, args) {
		return 2
	}
	if attemptID == "" && flags.NArg() > 0 {
		attemptID = flags.Arg(0)
	}
	metricsReq, err := metrics.request(flags)
	if err != nil {
		fmt.Fprintf(stderr, "fail argument error: %v\n", err)
		return 2
	}
	rt, ok := openCommandRuntime(ctx, flags.Name(), opts, stderr, deps)
	if !ok {
		return 1
	}
	defer rt.Close()
	result, err := rt.Fail(ctx, services.FailAttemptRequest{AttemptID: mustUUID(attemptID), FailureReason: reason, FailureCategory: category, Metrics: metricsReq})
	if err != nil {
		fmt.Fprintf(stderr, "fail error: %v\n", err)
		return 1
	}
	return writeJSON(stdout, stderr, transitionPayload(result))
}

func runBlockCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	flags := newFlagSet("block", stderr)
	var opts commandOptions
	opts.bind(flags)
	var metrics attemptMetricsFlags
	metrics.bind(flags)
	var attemptID, reason, category string
	flags.StringVar(&attemptID, "attempt-id", "", "attempt id")
	flags.StringVar(&reason, "reason", "", "blocker reason")
	flags.StringVar(&category, "category", "", "failure category")
	if !parseFlags(flags, args) {
		return 2
	}
	if attemptID == "" && flags.NArg() > 0 {
		attemptID = flags.Arg(0)
	}
	metricsReq, err := metrics.request(flags)
	if err != nil {
		fmt.Fprintf(stderr, "block argument error: %v\n", err)
		return 2
	}
	rt, ok := openCommandRuntime(ctx, flags.Name(), opts, stderr, deps)
	if !ok {
		return 1
	}
	defer rt.Close()
	result, err := rt.Block(ctx, services.BlockAttemptRequest{
		AttemptID:       mustUUID(attemptID),
		BlockerReason:   reason,
		FailureCategory: category,
		Blocker:         map[string]any{"reason": reason},
		Metrics:         metricsReq,
	})
	if err != nil {
		fmt.Fprintf(stderr, "block error: %v\n", err)
		return 1
	}
	return writeJSON(stdout, stderr, transitionPayload(result))
}

func runCancelCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	flags := newFlagSet("cancel", stderr)
	var opts commandOptions
	opts.bind(flags)
	var attemptID, reason string
	flags.StringVar(&attemptID, "attempt-id", "", "attempt id")
	flags.StringVar(&reason, "reason", "", "cancel reason")
	if !parseFlags(flags, args) {
		return 2
	}
	if attemptID == "" && flags.NArg() > 0 {
		attemptID = flags.Arg(0)
	}
	rt, ok := openCommandRuntime(ctx, flags.Name(), opts, stderr, deps)
	if !ok {
		return 1
	}
	defer rt.Close()
	result, err := rt.Cancel(ctx, services.CancelAttemptRequest{AttemptID: mustUUID(attemptID), Reason: reason})
	if err != nil {
		fmt.Fprintf(stderr, "cancel error: %v\n", err)
		return 1
	}
	return writeJSON(stdout, stderr, transitionPayload(result))
}

func runAttachCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	flags := newFlagSet("attach", stderr)
	var opts commandOptions
	opts.bind(flags)
	var workspaceID, projectID, ticketID, attemptID, artifactType, role, name, url, storageBackend, mimeType string
	var sizeBytes int64
	flags.StringVar(&workspaceID, "workspace-id", "", "workspace id")
	flags.StringVar(&projectID, "project-id", "", "project id")
	flags.StringVar(&ticketID, "ticket-id", "", "ticket id")
	flags.StringVar(&attemptID, "attempt-id", "", "attempt id")
	flags.StringVar(&artifactType, "type", "", "artifact type")
	flags.StringVar(&role, "role", "", "artifact role")
	flags.StringVar(&name, "name", "", "artifact name")
	flags.StringVar(&url, "url", "", "artifact URL")
	flags.StringVar(&storageBackend, "storage-backend", services.ArtifactStorageLocal, "storage backend")
	flags.StringVar(&mimeType, "mime-type", "", "MIME type")
	flags.Int64Var(&sizeBytes, "size-bytes", 0, "size in bytes")
	if !parseFlags(flags, args) {
		return 2
	}
	rt, ok := openCommandRuntime(ctx, flags.Name(), opts, stderr, deps)
	if !ok {
		return 1
	}
	defer rt.Close()
	artifact, err := rt.RegisterArtifact(ctx, services.RegisterArtifactRequest{
		WorkspaceID:    mustUUID(workspaceID),
		ProjectID:      mustUUID(projectID),
		TicketID:       mustUUID(ticketID),
		AttemptID:      mustUUID(attemptID),
		Type:           artifactType,
		Role:           role,
		Name:           name,
		URL:            url,
		StorageBackend: storageBackend,
		SizeBytes:      sizeBytes,
		MimeType:       mimeType,
	})
	if err != nil {
		fmt.Fprintf(stderr, "attach error: %v\n", err)
		return 1
	}
	return writeJSON(stdout, stderr, artifactPayload(artifact))
}

func runAnalyticsCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	if len(args) == 0 || isHelpArg(args[0]) {
		printAnalyticsHelp(stdout)
		return 0
	}
	subcommand := args[0]
	flags := newFlagSet("analytics "+subcommand, stderr)
	var opts commandOptions
	opts.bind(flags)
	var workspaceID, projectID string
	flags.StringVar(&workspaceID, "workspace-id", "", "workspace id")
	flags.StringVar(&projectID, "project-id", "", "project id")
	if !parseFlags(flags, args[1:]) {
		return 2
	}
	filter, err := analyticsFilterFromFlags(workspaceID, projectID)
	if err != nil {
		fmt.Fprintf(stderr, "analytics %s argument error: %v\n", subcommand, err)
		return 2
	}
	rt, ok := openCommandRuntime(ctx, flags.Name(), opts, stderr, deps)
	if !ok {
		return 1
	}
	defer rt.Close()
	switch subcommand {
	case "summary":
		summary, err := rt.AnalyticsSummary(ctx, filter)
		if err != nil {
			fmt.Fprintf(stderr, "analytics summary error: %v\n", err)
			return 1
		}
		if opts.JSON {
			return writeJSON(stdout, stderr, map[string]any{"summary": summary})
		}
		writeAnalyticsSummary(stdout, summary)
		return 0
	case "by-model":
		groups, err := rt.AnalyticsByModel(ctx, filter)
		if err != nil {
			fmt.Fprintf(stderr, "analytics by-model error: %v\n", err)
			return 1
		}
		if opts.JSON {
			return writeJSON(stdout, stderr, map[string]any{"groups": groups})
		}
		writeAnalyticsGroups(stdout, "Model", groups)
		return 0
	case "by-harness":
		groups, err := rt.AnalyticsByHarness(ctx, filter)
		if err != nil {
			fmt.Fprintf(stderr, "analytics by-harness error: %v\n", err)
			return 1
		}
		if opts.JSON {
			return writeJSON(stdout, stderr, map[string]any{"groups": groups})
		}
		writeAnalyticsGroups(stdout, "Harness", groups)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown analytics command %q\n\n", subcommand)
		printAnalyticsHelp(stderr)
		return 2
	}
}

func runCodexCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		printCodexHelp(stdout)
		return 0
	}

	subcommand := args[0]
	if isCodexSubcommandHelpRequest(args[1:]) {
		if printCodexSubcommandHelp(stdout, subcommand) {
			return 0
		}
	}
	switch subcommand {
	case "claim":
		return runCodexClaimCommand(ctx, args[1:], stdout, stderr, deps)
	case "checkpoint":
		return runCodexCheckpointCommand(ctx, args[1:], stdout, stderr, deps)
	case "complete":
		return runCodexCompleteCommand(ctx, args[1:], stdout, stderr, deps)
	case "follow-up":
		return runCodexFollowUpCommand(ctx, args[1:], stdout, stderr, deps)
	case "block":
		return runCodexBlockCommand(ctx, args[1:], stdout, stderr, deps)
	default:
		fmt.Fprintf(stderr, "unknown codex command %q\n\n", subcommand)
		printCodexHelp(stderr)
		return 2
	}
}

func runCodexClaimCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	flags := newFlagSet("codex claim", stderr)
	var opts commandOptions
	opts.bind(flags)
	var workspaceID, projectID, ticketType, agentID, model, lease, idempotencyKey string
	var tags, capabilities stringList
	flags.StringVar(&workspaceID, "workspace-id", "", "workspace id")
	flags.StringVar(&projectID, "project-id", "", "project id")
	flags.StringVar(&ticketType, "type", "", "ticket type filter")
	flags.Var(&tags, "tag", "ticket tag filter")
	flags.Var(&capabilities, "capability", "agent capability")
	flags.StringVar(&agentID, "agent-id", "codex", "agent id")
	flags.StringVar(&model, "model", "", "model")
	flags.StringVar(&lease, "lease", "30m", "attempt lease duration")
	flags.StringVar(&idempotencyKey, "idempotency-key", "", "idempotency key")
	if !parseFlags(flags, args) {
		return 2
	}
	duration, err := time.ParseDuration(lease)
	if err != nil {
		fmt.Fprintf(stderr, "codex claim argument error: lease must be a duration: %v\n", err)
		return 2
	}
	workspaceUUID, err := requiredUUIDFlag("--workspace-id", workspaceID)
	if err != nil {
		fmt.Fprintf(stderr, "codex claim argument error: %v\n", err)
		return 2
	}
	projectUUID, err := requiredUUIDFlag("--project-id", projectID)
	if err != nil {
		fmt.Fprintf(stderr, "codex claim argument error: %v\n", err)
		return 2
	}
	rt, ok := openCommandRuntime(ctx, flags.Name(), opts, stderr, deps)
	if !ok {
		return 1
	}
	defer rt.Close()
	result, err := rt.ClaimNext(ctx, services.ClaimNextRequest{
		WorkspaceID:    workspaceUUID,
		ProjectID:      projectUUID,
		Type:           ticketType,
		Tags:           tags,
		Harness:        "codex",
		Capabilities:   capabilities,
		AgentID:        agentID,
		Model:          model,
		Lease:          duration,
		IdempotencyKey: idempotencyKey,
	})
	if err != nil {
		fmt.Fprintf(stderr, "codex claim error: %v\n", err)
		return 1
	}
	return writeJSON(stdout, stderr, claimPayload(result))
}

func runCodexCheckpointCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	positionalAttemptID, parseArgs := splitLeadingAttemptID(args)
	flags := newFlagSet("codex checkpoint", stderr)
	var opts commandOptions
	opts.bind(flags)
	var attemptID, summary, nextStep, risk string
	var progress int
	var files, commands stringList
	flags.StringVar(&attemptID, "attempt-id", "", "attempt id")
	flags.StringVar(&summary, "summary", "", "checkpoint summary")
	flags.IntVar(&progress, "progress", 0, "progress percent")
	flags.Var(&files, "file", "file touched")
	flags.Var(&commands, "command", "command run")
	flags.StringVar(&nextStep, "next", "", "next step")
	flags.StringVar(&risk, "risk", "", "risk")
	if !parseFlags(flags, parseArgs) {
		return 2
	}
	if attemptID == "" {
		if positionalAttemptID != "" {
			attemptID = positionalAttemptID
		} else if flags.NArg() > 0 {
			attemptID = flags.Arg(0)
		}
	}
	attemptUUID, err := requiredUUIDFlag("--attempt-id", attemptID)
	if err != nil {
		fmt.Fprintf(stderr, "codex checkpoint argument error: %v\n", err)
		return 2
	}
	rt, ok := openCommandRuntime(ctx, flags.Name(), opts, stderr, deps)
	if !ok {
		return 1
	}
	defer rt.Close()
	result, err := rt.Checkpoint(ctx, services.CheckpointRequest{
		AttemptID:       attemptUUID,
		Summary:         summary,
		ProgressPercent: int32(progress),
		FilesTouched:    files,
		CommandsRun:     commands,
		NextStep:        nextStep,
		Risk:            risk,
	})
	if err != nil {
		fmt.Fprintf(stderr, "codex checkpoint error: %v\n", err)
		return 1
	}
	return writeJSON(stdout, stderr, checkpointPayload(result))
}

func runCodexCompleteCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	positionalAttemptID, parseArgs := splitLeadingAttemptID(args)
	flags := newFlagSet("codex complete", stderr)
	var opts commandOptions
	opts.bind(flags)
	var proof codexProofFlags
	proof.bind(flags)
	var metrics attemptMetricsFlags
	metrics.bind(flags)
	var attemptID, summary string
	flags.StringVar(&attemptID, "attempt-id", "", "attempt id")
	flags.StringVar(&summary, "summary", "", "output summary")
	if !parseFlags(flags, parseArgs) {
		return 2
	}
	if attemptID == "" {
		if positionalAttemptID != "" {
			attemptID = positionalAttemptID
		} else if flags.NArg() > 0 {
			attemptID = flags.Arg(0)
		}
	}
	if !proof.validate("codex complete", stderr) {
		return 2
	}
	attemptUUID, err := requiredUUIDFlag("--attempt-id", attemptID)
	if err != nil {
		fmt.Fprintf(stderr, "codex complete argument error: %v\n", err)
		return 2
	}
	metricsReq, err := metrics.request(flags)
	if err != nil {
		fmt.Fprintf(stderr, "codex complete argument error: %v\n", err)
		return 2
	}
	rt, ok := openCommandRuntime(ctx, flags.Name(), opts, stderr, deps)
	if !ok {
		return 1
	}
	defer rt.Close()
	artifactReqs, err := codexProofArtifactRequestsForAttempt(ctx, rt, proof, attemptUUID)
	if err != nil {
		if isCLIArgumentError(err) {
			fmt.Fprintf(stderr, "codex complete argument error: %v\n", err)
			return 2
		}
		fmt.Fprintf(stderr, "codex complete artifact error: %v\n", err)
		return 1
	}
	result, artifacts, err := rt.CompleteWithArtifacts(ctx, services.CompleteAttemptRequest{
		AttemptID: attemptUUID,
		Output: map[string]any{
			"summary": summary,
			"proofs":  []string(proof.Proofs),
		},
		Metrics: metricsReq,
	}, artifactReqs)
	if err != nil {
		fmt.Fprintf(stderr, "codex complete error: %v\n", err)
		return 1
	}
	return writeJSON(stdout, stderr, map[string]any{
		"transition": transitionPayload(result),
		"artifacts":  artifactPayloads(artifacts),
	})
}

func runCodexFollowUpCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	flags := newFlagSet("codex follow-up", stderr)
	var opts commandOptions
	opts.bind(flags)
	var workspaceID, projectID, attemptID, artifactID, kind, title, description, reason string
	var acceptance, verify, paths, tags stringList
	flags.StringVar(&workspaceID, "workspace-id", "", "workspace id")
	flags.StringVar(&projectID, "project-id", "", "project id")
	flags.StringVar(&attemptID, "attempt-id", "", "source attempt id")
	flags.StringVar(&artifactID, "artifact-id", "", "source artifact id")
	flags.StringVar(&kind, "type", services.TemplateFollowUp, "ticket template/type")
	flags.StringVar(&title, "title", "", "ticket title")
	flags.StringVar(&description, "description", "", "ticket description")
	flags.Var(&acceptance, "acceptance", "acceptance criterion")
	flags.Var(&verify, "verify", "verification command")
	flags.Var(&paths, "path", "relevant path")
	flags.Var(&tags, "tag", "ticket tag")
	flags.StringVar(&reason, "reason", "", "creation reason")
	if !parseFlags(flags, args) {
		return 2
	}
	sourceAttemptID, err := requiredUUIDFlag("--attempt-id", attemptID)
	if err != nil {
		fmt.Fprintf(stderr, "codex follow-up argument error: %v\n", err)
		return 2
	}
	sourceArtifactID, err := optionalUUID(artifactID)
	if err != nil {
		fmt.Fprintf(stderr, "codex follow-up argument error: --artifact-id must be a UUID: %v\n", err)
		return 2
	}
	rt, ok := openCommandRuntime(ctx, flags.Name(), opts, stderr, deps)
	if !ok {
		return 1
	}
	defer rt.Close()
	sourceAttempt, err := rt.GetAttempt(ctx, sourceAttemptID)
	if err != nil {
		fmt.Fprintf(stderr, "codex follow-up source attempt error: %v\n", err)
		return 1
	}
	if err := validateOptionalScopeFlags(workspaceID, projectID, sourceAttempt.WorkspaceID, sourceAttempt.ProjectID); err != nil {
		fmt.Fprintf(stderr, "codex follow-up argument error: %v\n", err)
		return 2
	}
	ticket, err := rt.CreateTicketFromAttempt(ctx, services.CreateTicketFromAttemptRequest{
		WorkspaceID:          sourceAttempt.WorkspaceID,
		ProjectID:            sourceAttempt.ProjectID,
		SourceAttemptID:      sourceAttempt.ID,
		SourceArtifactID:     sourceArtifactID,
		TemplateKind:         kind,
		Title:                title,
		Description:          description,
		Tags:                 tags,
		AcceptanceCriteria:   acceptance,
		VerificationCommands: verify,
		RelevantPaths:        paths,
		CreatedByID:          "codex",
		CreationReason:       reason,
	})
	if err != nil {
		fmt.Fprintf(stderr, "codex follow-up error: %v\n", err)
		return 1
	}
	return writeJSON(stdout, stderr, map[string]any{"ticket": ticketPayload(ticket)})
}

func runCodexBlockCommand(ctx context.Context, args []string, stdout, stderr io.Writer, deps Dependencies) int {
	positionalAttemptID, parseArgs := splitLeadingAttemptID(args)
	flags := newFlagSet("codex block", stderr)
	var opts commandOptions
	opts.bind(flags)
	var proof codexProofFlags
	proof.bind(flags)
	var metrics attemptMetricsFlags
	metrics.bind(flags)
	var attemptID, reason, category string
	flags.StringVar(&attemptID, "attempt-id", "", "attempt id")
	flags.StringVar(&reason, "reason", "", "blocker reason")
	flags.StringVar(&category, "category", "", "failure category")
	if !parseFlags(flags, parseArgs) {
		return 2
	}
	if attemptID == "" {
		if positionalAttemptID != "" {
			attemptID = positionalAttemptID
		} else if flags.NArg() > 0 {
			attemptID = flags.Arg(0)
		}
	}
	if !proof.validate("codex block", stderr) {
		return 2
	}
	attemptUUID, err := requiredUUIDFlag("--attempt-id", attemptID)
	if err != nil {
		fmt.Fprintf(stderr, "codex block argument error: %v\n", err)
		return 2
	}
	metricsReq, err := metrics.request(flags)
	if err != nil {
		fmt.Fprintf(stderr, "codex block argument error: %v\n", err)
		return 2
	}
	rt, ok := openCommandRuntime(ctx, flags.Name(), opts, stderr, deps)
	if !ok {
		return 1
	}
	defer rt.Close()
	artifactReqs, err := codexProofArtifactRequestsForAttempt(ctx, rt, proof, attemptUUID)
	if err != nil {
		if isCLIArgumentError(err) {
			fmt.Fprintf(stderr, "codex block argument error: %v\n", err)
			return 2
		}
		fmt.Fprintf(stderr, "codex block artifact error: %v\n", err)
		return 1
	}
	result, artifacts, err := rt.BlockWithArtifacts(ctx, services.BlockAttemptRequest{
		AttemptID:       attemptUUID,
		BlockerReason:   reason,
		FailureCategory: category,
		Blocker: map[string]any{
			"reason": reason,
			"proofs": []string(proof.Proofs),
		},
		Metrics: metricsReq,
	}, artifactReqs)
	if err != nil {
		fmt.Fprintf(stderr, "codex block error: %v\n", err)
		return 1
	}
	return writeJSON(stdout, stderr, map[string]any{
		"transition": transitionPayload(result),
		"artifacts":  artifactPayloads(artifacts),
	})
}

type codexProofFlags struct {
	WorkspaceID string
	ProjectID   string
	Proofs      stringList
	ProofType   string
	ProofRole   string
	MimeType    string
}

func (f *codexProofFlags) bind(flags *flag.FlagSet) {
	flags.StringVar(&f.WorkspaceID, "workspace-id", "", "workspace id for proof artifacts")
	flags.StringVar(&f.ProjectID, "project-id", "", "project id for proof artifacts")
	flags.Var(&f.Proofs, "proof", "proof artifact URL or local reference")
	flags.StringVar(&f.ProofType, "proof-type", services.ArtifactTypeOther, "proof artifact type")
	flags.StringVar(&f.ProofRole, "proof-role", services.ArtifactRoleEvidence, "proof artifact role")
	flags.StringVar(&f.MimeType, "mime-type", "", "proof artifact MIME type")
}

func (f codexProofFlags) validate(commandName string, stderr io.Writer) bool {
	for i, proof := range f.Proofs {
		if strings.TrimSpace(proof) == "" {
			fmt.Fprintf(stderr, "%s argument error: --proof[%d] must not be empty\n", commandName, i)
			return false
		}
	}
	return true
}

func splitLeadingAttemptID(args []string) (string, []string) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", args
	}
	return args[0], args[1:]
}

func codexProofArtifactRequests(flags codexProofFlags, workspaceID, projectID, ticketID, attemptID pgtype.UUID) []services.RegisterArtifactRequest {
	requests := make([]services.RegisterArtifactRequest, 0, len(flags.Proofs))
	for _, proof := range flags.Proofs {
		proof = strings.TrimSpace(proof)
		requests = append(requests, services.RegisterArtifactRequest{
			WorkspaceID:    workspaceID,
			ProjectID:      projectID,
			TicketID:       ticketID,
			AttemptID:      attemptID,
			Type:           flags.ProofType,
			Role:           flags.ProofRole,
			Name:           proofName(proof),
			URL:            proof,
			StorageBackend: services.ArtifactStorageLocal,
			MimeType:       flags.MimeType,
		})
	}
	return requests
}

func codexProofArtifactRequestsForAttempt(ctx context.Context, rt RuntimeHandle, flags codexProofFlags, attemptID pgtype.UUID) ([]services.RegisterArtifactRequest, error) {
	if len(flags.Proofs) == 0 {
		return []services.RegisterArtifactRequest{}, nil
	}
	attempt, err := rt.GetAttempt(ctx, attemptID)
	if err != nil {
		return nil, err
	}
	if err := validateOptionalScopeFlags(flags.WorkspaceID, flags.ProjectID, attempt.WorkspaceID, attempt.ProjectID); err != nil {
		return nil, cliArgumentError{err: err}
	}
	return codexProofArtifactRequests(flags, attempt.WorkspaceID, attempt.ProjectID, attempt.TicketID, attempt.ID), nil
}

func validateOptionalScopeFlags(workspaceID, projectID string, expectedWorkspaceID, expectedProjectID pgtype.UUID) error {
	parsedWorkspaceID, err := optionalUUID(workspaceID)
	if err != nil {
		return fmt.Errorf("--workspace-id must be a UUID: %w", err)
	}
	parsedProjectID, err := optionalUUID(projectID)
	if err != nil {
		return fmt.Errorf("--project-id must be a UUID: %w", err)
	}
	if parsedWorkspaceID.Valid && parsedWorkspaceID != expectedWorkspaceID {
		return fmt.Errorf("--workspace-id does not match source attempt")
	}
	if parsedProjectID.Valid && parsedProjectID != expectedProjectID {
		return fmt.Errorf("--project-id does not match source attempt")
	}
	return nil
}

type processOptions struct {
	config.Options
	TUIWorkspaceID string
	TUIProjectID   string
	TUIStatus      string
	TUIType        string
	TUILimit       int
}

func parseProcessOptions(name string, args []string, stderr io.Writer) (processOptions, bool) {
	var opts processOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--config":
			if i+1 >= len(args) {
				fmt.Fprintln(stderr, "--config requires a path")
				return processOptions{}, false
			}
			i++
			opts.ConfigPath = args[i]
		case strings.HasPrefix(arg, "--config="):
			opts.ConfigPath = strings.TrimPrefix(arg, "--config=")
			if opts.ConfigPath == "" {
				fmt.Fprintln(stderr, "--config requires a path")
				return processOptions{}, false
			}
		case arg == "--workspace-id":
			if name != "tui" {
				fmt.Fprintf(stderr, "unknown flag %q\n", arg)
				return processOptions{}, false
			}
			value, ok := nextProcessFlagValue(args, &i, stderr, "--workspace-id")
			if !ok {
				return processOptions{}, false
			}
			opts.TUIWorkspaceID = value
		case strings.HasPrefix(arg, "--workspace-id="):
			if name != "tui" {
				fmt.Fprintf(stderr, "unknown flag %q\n", arg)
				return processOptions{}, false
			}
			opts.TUIWorkspaceID = strings.TrimPrefix(arg, "--workspace-id=")
		case arg == "--project-id":
			if name != "tui" {
				fmt.Fprintf(stderr, "unknown flag %q\n", arg)
				return processOptions{}, false
			}
			value, ok := nextProcessFlagValue(args, &i, stderr, "--project-id")
			if !ok {
				return processOptions{}, false
			}
			opts.TUIProjectID = value
		case strings.HasPrefix(arg, "--project-id="):
			if name != "tui" {
				fmt.Fprintf(stderr, "unknown flag %q\n", arg)
				return processOptions{}, false
			}
			opts.TUIProjectID = strings.TrimPrefix(arg, "--project-id=")
		case arg == "--status":
			if name != "tui" {
				fmt.Fprintf(stderr, "unknown flag %q\n", arg)
				return processOptions{}, false
			}
			value, ok := nextProcessFlagValue(args, &i, stderr, "--status")
			if !ok {
				return processOptions{}, false
			}
			opts.TUIStatus = value
		case strings.HasPrefix(arg, "--status="):
			if name != "tui" {
				fmt.Fprintf(stderr, "unknown flag %q\n", arg)
				return processOptions{}, false
			}
			opts.TUIStatus = strings.TrimPrefix(arg, "--status=")
		case arg == "--type":
			if name != "tui" {
				fmt.Fprintf(stderr, "unknown flag %q\n", arg)
				return processOptions{}, false
			}
			value, ok := nextProcessFlagValue(args, &i, stderr, "--type")
			if !ok {
				return processOptions{}, false
			}
			opts.TUIType = value
		case strings.HasPrefix(arg, "--type="):
			if name != "tui" {
				fmt.Fprintf(stderr, "unknown flag %q\n", arg)
				return processOptions{}, false
			}
			opts.TUIType = strings.TrimPrefix(arg, "--type=")
		case arg == "--limit":
			if name != "tui" {
				fmt.Fprintf(stderr, "unknown flag %q\n", arg)
				return processOptions{}, false
			}
			value, ok := nextProcessFlagValue(args, &i, stderr, "--limit")
			if !ok {
				return processOptions{}, false
			}
			limit, err := parsePositiveIntFlag("--limit", value)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return processOptions{}, false
			}
			opts.TUILimit = limit
		case strings.HasPrefix(arg, "--limit="):
			if name != "tui" {
				fmt.Fprintf(stderr, "unknown flag %q\n", arg)
				return processOptions{}, false
			}
			value := strings.TrimPrefix(arg, "--limit=")
			limit, err := parsePositiveIntFlag("--limit", value)
			if err != nil {
				fmt.Fprintln(stderr, err)
				return processOptions{}, false
			}
			opts.TUILimit = limit
		default:
			fmt.Fprintf(stderr, "unknown flag %q\n", arg)
			return processOptions{}, false
		}
	}
	return opts, true
}

func nextProcessFlagValue(args []string, index *int, stderr io.Writer, name string) (string, bool) {
	if *index+1 >= len(args) {
		fmt.Fprintf(stderr, "%s requires a value\n", name)
		return "", false
	}
	*index = *index + 1
	return args[*index], true
}

func parsePositiveIntFlag(name, value string) (int, error) {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("%s must be non-negative", name)
	}
	if parsed > math.MaxInt32 {
		return 0, fmt.Errorf("%s must be less than or equal to %d", name, math.MaxInt32)
	}
	return parsed, nil
}

type commandOptions struct {
	ConfigPath string
	JSON       bool
}

func (o *commandOptions) bind(flags *flag.FlagSet) {
	flags.StringVar(&o.ConfigPath, "config", "", "config path")
	flags.BoolVar(&o.JSON, "json", false, "write JSON output")
}

type attemptMetricsFlags struct {
	TokensIn  int64
	TokensOut int64
	CostUSD   float64
	Duration  string
	Retries   int
}

func (f *attemptMetricsFlags) bind(flags *flag.FlagSet) {
	flags.Int64Var(&f.TokensIn, "tokens-in", 0, "input tokens used by the attempt")
	flags.Int64Var(&f.TokensOut, "tokens-out", 0, "output tokens produced by the attempt")
	flags.Float64Var(&f.CostUSD, "cost-usd", 0, "attempt cost in USD")
	flags.StringVar(&f.Duration, "duration", "", "attempt duration, for example 95s or 1m35s")
	flags.IntVar(&f.Retries, "retries", 0, "retry count used by the harness")
}

func (f attemptMetricsFlags) request(flags *flag.FlagSet) (*services.AttemptMetricsRequest, error) {
	seen := visitedFlags(flags)
	if !seen["tokens-in"] && !seen["tokens-out"] && !seen["cost-usd"] && !seen["duration"] && !seen["retries"] {
		return nil, nil
	}
	if f.TokensIn < 0 {
		return nil, fmt.Errorf("--tokens-in must be non-negative")
	}
	if f.TokensOut < 0 {
		return nil, fmt.Errorf("--tokens-out must be non-negative")
	}
	if f.CostUSD < 0 || math.IsNaN(f.CostUSD) || math.IsInf(f.CostUSD, 0) {
		return nil, fmt.Errorf("--cost-usd must be a finite non-negative number")
	}
	if f.Retries < 0 || f.Retries > math.MaxInt32 {
		return nil, fmt.Errorf("--retries must be between 0 and %d", math.MaxInt32)
	}
	durationSeconds := 0.0
	if strings.TrimSpace(f.Duration) != "" {
		duration, err := time.ParseDuration(f.Duration)
		if err != nil {
			return nil, fmt.Errorf("--duration must be a duration like 95s or 1m35s: %w", err)
		}
		if duration < 0 {
			return nil, fmt.Errorf("--duration must be non-negative")
		}
		durationSeconds = duration.Seconds()
	}
	return &services.AttemptMetricsRequest{
		TokensIn:        f.TokensIn,
		TokensOut:       f.TokensOut,
		CostUSD:         f.CostUSD,
		DurationSeconds: durationSeconds,
		RetryCount:      int32(f.Retries),
	}, nil
}

func visitedFlags(flags *flag.FlagSet) map[string]bool {
	seen := make(map[string]bool)
	flags.Visit(func(flag *flag.Flag) {
		seen[flag.Name] = true
	})
	return seen
}

type createTicketFlags struct {
	WorkspaceID string
	ProjectID   string
	Title       string
	Description string
	Type        string
	Priority    int
	Tags        stringList
	Acceptance  stringList
	Verify      stringList
	CreatedBy   string
	CreatedByID string
	Reason      string
	Enqueue     bool
}

func (f *createTicketFlags) bind(flags *flag.FlagSet) {
	flags.StringVar(&f.WorkspaceID, "workspace-id", "", "workspace id")
	flags.StringVar(&f.ProjectID, "project-id", "", "project id")
	flags.StringVar(&f.Title, "title", "", "ticket title")
	flags.StringVar(&f.Description, "description", "", "ticket description")
	flags.StringVar(&f.Type, "type", "", "ticket type")
	flags.IntVar(&f.Priority, "priority", 2, "priority 0-4")
	flags.Var(&f.Tags, "tag", "ticket tag")
	flags.Var(&f.Acceptance, "acceptance", "acceptance criterion")
	flags.Var(&f.Verify, "verify", "verification command")
	flags.StringVar(&f.CreatedBy, "created-by", "", "human, agent, or system")
	flags.StringVar(&f.CreatedByID, "created-by-id", "", "creator id")
	flags.StringVar(&f.Reason, "reason", "", "creation reason")
	flags.BoolVar(&f.Enqueue, "enqueue", false, "create directly in todo when permitted")
}

func (f createTicketFlags) request() (services.CreateTicketRequest, error) {
	priority := int32(f.Priority)
	return services.CreateTicketRequest{
		WorkspaceID:          mustUUID(f.WorkspaceID),
		ProjectID:            mustUUID(f.ProjectID),
		Title:                f.Title,
		Description:          f.Description,
		Type:                 f.Type,
		Priority:             &priority,
		Tags:                 f.Tags,
		AcceptanceCriteria:   f.Acceptance,
		VerificationCommands: f.Verify,
		CreatedBy:            f.CreatedBy,
		CreatedByID:          f.CreatedByID,
		CreationReason:       f.Reason,
		Enqueue:              f.Enqueue,
		CanEnqueue:           f.Enqueue,
	}, nil
}

type stringList []string

func (l *stringList) String() string {
	return strings.Join(*l, ",")
}

func (l *stringList) Set(value string) error {
	*l = append(*l, value)
	return nil
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	return flags
}

func parseFlags(flags *flag.FlagSet, args []string) bool {
	return flags.Parse(args) == nil
}

func openCommandRuntime(ctx context.Context, commandName string, opts commandOptions, stderr io.Writer, deps Dependencies) (RuntimeHandle, bool) {
	cfg, err := config.Load(config.Options{ConfigPath: opts.ConfigPath})
	if err != nil {
		fmt.Fprintf(stderr, "%s configuration error: %v\n", commandName, err)
		return nil, false
	}
	rt, err := deps.OpenRuntime(ctx, cfg)
	if err != nil {
		fmt.Fprintf(stderr, "%s runtime error: %v\n", commandName, err)
		return nil, false
	}
	return rt, true
}

func mustUUID(value string) pgtype.UUID {
	var id pgtype.UUID
	if value == "" {
		return id
	}
	_ = id.Scan(value)
	return id
}

func optionalUUID(value string) (pgtype.UUID, error) {
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

func analyticsFilterFromFlags(workspaceID, projectID string) (services.AnalyticsFilter, error) {
	workspaceUUID, err := optionalUUID(workspaceID)
	if err != nil {
		return services.AnalyticsFilter{}, fmt.Errorf("--workspace-id must be a UUID: %w", err)
	}
	projectUUID, err := optionalUUID(projectID)
	if err != nil {
		return services.AnalyticsFilter{}, fmt.Errorf("--project-id must be a UUID: %w", err)
	}
	return services.AnalyticsFilter{WorkspaceID: workspaceUUID, ProjectID: projectUUID}, nil
}

func requiredUUIDFlag(name, value string) (pgtype.UUID, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return pgtype.UUID{}, fmt.Errorf("%s is required", name)
	}
	id, err := optionalUUID(value)
	if err != nil {
		return pgtype.UUID{}, fmt.Errorf("%s must be a UUID: %w", name, err)
	}
	return id, nil
}

type cliArgumentError struct {
	err error
}

func (e cliArgumentError) Error() string {
	return e.err.Error()
}

func (e cliArgumentError) Unwrap() error {
	return e.err
}

func isCLIArgumentError(err error) bool {
	var target cliArgumentError
	return errors.As(err, &target)
}

func uuidText(id pgtype.UUID) string {
	value, err := id.Value()
	if err != nil {
		return ""
	}
	text, _ := value.(string)
	return text
}

func ticketPayload(ticket db.Ticket) map[string]any {
	return map[string]any{
		"id":           uuidText(ticket.ID),
		"title":        ticket.Title,
		"type":         ticket.Type,
		"status":       ticket.Status,
		"priority":     ticket.Priority,
		"created_by":   ticket.CreatedBy,
		"project_id":   uuidText(ticket.ProjectID),
		"workspace_id": uuidText(ticket.WorkspaceID),
	}
}

func attemptPayload(attempt db.Attempt) map[string]any {
	return map[string]any{
		"id":        uuidText(attempt.ID),
		"ticket_id": uuidText(attempt.TicketID),
		"agent_id":  attempt.AgentID,
		"harness":   attempt.Harness,
		"model":     attempt.Model,
		"status":    attempt.Status,
	}
}

func transitionPayload(result services.AttemptTransitionResult) map[string]any {
	return map[string]any{
		"attempt_id":     uuidText(result.AttemptID),
		"ticket_id":      uuidText(result.TicketID),
		"attempt_status": result.AttemptStatus,
		"ticket_status":  result.TicketStatus,
	}
}

func claimPayload(result services.ClaimNextResult) map[string]any {
	return map[string]any{
		"ticket":     ticketPayload(result.Ticket),
		"attempt":    attemptPayload(result.Attempt),
		"context":    result.Context,
		"ticket_id":  uuidText(result.Ticket.ID),
		"attempt_id": uuidText(result.Attempt.ID),
	}
}

func checkpointPayload(result services.CheckpointResult) map[string]any {
	return map[string]any{
		"checkpoint_id": uuidText(result.Checkpoint.ID),
		"attempt_id":    uuidText(result.Checkpoint.AttemptID),
		"summary":       result.Checkpoint.Summary,
		"progress":      result.ProgressPercent,
	}
}

func artifactPayload(artifact db.Artifact) map[string]any {
	return map[string]any{
		"id":              uuidText(artifact.ID),
		"ticket_id":       uuidText(artifact.TicketID),
		"attempt_id":      uuidText(artifact.AttemptID),
		"type":            artifact.Type,
		"role":            artifact.Role,
		"name":            artifact.Name,
		"url":             artifact.Url,
		"storage_backend": artifact.StorageBackend,
		"size_bytes":      artifact.SizeBytes,
		"mime_type":       artifact.MimeType,
	}
}

func artifactPayloads(artifacts []db.Artifact) []map[string]any {
	payloads := make([]map[string]any, 0, len(artifacts))
	for _, artifact := range artifacts {
		payloads = append(payloads, artifactPayload(artifact))
	}
	return payloads
}

func writeAnalyticsSummary(stdout io.Writer, summary services.AnalyticsSummary) {
	fmt.Fprintf(stdout, "Attempts: %d\n", summary.AttemptCount)
	fmt.Fprintf(stdout, "Succeeded: %d\n", summary.SucceededAttempts)
	fmt.Fprintf(stdout, "Failed: %d\n", summary.FailedAttempts)
	fmt.Fprintf(stdout, "Blocked: %d\n", summary.BlockedAttempts)
	fmt.Fprintf(stdout, "Tokens: %d\n", summary.TotalTokensIn+summary.TotalTokensOut)
	fmt.Fprintf(stdout, "Cost: $%.6f\n", summary.TotalCostUSD)
	fmt.Fprintf(stdout, "Duration: %.3fs\n", summary.TotalDurationSeconds)
	fmt.Fprintf(stdout, "Retries: %d\n", summary.TotalRetries)
	fmt.Fprintf(stdout, "Attempts with metrics: %d\n", summary.AttemptsWithMetrics)
}

func writeAnalyticsGroups(stdout io.Writer, label string, groups []services.AnalyticsGroup) {
	fmt.Fprintf(stdout, "%s\tAttempts\tSucceeded\tCost\tTokens\tRetries\n", label)
	for _, group := range groups {
		fmt.Fprintf(
			stdout,
			"%s\t%d\t%d\t$%.6f\t%d\t%d\n",
			group.Group,
			group.AttemptCount,
			group.SucceededAttempts,
			group.TotalCostUSD,
			group.TotalTokensIn+group.TotalTokensOut,
			group.TotalRetries,
		)
	}
}

func proofName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "proof"
	}
	value = strings.TrimRight(value, "/")
	if index := strings.LastIndex(value, "/"); index >= 0 && index < len(value)-1 {
		return value[index+1:]
	}
	return value
}

func writeJSON(stdout, stderr io.Writer, value any) int {
	data, err := json.Marshal(value)
	if err != nil {
		fmt.Fprintf(stderr, "json error: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(data))
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func isKnownCommand(name string) bool {
	for _, cmd := range commands {
		if cmd.name == name {
			return true
		}
	}
	return false
}

func isRuntimeCommand(name string) bool {
	switch name {
	case "create", "propose", "claim-next", "heartbeat", "checkpoint", "complete", "fail", "block", "cancel", "attach", "list", "get", "analytics":
		return true
	default:
		return false
	}
}

func printHelp(w io.Writer) {
	fmt.Fprintln(w, "Forge")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "A pull-based work ledger for autonomous AI agents.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  forge <command> [flags]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	for _, cmd := range commands {
		fmt.Fprintf(w, "  %-12s %s\n", cmd.name, cmd.description)
	}
}

func printCommandHelp(w io.Writer, name string) {
	if name == "codex" {
		printCodexHelp(w)
		return
	}
	for _, cmd := range commands {
		if cmd.name == name {
			fmt.Fprintf(w, "Usage:\n  forge %s [flags]\n\n%s\n", cmd.name, strings.TrimSuffix(cmd.description, "."))
			return
		}
	}
}

func printCodexHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  forge codex <command> [flags]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	fmt.Fprintln(w, "  claim       Claim the next Codex-ready ticket")
	fmt.Fprintln(w, "  checkpoint  Record resumable progress")
	fmt.Fprintln(w, "  complete    Complete an attempt and attach proof artifacts")
	fmt.Fprintln(w, "  follow-up   Create structured follow-up from an attempt")
	fmt.Fprintln(w, "  block       Mark an attempt blocked with captured context")
}

func printAnalyticsHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  forge analytics <summary|by-model|by-harness> [flags]")
}

func printCodexSubcommandHelp(w io.Writer, name string) bool {
	switch name {
	case "claim", "checkpoint", "complete", "follow-up", "block":
		fmt.Fprintf(w, "Usage:\n  forge codex %s [flags]\n", name)
		return true
	default:
		return false
	}
}

func isHelpArg(value string) bool {
	return value == "help" || value == "--help" || value == "-h"
}

func isCodexSubcommandHelpRequest(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == "help" {
		return true
	}
	for i, arg := range args {
		if arg == "--" {
			return false
		}
		if arg == "--help" || arg == "-h" {
			if i > 0 && codexFlagConsumesValue(args[i-1]) && !strings.Contains(args[i-1], "=") {
				continue
			}
			return true
		}
	}
	return false
}

func codexFlagConsumesValue(arg string) bool {
	if !strings.HasPrefix(arg, "-") || arg == "-" {
		return false
	}
	name := strings.TrimLeft(arg, "-")
	if idx := strings.Index(name, "="); idx >= 0 {
		return false
	}
	switch name {
	case "config",
		"workspace-id",
		"project-id",
		"type",
		"tag",
		"capability",
		"agent-id",
		"model",
		"lease",
		"idempotency-key",
		"attempt-id",
		"summary",
		"progress",
		"file",
		"command",
		"next",
		"risk",
		"artifact-id",
		"title",
		"description",
		"acceptance",
		"verify",
		"path",
		"reason",
		"category",
		"proof",
		"proof-type",
		"proof-role",
		"mime-type":
		return true
	default:
		return false
	}
}
