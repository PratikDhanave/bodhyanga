package analytics

import (
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/eval"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

func TestSummarizeCases(t *testing.T) {
	records := []eval.InteractionRecord{
		{Metrics: map[string]float64{"accuracy": 1, "cost_usd": 100, "rpm_alert_count": 0},
			Metadata: map[string]any{"under_budget": true}},
		{Metrics: map[string]float64{"accuracy": 0, "cost_usd": 300, "rpm_alert_count": 2},
			Metadata: map[string]any{"under_budget": false}},
	}
	m := SummarizeCases(records)
	if m.Cases != 2 || m.MeanAccuracy != 0.5 || m.MeanCostUSD != 200 || m.UnderBudgetPct != 0.5 {
		t.Errorf("unexpected cohort metrics: %+v", m)
	}
}

func TestGapBreakdown(t *testing.T) {
	r := medical.PanelReport{Gaps: []medical.CareGap{
		{Rule: "a1c_overdue"}, {Rule: "a1c_overdue"}, {Rule: "bp_uncontrolled"},
	}}
	b := GapBreakdown(r)
	if b["a1c_overdue"] != 2 || b["bp_uncontrolled"] != 1 {
		t.Errorf("breakdown: %+v", b)
	}
}
