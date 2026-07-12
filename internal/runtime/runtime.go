package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vivek/agent-task-tracker/internal/config"
	"github.com/vivek/agent-task-tracker/internal/db"
	"github.com/vivek/agent-task-tracker/internal/jobs"
	"github.com/vivek/agent-task-tracker/internal/services"
	"github.com/vivek/agent-task-tracker/internal/storage"
)

type Runtime struct {
	Pool          *pgxpool.Pool
	beginTx       func(context.Context, pgx.TxOptions) (pgx.Tx, error)
	Queries       *db.Queries
	Tickets       *services.TicketService
	Claims        *services.ClaimService
	Policy        *services.PolicyService
	Attempts      *services.AttemptService
	Artifacts     *services.ArtifactService
	LocalStore    *storage.LocalStore
	S3Store       *storage.S3Store
	ArtifactStore storage.Store
	Events        *services.EventService
	Search        *services.SearchService
	Capabilities  *services.CapabilityService
	Analytics     *services.AnalyticsService
	Maintenance   *jobs.MaintenanceWorker
	Webhooks      *jobs.WebhookWorker
}

// Ready verifies that the runtime can reach its required PostgreSQL dependency.
func (r *Runtime) Ready(ctx context.Context) error {
	if r == nil || r.Pool == nil {
		return fmt.Errorf("runtime database is not configured")
	}
	return r.Pool.Ping(ctx)
}

func Open(ctx context.Context, cfg config.Config) (*Runtime, error) {
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	rt, err := NewWithConfig(ctx, db.New(pool), cfg)
	if err != nil {
		pool.Close()
		return nil, err
	}
	rt.Pool = pool
	return rt, nil
}

func New(queries *db.Queries) *Runtime {
	rt, err := NewWithConfig(context.Background(), queries, config.Config{})
	if err != nil {
		panic(err)
	}
	return rt
}

func NewWithConfig(ctx context.Context, queries *db.Queries, cfg config.Config) (*Runtime, error) {
	if err := cfg.ValidateArtifactStorage(); err != nil {
		return nil, err
	}
	artifactRoot := cfg.ArtifactRoot
	if artifactRoot == "" {
		artifactRoot = ".forge/artifacts"
	}
	localStore := storage.NewLocalStore(artifactRoot)
	artifactStore := storage.Store(localStore)
	var s3Store *storage.S3Store
	if strings.EqualFold(strings.TrimSpace(cfg.ArtifactBackend), storage.BackendS3) {
		client, err := newS3Client(ctx, cfg)
		if err != nil {
			return nil, err
		}
		s3Store, err = storage.NewS3Store(client, storage.S3Options{Bucket: cfg.S3Bucket, Prefix: cfg.S3Prefix})
		if err != nil {
			return nil, fmt.Errorf("configure s3 artifact store: %w", err)
		}
		artifactStore = s3Store
	}
	policy := services.NewPolicyService(services.PolicyConfig{})
	webhookPolicy, err := jobs.NewWebhookDestinationPolicy(cfg.WebhookAllowedHosts, cfg.WebhookAllowedCIDRs)
	if err != nil {
		return nil, fmt.Errorf("configure webhook destination policy: %w", err)
	}
	return &Runtime{
		Queries:       queries,
		Tickets:       services.NewTicketService(queries),
		Claims:        services.NewClaimService(queries, services.WithClaimPolicy(policy)),
		Policy:        policy,
		Attempts:      services.NewAttemptService(queries),
		Artifacts:     services.NewArtifactService(queries),
		LocalStore:    localStore,
		S3Store:       s3Store,
		ArtifactStore: artifactStore,
		Events:        services.NewEventService(queries),
		Search:        services.NewSearchService(queries),
		Capabilities:  services.NewCapabilityService(queries),
		Analytics:     services.NewAnalyticsService(queries),
		Maintenance:   jobs.NewMaintenanceWorker(queries, jobs.WithWebhookRetention(time.Duration(cfg.WebhookRetentionHours)*time.Hour)),
		Webhooks:      jobs.NewWebhookWorker(queries, jobs.WithWebhookDestinationPolicy(webhookPolicy)),
	}, nil
}

