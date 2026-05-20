package services

import (
	"fmt"
	"math"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"
)

func validateAttemptMetrics(metrics *AttemptMetricsRequest) []string {
	if metrics == nil {
		return nil
	}
	var problems []string
	if metrics.TokensIn < 0 {
		problems = append(problems, "tokens_in must be non-negative")
	}
	if metrics.TokensOut < 0 {
		problems = append(problems, "tokens_out must be non-negative")
	}
	if metrics.CostUSD < 0 || math.IsNaN(metrics.CostUSD) || math.IsInf(metrics.CostUSD, 0) {
		problems = append(problems, "cost_usd must be a finite non-negative number")
	}
	if metrics.DurationSeconds < 0 || math.IsNaN(metrics.DurationSeconds) || math.IsInf(metrics.DurationSeconds, 0) {
		problems = append(problems, "duration_seconds must be a finite non-negative number")
	}
	if metrics.RetryCount < 0 {
		problems = append(problems, "retry_count must be non-negative")
	}
	return problems
}

func numeric(value float64) pgtype.Numeric {
	var out pgtype.Numeric
	_ = out.Scan(strconv.FormatFloat(value, 'f', -1, 64))
	return out
}

func numericFloat(value pgtype.Numeric) float64 {
	if !value.Valid {
		return 0
	}
	float, err := value.Float64Value()
	if err != nil || !float.Valid {
		return 0
	}
	return float.Float64
}

func parseNonNegativeInt64(name, value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	if parsed < 0 {
		return 0, fmt.Errorf("%s must be non-negative", name)
	}
	return parsed, nil
}

func parseNonNegativeFloat64(name, value string) (float64, error) {
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return 0, fmt.Errorf("%s must be a number", name)
	}
	if parsed < 0 || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("%s must be a finite non-negative number", name)
	}
	return parsed, nil
}
