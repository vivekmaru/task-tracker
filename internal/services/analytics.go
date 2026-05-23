package services

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

type AnalyticsStore interface {
	GetAnalyticsSummary(context.Context, db.GetAnalyticsSummaryParams) (db.GetAnalyticsSummaryRow, error)
	GetAnalyticsByModel(context.Context, db.GetAnalyticsByModelParams) ([]db.GetAnalyticsByModelRow, error)
	GetAnalyticsByHarness(context.Context, db.GetAnalyticsByHarnessParams) ([]db.GetAnalyticsByHarnessRow, error)
	GetAnalyticsByStatus(context.Context, db.GetAnalyticsByStatusParams) ([]db.GetAnalyticsByStatusRow, error)
	GetAnalyticsByAgent(context.Context, db.GetAnalyticsByAgentParams) ([]db.GetAnalyticsByAgentRow, error)
	GetAnalyticsTrends(context.Context, db.GetAnalyticsTrendsParams) ([]db.GetAnalyticsTrendsRow, error)
}

var _ AnalyticsStore = (*db.Queries)(nil)

type AnalyticsService struct {
	store AnalyticsStore
}

func NewAnalyticsService(store AnalyticsStore) *AnalyticsService {
	return &AnalyticsService{store: store}
}

type AnalyticsFilter struct {
	WorkspaceID pgtype.UUID
	ProjectID   pgtype.UUID
}

type AnalyticsTrendBucket string

const (
	AnalyticsTrendBucketDay  AnalyticsTrendBucket = "day"
	AnalyticsTrendBucketWeek AnalyticsTrendBucket = "week"
)

type AnalyticsTrendFilter struct {
	AnalyticsFilter
	Bucket AnalyticsTrendBucket
}

type AnalyticsSummary struct {
	AttemptCount           int64   `json:"attempt_count"`
	SucceededAttempts      int64   `json:"succeeded_attempts"`
	FailedAttempts         int64   `json:"failed_attempts"`
	BlockedAttempts        int64   `json:"blocked_attempts"`
	TotalTokensIn          int64   `json:"total_tokens_in"`
	TotalTokensOut         int64   `json:"total_tokens_out"`
	TotalTokens            int64   `json:"total_tokens"`
	TotalCostUSD           float64 `json:"total_cost_usd"`
	TotalDurationSeconds   float64 `json:"total_duration_seconds"`
	TotalRetries           int64   `json:"total_retries"`
	AttemptsWithMetrics    int64   `json:"attempts_with_metrics"`
	SuccessRate            float64 `json:"success_rate"`
	AverageCostUSD         float64 `json:"average_cost_usd"`
	AverageDurationSeconds float64 `json:"average_duration_seconds"`
}

type AnalyticsGroup struct {
	Group                  string  `json:"group"`
	AttemptCount           int64   `json:"attempt_count"`
	SucceededAttempts      int64   `json:"succeeded_attempts"`
	FailedAttempts         int64   `json:"failed_attempts"`
	BlockedAttempts        int64   `json:"blocked_attempts"`
	TotalTokensIn          int64   `json:"total_tokens_in"`
	TotalTokensOut         int64   `json:"total_tokens_out"`
	TotalTokens            int64   `json:"total_tokens"`
	TotalCostUSD           float64 `json:"total_cost_usd"`
	TotalDurationSeconds   float64 `json:"total_duration_seconds"`
	TotalRetries           int64   `json:"total_retries"`
	AttemptsWithMetrics    int64   `json:"attempts_with_metrics"`
	SuccessRate            float64 `json:"success_rate"`
	AverageCostUSD         float64 `json:"average_cost_usd"`
	AverageDurationSeconds float64 `json:"average_duration_seconds"`
}