func (r *Runtime) Close() {
	if r != nil && r.Pool != nil {
		r.Pool.Close()
	}
}

func (r *Runtime) RunMaintenance(ctx context.Context) (jobs.MaintenanceResult, error) {
	return r.Maintenance.RunOnce(ctx)
}

func (r *Runtime) RunWebhooks(ctx context.Context) (jobs.WebhookRunResult, error) {
	return r.Webhooks.RunOnce(ctx)
}

func (r *Runtime) CreateWebhookSubscription(ctx context.Context, req db.CreateWebhookSubscriptionParams) (db.WebhookSubscription, error) {
	return r.Queries.CreateWebhookSubscription(ctx, req)
}

func (r *Runtime) ListWebhookSubscriptions(ctx context.Context, req db.ListWebhookSubscriptionsParams) ([]db.WebhookSubscription, error) {
	return r.Queries.ListWebhookSubscriptions(ctx, req)
}

func (r *Runtime) CreateTicket(ctx context.Context, req services.CreateTicketRequest) (db.Ticket, error) {
	var ticket db.Ticket
	err := r.withTransaction(ctx, "ticket", func(queries *db.Queries) error {
		var err error
		ticket, err = services.NewTicketService(queries).CreateTicket(ctx, req)
		return err
	})
	return ticket, err
}

func (r *Runtime) ProposeTicket(ctx context.Context, req services.CreateTicketRequest) (db.Ticket, error) {
	var ticket db.Ticket
	err := r.withTransaction(ctx, "ticket", func(queries *db.Queries) error {
		var err error
		ticket, err = services.NewTicketService(queries).ProposeTicket(ctx, req)
		return err
	})
	return ticket, err
}

func (r *Runtime) CreateTicketFromAttempt(ctx context.Context, req services.CreateTicketFromAttemptRequest) (db.Ticket, error) {
	return r.Tickets.CreateTicketFromAttempt(ctx, req)
}

func (r *Runtime) UpdateTicket(ctx context.Context, req services.UpdateTicketRequest) (db.Ticket, error) {
	var ticket db.Ticket
	err := r.withTransaction(ctx, "ticket", func(queries *db.Queries) error {
		var err error
		ticket, err = services.NewTicketService(queries).UpdateTicket(ctx, req)
		return err
	})
	return ticket, err
}

func (r *Runtime) MarkReady(ctx context.Context, req services.TicketTransitionRequest) (db.Ticket, error) {
	return r.Tickets.MarkReady(ctx, req)
}

func (r *Runtime) Reopen(ctx context.Context, req services.TicketTransitionRequest) (db.Ticket, error) {
	return r.Tickets.Reopen(ctx, req)
}

func (r *Runtime) Unblock(ctx context.Context, req services.TicketTransitionRequest) (db.Ticket, error) {
	return r.Tickets.Unblock(ctx, req)
}

func (r *Runtime) RequestReview(ctx context.Context, req services.TicketTransitionRequest) (db.Ticket, error) {
	return r.Tickets.RequestReview(ctx, req)
}

func (r *Runtime) Review(ctx context.Context, req services.ReviewTicketRequest) (db.Ticket, error) {
	return r.Tickets.Review(ctx, req)
}

func (r *Runtime) Archive(ctx context.Context, req services.TicketTransitionRequest) (db.Ticket, error) {
	return r.Tickets.Archive(ctx, req)
}

func (r *Runtime) ClaimNext(ctx context.Context, req services.ClaimNextRequest) (services.ClaimNextResult, error) {
	return r.Claims.ClaimNext(ctx, req)
}

