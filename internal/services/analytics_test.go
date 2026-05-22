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
				TotalTokensIn:       5400,
				TotalTokensOut:      1900,
				TotalCostUsd:        numericForTest(t, 0.61),
				TotalDurationSecs:   numericForTest(t, 900),
				TotalRetries:        2,
				AttemptsWithMetrics: 4,
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

	if len(byModel) != 1 || byModel[0].Group != "gpt-5.4" || byModel[0].TotalCostUSD != 0.21 {
		t.Fatalf("unexpected by-model rows: %#v", byModel)
	}
	if len(byHarness) != 1 || byHarness[0].Group != "codex" || byHarness[0].AttemptCount != 5 {
		t.Fatalf("unexpected by-harness rows: %#v", byHarness)
	}
	if store.modelParams[0].WorkspaceID != testUUID(1) {
		t.Fatalf("expected by-model workspace filter, got %#v", store.modelParams[0])
	}
	if store.harnessParams[0].ProjectID != testUUID(2) {
		t.Fatalf("expected by-harness project filter, got %#v", store.harnessParams[0])
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

func numericForTest(t *testing.T, value float64) pgtype.Numeric {
	t.Helper()

	var numeric pgtype.Numeric
	if err := numeric.Scan(strconv.FormatFloat(value, 'f', -1, 64)); err != nil {
		t.Fatalf("scan numeric: %v", err)
	}
	return numeric
}
