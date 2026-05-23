package services

import (
	"context"
	"strconv"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/vivek/agent-task-tracker/internal/db"
)

func TestAnalyticsSummaryReturnsAttemptAndMetricTotals(t *testing.T) {
	store := &fakeAnalyticsStore{
		summary: db.GetAnalyticsSummaryRow{
			AttemptCount:        4,
			SucceededAttempts:   2,
			FailedAttempts:      1,
			BlockedAttempts:     1,
			TotalTokensIn:       3000,
			TotalTokensOut:      1400,
			TotalCostUsd:        numericForTest(t, 0.345),
			TotalDurationSecs:   numericForTest(t, 700.5),
			TotalRetries:        3,
			AttemptsWithMetrics: 3,
		},
	}
	service := NewAnalyticsService(store)

	got, err := service.Summary(context.Background(), AnalyticsFilter{
		WorkspaceID: testUUID(1),
		ProjectID:   testUUID(2),
	})
	if err != nil {
		t.Fatalf("analytics summary: %v", err)
	}

	params := store.summaryParams[0]
	if params.WorkspaceID != testUUID(1) || params.ProjectID != testUUID(2) {
		t.Fatalf("expected scoped summary params, got %#v", params)
	}
	if got.AttemptCount != 4 || got.SucceededAttempts != 2 || got.FailedAttempts != 1 || got.BlockedAttempts != 1 {
		t.Fatalf("unexpected attempt counts: %#v", got)
	}
	if got.TotalTokensIn != 3000 || got.TotalTokensOut != 1400 || got.TotalRetries != 3 || got.AttemptsWithMetrics != 3 {
		t.Fatalf("unexpected metric totals: %#v", got)
	}
	if got.TotalCostUSD != 0.345 || got.TotalDurationSeconds != 700.5 {
		t.Fatalf("unexpected numeric totals: %#v", got)
	}
}

func TestAnalyticsByModelAndHarnessReturnGroupedRows(t *testing.T) {
	store := &fakeAnalyticsStore{
		byModel: []db.GetAnalyticsByModelRow{
			{
				Model:               "gpt-5.4",
				AttemptCount:        3,
				SucceededAttempts:   2,
				FailedAttempts:      1,
				TotalTokensIn:       2000,
				TotalTokensOut:      750,
				TotalCostUsd:        numericForTest(t, 0.21),
				TotalDurationSecs:   numericForTest(t, 320),
				TotalRetries:        1,
				AttemptsWithMetrics: 2,
			},
		},
		byHarness: []db.GetAnalyticsByHarnessRow{
			{
				Harness:             "codex",
				AttemptCount:        5,
				SucceededAttempts:   4,
				BlockedAttempts:     1,
				TotalTokensIn:       5400,
				TotalTokensOut:      1900,
				TotalCostUsd:        numericForTest(t, 0.61),
				TotalDurationSecs:   numericForTest(t, 900),
				TotalRetries:        2,
				AttemptsWithMetrics: 4,
			},
		},
		byStatus: []db.GetAnalyticsByStatusRow{
			{
				Status:              "failed",
				AttemptCount:        2,
				FailedAttempts:      2,
				TotalTokensIn:       1200,
				TotalTokensOut:      400,
				TotalCostUsd:        numericForTest(t, 0.19),
				TotalDurationSecs:   numericForTest(t, 180),
				TotalRetries:        3,
				AttemptsWithMetrics: 2,
			},
		},
		byAgent: []db.GetAnalyticsByAgentRow{
			{
				AgentID:             "codex-1",
				AttemptCount:        4,
				SucceededAttempts:   2,
				FailedAttempts:      1,
				BlockedAttempts:     1,
				TotalTokensIn:       3000,
				TotalTokensOut:      900,
				TotalCostUsd:        numericForTest(t, 0.41),
				TotalDurationSecs:   numericForTest(t, 600),
				TotalRetries:        2,
				AttemptsWithMetrics: 3,
			},
		},
	}
	service := NewAnalyticsService(store)

	byModel, err := service.ByModel(context.Background(), AnalyticsFilter{WorkspaceID: testUUID(1)})
	if err != nil {
		t.Fatalf("analytics by model: %v", err)
	}
	byHarness, err := service.ByHarness(context.Background(), AnalyticsFilter{ProjectID: testUUID(2)})
	if err != nil {
		t.Fatalf("analytics by harness: %v", err)
	}
	byStatus, err := service.ByStatus(context.Background(), AnalyticsFilter{WorkspaceID: testUUID(3)})
	if err != nil {
		t.Fatalf("analytics by status: %v", err)
	}
	byAgent, err := service.ByAgent(context.Background(), AnalyticsFilter{ProjectID: testUUID(4)})
	if err != nil {
		t.Fatalf("analytics by agent: %v", err)
	}

	if len(byModel) != 1 || byModel[0].Group != "gpt-5.4" || byModel[0].FailedAttempts != 1 {
		t.Fatalf("unexpected by-model rows: %#v", byModel)
	}
	if byModel[0].SuccessRate != 2.0/3.0 || byModel[0].AverageCostUSD != 0.105 || byModel[0].AverageDurationSeconds != 160 {
		t.Fatalf("expected by-model comparison fields, got %#v", byModel[0])
	}
	if len(byHarness) != 1 || byHarness[0].Group != "codex" || byHarness[0].BlockedAttempts != 1 {
		t.Fatalf("unexpected by-harness rows: %#v", byHarness)
	}
	if byHarness[0].SuccessRate != 0.8 || byHarness[0].AverageCostUSD != 0.1525 || byHarness[0].AverageDurationSeconds != 225 {
		t.Fatalf("expected by-harness comparison fields, got %#v", byHarness[0])
	}
	if len(byStatus) != 1 || byStatus[0].Group != "failed" || byStatus[0].FailedAttempts != 2 {
		t.Fatalf("unexpected by-status rows: %#v", byStatus)
	}
	if len(byAgent) != 1 || byAgent[0].Group != "codex-1" || byAgent[0].TotalDurationSeconds != 600 {
		t.Fatalf("unexpected by-agent rows: %#v", byAgent)
	}
	if store.modelParams[0].WorkspaceID != testUUID(1) {
		t.Fatalf("expected by-model workspace filter, got %#v", store.modelParams[0])
	}
	if store.harnessParams[0].ProjectID != testUUID(2) {
		t.Fatalf("expected by-harness project filter, got %#v", store.harnessParams[0])
	}
	if store.statusParams[0].WorkspaceID != testUUID(3) {
		t.Fatalf("expected by-status workspace filter, got %#v", store.statusParams[0])
	}
	if store.agentParams[0].ProjectID != testUUID(4) {
		t.Fatalf("expected by-agent project filter, got %#v", store.agentParams[0])
	}
}