func (r *Runtime) Heartbeat(ctx context.Context, req services.HeartbeatRequest) (db.Attempt, error) {
	return r.Attempts.Heartbeat(ctx, req)
}

func (r *Runtime) Checkpoint(ctx context.Context, req services.CheckpointRequest) (services.CheckpointResult, error) {
	return r.Attempts.Checkpoint(ctx, req)
}

func (r *Runtime) Complete(ctx context.Context, req services.CompleteAttemptRequest) (services.AttemptTransitionResult, error) {
	return r.transitionAttempt(ctx, func(attempts *services.AttemptService) (services.AttemptTransitionResult, error) {
		return attempts.Complete(ctx, req)
	})
}

func (r *Runtime) CompleteWithArtifacts(ctx context.Context, req services.CompleteAttemptRequest, artifactReqs []services.RegisterArtifactRequest) (services.AttemptTransitionResult, []db.Artifact, error) {
	return r.transitionWithArtifacts(ctx, artifactReqs, func(attempts *services.AttemptService) (services.AttemptTransitionResult, error) {
		return attempts.Complete(ctx, req)
	})
}

func (r *Runtime) Fail(ctx context.Context, req services.FailAttemptRequest) (services.AttemptTransitionResult, error) {
	return r.transitionAttempt(ctx, func(attempts *services.AttemptService) (services.AttemptTransitionResult, error) {
		return attempts.Fail(ctx, req)
	})
}

func (r *Runtime) Block(ctx context.Context, req services.BlockAttemptRequest) (services.AttemptTransitionResult, error) {
	return r.transitionAttempt(ctx, func(attempts *services.AttemptService) (services.AttemptTransitionResult, error) {
		return attempts.Block(ctx, req)
	})
}

func (r *Runtime) BlockWithArtifacts(ctx context.Context, req services.BlockAttemptRequest, artifactReqs []services.RegisterArtifactRequest) (services.AttemptTransitionResult, []db.Artifact, error) {
	return r.transitionWithArtifacts(ctx, artifactReqs, func(attempts *services.AttemptService) (services.AttemptTransitionResult, error) {
		return attempts.Block(ctx, req)
	})
}

func (r *Runtime) Cancel(ctx context.Context, req services.CancelAttemptRequest) (services.AttemptTransitionResult, error) {
	return r.Attempts.Cancel(ctx, req)
}

func (r *Runtime) ListTickets(ctx context.Context, req services.ListTicketsRequest) ([]db.Ticket, error) {
	return r.Tickets.ListTickets(ctx, req)
}

func (r *Runtime) ListProposedTickets(ctx context.Context, req services.ListProposedTicketsRequest) ([]services.ProposedTicketTriageItem, error) {
	return r.Tickets.ListProposedTickets(ctx, req)
}

func (r *Runtime) SearchTickets(ctx context.Context, req services.SearchTicketsRequest) ([]services.SearchResult, error) {
	return r.Search.SearchTickets(ctx, req)
}

func (r *Runtime) RecommendTickets(ctx context.Context, req services.RecommendationRequest) ([]services.RecommendationResult, error) {
	return r.Search.RecommendTickets(ctx, req)
}

func (r *Runtime) RelatedWork(ctx context.Context, req services.RelatedWorkRequest) ([]services.RelatedWorkResult, error) {
	return r.Search.RelatedWork(ctx, req)
}

func (r *Runtime) ReadyProposedTicket(ctx context.Context, req services.ProposedTicketTriageRequest) (db.Ticket, error) {
	return r.Tickets.ReadyProposedTicket(ctx, req)
}

func (r *Runtime) EnqueueProposedTicket(ctx context.Context, req services.ProposedTicketTriageRequest) (db.Ticket, error) {
	return r.Tickets.EnqueueProposedTicket(ctx, req)
}

func (r *Runtime) RefineProposedTicket(ctx context.Context, req services.RefineProposedTicketRequest) (db.Ticket, error) {
	return r.Tickets.RefineProposedTicket(ctx, req)
}

