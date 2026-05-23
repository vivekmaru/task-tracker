package services

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

type AnalyticsStore interface {
	GetAnalyticsSummary(context.Context, db.GetAnalyticsSummaryParams) (db.GetAnalyticsSummaryRow, error)
	GetAnalyticsByModel(context.Context, db.GetAnalyticsByModelParams) ([]db.GetAnalyticsByModelRow, error)
	GetAnalyticsByHarness(context.Context, db.GetAnalyticsByHarnessParams) ([]db.GetAnalyticsByHarnessRow, error)
	GetAnalyticsByStatus(context.Context, db.GetAnalyticsByStatusParams) ([]db.GetAnalyticsByStatusRow, error)
	GetAnalyticsByAgent(context.Context, db.GetAnalyticsByAgentParams) ([]db.GetAnalyticsByAgentRow, error)
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

type AnalyticsSummary struct {
	AttemptCount         int64   `json:"attempt_count"`
	SucceededAttempts    int64   `json:"succeeded_attempts"`
	FailedAttempts       int64   `json:"failed_attempts"`
	BlockedAttempts      int64   `json:"blocked_attempts"`
	TotalTokensIn        int64   `json:"total_tokens_in"`
	TotalTokensOut       int64   `json:"total_tokens_out"`
	TotalCostUSD         float64 `json:"total_cost_usd"`
	TotalDurationSeconds float64 `json:"total_duration_seconds"`
	TotalRetries         int64   `json:"total_retries"`
	AttemptsWithMetrics  int64   `json:"attempts_with_metrics"`
}

type AnalyticsGroup struct {
	Group                string  `json:"group"`
	AttemptCount         int64   `json:"attempt_count"`
	SucceededAttempts    int64   `json:"succeeded_attempts"`
	FailedAttempts       int64   `json:"failed_attempts"`
	BlockedAttempts      int64   `json:"blocked_attempts"`
	TotalTokensIn        int64   `json:"total_tokens_in"`
	TotalTokensOut       int64   `json:"total_tokens_out"`
	TotalCostUSD         float64 `json:"total_cost_usd"`
	TotalDurationSeconds float64 `json:"total_duration_seconds"`
	TotalRetries         int64   `json:"total_retries"`
	AttemptsWithMetrics  int64   `json:"attempts_with_metrics"`
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

func analyticsSummaryFromRow(row db.GetAnalyticsSummaryRow) AnalyticsSummary {
	return AnalyticsSummary{
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
}

func analyticsGroupFromModelRow(row db.GetAnalyticsByModelRow) AnalyticsGroup {
	return AnalyticsGroup{
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
}

func analyticsGroupFromHarnessRow(row db.GetAnalyticsByHarnessRow) AnalyticsGroup {
	return AnalyticsGroup{
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
}

func analyticsGroupFromStatusRow(row db.GetAnalyticsByStatusRow) AnalyticsGroup {
	return AnalyticsGroup{
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
}

func analyticsGroupFromAgentRow(row db.GetAnalyticsByAgentRow) AnalyticsGroup {
	return AnalyticsGroup{
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
}