type AnalyticsTrend struct {
	BucketStart            time.Time `json:"bucket_start"`
	Bucket                 string    `json:"bucket"`
	AttemptCount           int64     `json:"attempt_count"`
	SucceededAttempts      int64     `json:"succeeded_attempts"`
	FailedAttempts         int64     `json:"failed_attempts"`
	BlockedAttempts        int64     `json:"blocked_attempts"`
	TotalTokensIn          int64     `json:"total_tokens_in"`
	TotalTokensOut         int64     `json:"total_tokens_out"`
	TotalTokens            int64     `json:"total_tokens"`
	AverageTokens          float64   `json:"average_tokens"`
	TotalCostUSD           float64   `json:"total_cost_usd"`
	TotalDurationSeconds   float64   `json:"total_duration_seconds"`
	TotalRetries           int64     `json:"total_retries"`
	AttemptsWithMetrics    int64     `json:"attempts_with_metrics"`
	SuccessRate            float64   `json:"success_rate"`
	AverageCostUSD         float64   `json:"average_cost_usd"`
	AverageDurationSeconds float64   `json:"average_duration_seconds"`
}

func (s *AnalyticsService) Summary(ctx context.Context, filter AnalyticsFilter) (AnalyticsSummary, error) {
	row, err := s.store.GetAnalyticsSummary(ctx, db.GetAnalyticsSummaryParams{
		WorkspaceID: filter.WorkspaceID,
		ProjectID:   filter.ProjectID,
	})
	if err != nil {
		return AnalyticsSummary{}, fmt.Errorf("get analytics summary: %w", err)
	}
	return analyticsSummaryFromRow(row), nil
}