func (r *Runtime) RejectProposedTicket(ctx context.Context, req services.ProposedTicketTriageRequest) (db.Ticket, error) {
	return r.Tickets.RejectProposedTicket(ctx, req)
}

func (r *Runtime) MergeProposedTicket(ctx context.Context, req services.MergeProposedTicketRequest) (db.Ticket, error) {
	return r.Tickets.MergeProposedTicket(ctx, req)
}

func (r *Runtime) ArchiveProposedTicket(ctx context.Context, req services.ProposedTicketTriageRequest) (db.Ticket, error) {
	return r.Tickets.ArchiveProposedTicket(ctx, req)
}

func (r *Runtime) GetTicket(ctx context.Context, id pgtype.UUID) (db.Ticket, error) {
	return r.Queries.GetTicket(ctx, id)
}

func (r *Runtime) GetAttempt(ctx context.Context, id pgtype.UUID) (db.Attempt, error) {
	return r.Queries.GetAttempt(ctx, id)
}

func (r *Runtime) ListAttemptsByTicket(ctx context.Context, ticketID pgtype.UUID) ([]db.Attempt, error) {
	return r.Queries.ListAttemptsByTicket(ctx, ticketID)
}

func (r *Runtime) ListAttemptCheckpointsByTicket(ctx context.Context, ticketID pgtype.UUID) ([]db.AttemptCheckpoint, error) {
	return r.Queries.ListAttemptCheckpointsByTicket(ctx, ticketID)
}

func (r *Runtime) ListTicketEventsByTicket(ctx context.Context, ticketID pgtype.UUID) ([]db.TicketEvent, error) {
	return r.Queries.ListTicketEventsByTicket(ctx, ticketID)
}

func (r *Runtime) ListEvents(ctx context.Context, req services.ListEventsRequest) (services.ListEventsResult, error) {
	return r.Events.ListEvents(ctx, req)
}

func (r *Runtime) RegisterArtifact(ctx context.Context, req services.RegisterArtifactRequest) (db.Artifact, error) {
	return r.Artifacts.RegisterArtifact(ctx, req)
}

func (r *Runtime) transitionWithArtifacts(
	ctx context.Context,
	artifactReqs []services.RegisterArtifactRequest,
	transition func(*services.AttemptService) (services.AttemptTransitionResult, error),
) (services.AttemptTransitionResult, []db.Artifact, error) {
	var result services.AttemptTransitionResult
	created := make([]db.Artifact, 0, len(artifactReqs))
	err := r.withTransaction(ctx, "transition", func(queries *db.Queries) error {
		attempts := services.NewAttemptService(queries)
		artifacts := services.NewArtifactService(queries)

		var err error
		result, err = transition(attempts)
		if err != nil {
			return err
		}
		for _, req := range artifactReqs {
			artifact, err := artifacts.RegisterArtifact(ctx, req)
			if err != nil {
				return err
			}
			created = append(created, artifact)
		}
		return nil
	})
	if err != nil {
		return services.AttemptTransitionResult{}, nil, err
	}
	return result, created, nil
}

func (r *Runtime) transitionAttempt(ctx context.Context, transition func(*services.AttemptService) (services.AttemptTransitionResult, error)) (services.AttemptTransitionResult, error) {
	var result services.AttemptTransitionResult
	err := r.withTransaction(ctx, "transition", func(queries *db.Queries) error {
		var err error
		result, err = transition(services.NewAttemptService(queries))
		return err
	})
	if err != nil {
		return services.AttemptTransitionResult{}, err
	}
	return result, nil
}

func (r *Runtime) withTransaction(ctx context.Context, operation string, callback func(*db.Queries) error) error {
	begin := r.beginTx
	if begin == nil {
		if r.Pool == nil {
			return fmt.Errorf("runtime pool is not configured")
		}
		begin = r.Pool.BeginTx
	}
	tx, err := begin(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin %s transaction: %w", operation, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	if err := callback(r.Queries.WithTx(tx)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit %s transaction: %w", operation, err)
	}
	committed = true
	return nil
}