type fakeAnalyticsStore struct {
	summaryParams []db.GetAnalyticsSummaryParams
	summary       db.GetAnalyticsSummaryRow
	summaryErr    error
	modelParams   []db.GetAnalyticsByModelParams
	byModel       []db.GetAnalyticsByModelRow
	modelErr      error
	harnessParams []db.GetAnalyticsByHarnessParams
	byHarness     []db.GetAnalyticsByHarnessRow
	harnessErr    error
	statusParams  []db.GetAnalyticsByStatusParams
	byStatus      []db.GetAnalyticsByStatusRow
	statusErr     error
	agentParams   []db.GetAnalyticsByAgentParams
	byAgent       []db.GetAnalyticsByAgentRow
	agentErr      error
}

func (s *fakeAnalyticsStore) GetAnalyticsSummary(_ context.Context, params db.GetAnalyticsSummaryParams) (db.GetAnalyticsSummaryRow, error) {
	s.summaryParams = append(s.summaryParams, params)
	return s.summary, s.summaryErr
}

func (s *fakeAnalyticsStore) GetAnalyticsByModel(_ context.Context, params db.GetAnalyticsByModelParams) ([]db.GetAnalyticsByModelRow, error) {
	s.modelParams = append(s.modelParams, params)
	return s.byModel, s.modelErr
}

func (s *fakeAnalyticsStore) GetAnalyticsByHarness(_ context.Context, params db.GetAnalyticsByHarnessParams) ([]db.GetAnalyticsByHarnessRow, error) {
	s.harnessParams = append(s.harnessParams, params)
	return s.byHarness, s.harnessErr
}

func (s *fakeAnalyticsStore) GetAnalyticsByStatus(_ context.Context, params db.GetAnalyticsByStatusParams) ([]db.GetAnalyticsByStatusRow, error) {
	s.statusParams = append(s.statusParams, params)
	return s.byStatus, s.statusErr
}

func (s *fakeAnalyticsStore) GetAnalyticsByAgent(_ context.Context, params db.GetAnalyticsByAgentParams) ([]db.GetAnalyticsByAgentRow, error) {
	s.agentParams = append(s.agentParams, params)
	return s.byAgent, s.agentErr
}

func numericForTest(t *testing.T, value float64) pgtype.Numeric {
	t.Helper()

	var numeric pgtype.Numeric
	if err := numeric.Scan(strconv.FormatFloat(value, 'f', -1, 64)); err != nil {
		t.Fatalf("scan numeric: %v", err)
	}
	return numeric
}