func (s *AnalyticsService) ByModel(ctx context.Context, filter AnalyticsFilter) ([]AnalyticsGroup, error) {
	rows, err := s.store.GetAnalyticsByModel(ctx, db.GetAnalyticsByModelParams{
		WorkspaceID: filter.WorkspaceID,
		ProjectID:   filter.ProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("get analytics by model: %w", err)
	}
	groups := make([]AnalyticsGroup, 0, len(rows))
	for _, row := range rows {
		groups = append(groups, analyticsGroupFromModelRow(row))
	}
	return groups, nil
}

func (s *AnalyticsService) ByHarness(ctx context.Context, filter AnalyticsFilter) ([]AnalyticsGroup, error) {
	rows, err := s.store.GetAnalyticsByHarness(ctx, db.GetAnalyticsByHarnessParams{
		WorkspaceID: filter.WorkspaceID,
		ProjectID:   filter.ProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("get analytics by harness: %w", err)
	}
	groups := make([]AnalyticsGroup, 0, len(rows))
	for _, row := range rows {
		groups = append(groups, analyticsGroupFromHarnessRow(row))
	}
	return groups, nil
}

func (s *AnalyticsService) ByStatus(ctx context.Context, filter AnalyticsFilter) ([]AnalyticsGroup, error) {
	rows, err := s.store.GetAnalyticsByStatus(ctx, db.GetAnalyticsByStatusParams{
		WorkspaceID: filter.WorkspaceID,
		ProjectID:   filter.ProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("get analytics by status: %w", err)
	}
	groups := make([]AnalyticsGroup, 0, len(rows))
	for _, row := range rows {
		groups = append(groups, analyticsGroupFromStatusRow(row))
	}
	return groups, nil
}

func (s *AnalyticsService) ByAgent(ctx context.Context, filter AnalyticsFilter) ([]AnalyticsGroup, error) {
	rows, err := s.store.GetAnalyticsByAgent(ctx, db.GetAnalyticsByAgentParams{
		WorkspaceID: filter.WorkspaceID,
		ProjectID:   filter.ProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("get analytics by agent: %w", err)
	}
	groups := make([]AnalyticsGroup, 0, len(rows))
	for _, row := range rows {
		groups = append(groups, analyticsGroupFromAgentRow(row))
	}
	return groups, nil
}

func (s *AnalyticsService) Trends(ctx context.Context, filter AnalyticsTrendFilter) ([]AnalyticsTrend, error) {
	bucket, err := normalizeTrendBucket(filter.Bucket)
	if err != nil {
		return nil, err
	}
	rows, err := s.store.GetAnalyticsTrends(ctx, db.GetAnalyticsTrendsParams{
		Bucket:      string(bucket),
		WorkspaceID: filter.WorkspaceID,
		ProjectID:   filter.ProjectID,
	})
	if err != nil {
		return nil, fmt.Errorf("get analytics trends: %w", err)
	}
	trends := make([]AnalyticsTrend, 0, len(rows))
	for _, row := range rows {
		trends = append(trends, analyticsTrendFromRow(row, bucket))
	}
	return trends, nil
}

func normalizeTrendBucket(bucket AnalyticsTrendBucket) (AnalyticsTrendBucket, error) {
	switch bucket {
	case "", AnalyticsTrendBucketDay:
		return AnalyticsTrendBucketDay, nil
	case AnalyticsTrendBucketWeek:
		return AnalyticsTrendBucketWeek, nil
	default:
		return "", fmt.Errorf("unsupported analytics trend bucket %q", bucket)
	}
}

func analyticsSummaryFromRow(row db.GetAnalyticsSummaryRow) AnalyticsSummary {
	totalCost := numericFloat(row.TotalCostUsd)
	totalDuration := numericFloat(row.TotalDurationSecs)
	return AnalyticsSummary{
		AttemptCount:           row.AttemptCount,
		SucceededAttempts:      row.SucceededAttempts,
		FailedAttempts:         row.FailedAttempts,
		BlockedAttempts:        row.BlockedAttempts,
		TotalTokensIn:          row.TotalTokensIn,
		TotalTokensOut:         row.TotalTokensOut,
		TotalTokens:            row.TotalTokensIn + row.TotalTokensOut,
		TotalCostUSD:           totalCost,
		TotalDurationSeconds:   totalDuration,
		TotalRetries:           row.TotalRetries,
		AttemptsWithMetrics:    row.AttemptsWithMetrics,
		SuccessRate:            ratio(row.SucceededAttempts, row.AttemptCount),
		AverageCostUSD:         average(totalCost, row.AttemptsWithMetrics),
		AverageDurationSeconds: average(totalDuration, row.AttemptsWithMetrics),
	}
}

func analyticsGroupFromModelRow(row db.GetAnalyticsByModelRow) AnalyticsGroup {
	group := AnalyticsGroup{
		Group:                row.Model,
		AttemptCount:         row.AttemptCount,
		SucceededAttempts:    row.SucceededAttempts,
		FailedAttempts:       row.FailedAttempts,
		BlockedAttempts:      row.BlockedAttempts,
		TotalTokensIn:        row.TotalTokensIn,
		TotalTokensOut:       row.TotalTokensOut,
		TotalCostUSD:         numericFloat(row.TotalCostUsd),
		TotalDurationSeconds: numericFloat(row.TotalDurationSecs),
		TotalRetries:         row.TotalRetries,
		AttemptsWithMetrics:  row.AttemptsWithMetrics,
	}
	return withComparisonFields(group)
}

func analyticsGroupFromHarnessRow(row db.GetAnalyticsByHarnessRow) AnalyticsGroup {
	group := AnalyticsGroup{
		Group:                row.Harness,
		AttemptCount:         row.AttemptCount,
		SucceededAttempts:    row.SucceededAttempts,
		FailedAttempts:       row.FailedAttempts,
		BlockedAttempts:      row.BlockedAttempts,
		TotalTokensIn:        row.TotalTokensIn,
		TotalTokensOut:       row.TotalTokensOut,
		TotalCostUSD:         numericFloat(row.TotalCostUsd),
		TotalDurationSeconds: numericFloat(row.TotalDurationSecs),
		TotalRetries:         row.TotalRetries,
		AttemptsWithMetrics:  row.AttemptsWithMetrics,
	}
	return withComparisonFields(group)
}

func analyticsGroupFromStatusRow(row db.GetAnalyticsByStatusRow) AnalyticsGroup {
	group := AnalyticsGroup{
		Group:                row.Status,
		AttemptCount:         row.AttemptCount,
		SucceededAttempts:    row.SucceededAttempts,
		FailedAttempts:       row.FailedAttempts,
		BlockedAttempts:      row.BlockedAttempts,
		TotalTokensIn:        row.TotalTokensIn,
		TotalTokensOut:       row.TotalTokensOut,
		TotalCostUSD:         numericFloat(row.TotalCostUsd),
		TotalDurationSeconds: numericFloat(row.TotalDurationSecs),
		TotalRetries:         row.TotalRetries,
		AttemptsWithMetrics:  row.AttemptsWithMetrics,
	}
	return withComparisonFields(group)
}

func analyticsGroupFromAgentRow(row db.GetAnalyticsByAgentRow) AnalyticsGroup {
	group := AnalyticsGroup{
		Group:                row.AgentID,
		AttemptCount:         row.AttemptCount,
		SucceededAttempts:    row.SucceededAttempts,
		FailedAttempts:       row.FailedAttempts,
		BlockedAttempts:      row.BlockedAttempts,
		TotalTokensIn:        row.TotalTokensIn,
		TotalTokensOut:       row.TotalTokensOut,
		TotalCostUSD:         numericFloat(row.TotalCostUsd),
		TotalDurationSeconds: numericFloat(row.TotalDurationSecs),
		TotalRetries:         row.TotalRetries,
		AttemptsWithMetrics:  row.AttemptsWithMetrics,
	}
	return withComparisonFields(group)
}

func analyticsTrendFromRow(row db.GetAnalyticsTrendsRow, bucket AnalyticsTrendBucket) AnalyticsTrend {
	totalCost := numericFloat(row.TotalCostUsd)
	totalDuration := numericFloat(row.TotalDurationSecs)
	totalTokens := row.TotalTokensIn + row.TotalTokensOut
	trend := AnalyticsTrend{
		BucketStart:          row.BucketStart.Time,
		Bucket:               string(bucket),
		AttemptCount:         row.AttemptCount,
		SucceededAttempts:    row.SucceededAttempts,
		FailedAttempts:       row.FailedAttempts,
		BlockedAttempts:      row.BlockedAttempts,
		TotalTokensIn:        row.TotalTokensIn,
		TotalTokensOut:       row.TotalTokensOut,
		TotalTokens:          totalTokens,
		TotalCostUSD:         totalCost,
		TotalDurationSeconds: totalDuration,
		TotalRetries:         row.TotalRetries,
		AttemptsWithMetrics:  row.AttemptsWithMetrics,
	}
	trend.SuccessRate = ratio(trend.SucceededAttempts, trend.AttemptCount)
	trend.AverageCostUSD = average(totalCost, row.AttemptsWithMetrics)
	trend.AverageDurationSeconds = average(totalDuration, row.AttemptsWithMetrics)
	trend.AverageTokens = average(float64(totalTokens), row.AttemptsWithMetrics)
	return trend
}

func withComparisonFields(group AnalyticsGroup) AnalyticsGroup {
	group.TotalTokens = group.TotalTokensIn + group.TotalTokensOut
	group.SuccessRate = ratio(group.SucceededAttempts, group.AttemptCount)
	group.AverageCostUSD = average(group.TotalCostUSD, group.AttemptsWithMetrics)
	group.AverageDurationSeconds = average(group.TotalDurationSeconds, group.AttemptsWithMetrics)
	return group
}

func ratio(numerator, denominator int64) float64 {
	if denominator == 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func average(total float64, count int64) float64 {
	if count == 0 {
		return 0
	}
	return total / float64(count)
}