func (r *Runtime) ListArtifactsByTicket(ctx context.Context, ticketID pgtype.UUID) ([]db.Artifact, error) {
	return r.Artifacts.ListArtifactsByTicket(ctx, ticketID)
}

func (r *Runtime) ListArtifacts(ctx context.Context, req services.ListArtifactsRequest) ([]db.Artifact, error) {
	return r.Artifacts.ListArtifacts(ctx, req)
}

func (r *Runtime) ListArtifactsByAttempt(ctx context.Context, attemptID pgtype.UUID) ([]db.Artifact, error) {
	return r.Artifacts.ListArtifactsByAttempt(ctx, attemptID)
}

func (r *Runtime) GetArtifact(ctx context.Context, id pgtype.UUID) (db.Artifact, error) {
	return r.Artifacts.GetArtifact(ctx, id)
}

func (r *Runtime) CreateWorkspace(ctx context.Context, name string) (db.Workspace, error) {
	return r.Queries.CreateWorkspace(ctx, name)
}

func (r *Runtime) GetWorkspace(ctx context.Context, id pgtype.UUID) (db.Workspace, error) {
	return r.Queries.GetWorkspace(ctx, id)
}

func (r *Runtime) ListWorkspaces(ctx context.Context) ([]db.Workspace, error) {
	return r.Queries.ListWorkspaces(ctx)
}

func (r *Runtime) CreateProject(ctx context.Context, workspaceID pgtype.UUID, name string) (db.Project, error) {
	return r.Queries.CreateProject(ctx, db.CreateProjectParams{WorkspaceID: workspaceID, Name: name})
}

func (r *Runtime) ListProjectsByWorkspace(ctx context.Context, workspaceID pgtype.UUID) ([]db.Project, error) {
	return r.Queries.ListProjectsByWorkspace(ctx, workspaceID)
}

func (r *Runtime) OpenArtifact(ctx context.Context, artifact db.Artifact) (storage.ArtifactContent, error) {
	switch artifact.StorageBackend {
	case services.ArtifactStorageLocal:
		return r.LocalStore.Open(ctx, artifact)
	case services.ArtifactStorageS3:
		if r.S3Store == nil {
			return storage.ArtifactContent{}, fmt.Errorf("s3 artifact storage is not configured")
		}
		return r.S3Store.Open(ctx, artifact)
	default:
		return storage.ArtifactContent{}, fmt.Errorf("artifact storage backend %q is not openable", artifact.StorageBackend)
	}
}

func (r *Runtime) ArtifactContentOpenable(artifact db.Artifact) bool {
	switch artifact.StorageBackend {
	case services.ArtifactStorageLocal:
		return storage.IsLocalArtifactURL(artifact.Url)
	case services.ArtifactStorageS3:
		return r.S3Store != nil && r.S3Store.CanOpenURL(artifact.Url)
	default:
		return false
	}
}

func (r *Runtime) StoreArtifact(ctx context.Context, sourcePath string, preferredName string) (storage.StoredArtifact, error) {
	return r.ArtifactStore.StoreFile(ctx, sourcePath, preferredName)
}

func (r *Runtime) StoreLocalArtifact(ctx context.Context, sourcePath string, preferredName string) (storage.StoredArtifact, error) {
	return r.LocalStore.StoreFile(ctx, sourcePath, preferredName)
}

func (r *Runtime) RemoveArtifact(ctx context.Context, rawURL string) error {
	switch storage.BackendForURL(rawURL) {
	case storage.BackendLocal:
		return r.LocalStore.Remove(ctx, rawURL)
	case storage.BackendS3:
		if r.S3Store == nil {
			return fmt.Errorf("s3 artifact storage is not configured")
		}
		return r.S3Store.Remove(ctx, rawURL)
	default:
		return fmt.Errorf("artifact storage backend is not configured for %q", rawURL)
	}
}

