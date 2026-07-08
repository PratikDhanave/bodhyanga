// Package analytics aggregates eval.InteractionRecords and panel reports into
// cohort-level metrics for outcome dashboards.
package analytics

import (
	"github.com/PratikDhanave/bodh/pkg/eval"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// CohortMetrics summarizes a set of recorded cases.
type CohortMetrics struct {
	Cases          int     `json:"cases"`
	MeanAccuracy   float64 `json:"mean_accuracy"`
	MeanCostUSD    float64 `json:"mean_cost_usd"`
	UnderBudgetPct float64 `json:"under_budget_pct"`
	RPMAlertRate   float64 `json:"rpm_alert_rate"`
}

// SummarizeCases produces CohortMetrics from a list of interaction records.
// Records produced by eval.CaseMetrics.ToInteractionRecord are expected, but
// missing fields default to zero.
func SummarizeCases(records []eval.InteractionRecord) CohortMetrics {
	if len(records) == 0 {
		return CohortMetrics{}
	}
	var accSum, costSum, alertSum float64
	under := 0
	for _, r := range records {
		accSum += r.Metrics["accuracy"]
		costSum += r.Metrics["cost_usd"]
		alertSum += r.Metrics["rpm_alert_count"]
		if u, ok := r.Metadata["under_budget"].(bool); ok && u {
			under++
		}
	}
	n := float64(len(records))
	return CohortMetrics{
		Cases:          len(records),
		MeanAccuracy:   accSum / n,
		MeanCostUSD:    costSum / n,
		UnderBudgetPct: float64(under) / n,
		RPMAlertRate:   alertSum / n,
	}
}

// GapBreakdown counts gaps by rule, used in panel dashboards.
func GapBreakdown(report medical.PanelReport) map[string]int {
	out := map[string]int{}
	for _, g := range report.Gaps {
		out[g.Rule]++
	}
	return out
}