func (r *Runtime) RemoveLocalArtifact(ctx context.Context, rawURL string) error {
	return r.LocalStore.Remove(ctx, rawURL)
}

func (r *Runtime) DeleteLocalArtifact(ctx context.Context, id pgtype.UUID) (db.Artifact, error) {
	return r.Artifacts.DeleteLocalArtifact(ctx, id, func(rawURL string) error {
		return r.RemoveLocalArtifact(ctx, rawURL)
	})
}

func (r *Runtime) RegisterCapabilities(ctx context.Context, req services.RegisterCapabilitiesRequest) (db.AgentCapability, error) {
	return r.Capabilities.Register(ctx, req)
}

func (r *Runtime) DecomposeTicket(ctx context.Context, req services.DecomposeTicketRequest) (services.DecomposeTicketResult, error) {
	var result services.DecomposeTicketResult
	err := r.withTransaction(ctx, "ticket decomposition", func(queries *db.Queries) error {
		var err error
		result, err = services.NewTicketService(queries).DecomposeTicket(ctx, req)
		return err
	})
	return result, err
}

func newS3Client(ctx context.Context, cfg config.Config) (storage.S3Client, error) {
	region := strings.TrimSpace(cfg.S3Region)
	if region == "" {
		region = "us-east-1"
	}
	loadOptions := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(region),
	}
	if cfg.S3AccessKeyID != "" || cfg.S3SecretAccessKey != "" || cfg.S3SessionToken != "" {
		loadOptions = append(loadOptions, awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.S3AccessKeyID,
			cfg.S3SecretAccessKey,
			cfg.S3SessionToken,
		)))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, loadOptions...)
	if err != nil {
		return nil, fmt.Errorf("load s3 configuration: %w", err)
	}
	return s3.NewFromConfig(awsCfg, func(opts *s3.Options) {
		if endpoint := strings.TrimSpace(cfg.S3Endpoint); endpoint != "" {
			opts.BaseEndpoint = aws.String(endpoint)
		}
		opts.UsePathStyle = cfg.S3UsePathStyle
	}), nil
}

func (r *Runtime) GetCapabilities(ctx context.Context, req services.GetCapabilitiesRequest) (db.AgentCapability, error) {
	return r.Capabilities.Get(ctx, req)
}

func (r *Runtime) AnalyticsSummary(ctx context.Context, filter services.AnalyticsFilter) (services.AnalyticsSummary, error) {
	return r.Analytics.Summary(ctx, filter)
}

func (r *Runtime) AnalyticsByModel(ctx context.Context, filter services.AnalyticsFilter) ([]services.AnalyticsGroup, error) {
	return r.Analytics.ByModel(ctx, filter)
}

func (r *Runtime) AnalyticsByHarness(ctx context.Context, filter services.AnalyticsFilter) ([]services.AnalyticsGroup, error) {
	return r.Analytics.ByHarness(ctx, filter)
}

func (r *Runtime) AnalyticsByStatus(ctx context.Context, filter services.AnalyticsFilter) ([]services.AnalyticsGroup, error) {
	return r.Analytics.ByStatus(ctx, filter)
}

func (r *Runtime) AnalyticsByAgent(ctx context.Context, filter services.AnalyticsFilter) ([]services.AnalyticsGroup, error) {
	return r.Analytics.ByAgent(ctx, filter)
}

func (r *Runtime) AnalyticsTrends(ctx context.Context, filter services.AnalyticsTrendFilter) ([]services.AnalyticsTrend, error) {
	return r.Analytics.Trends(ctx, filter)
}

func (r *Runtime) ListCapabilities(ctx context.Context, req services.ListCapabilitiesRequest) ([]db.AgentCapability, error) {
	return r.Capabilities.List(ctx, req)
}
