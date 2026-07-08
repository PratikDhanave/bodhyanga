package eval

import (
	"strings"

	"github.com/PratikDhanave/bodh/pkg/medical"
)

// CaseMetrics captures SD Bench–style outcomes plus RPM-aware population health proxies.
type CaseMetrics struct {
	DiagnosisMatch  bool    `json:"diagnosis_match"`
	Accuracy        float64 `json:"accuracy"` // 1.0 or 0.0 for exact match
	CostUSD         float64 `json:"cost_usd"`
	CostUtilization float64 `json:"cost_utilization"` // total / max budget
	MessageSteps    int     `json:"message_steps"`
	RPMAlertCount   int     `json:"rpm_alert_count"`
	UnderBudget     bool    `json:"under_budget"`
	// Metadata carries arbitrary per-case annotations (e.g. demographics for
	// equity stratification). Keys understood by pkg/eval/equity: "age",
	// "age_group", "sex", "gender", "race", "ethnicity", "correct",
	// "diagnosis_match".
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ScoreCase computes metrics from a completed case state.
func ScoreCase(state medical.CaseState, messageSteps int) CaseMetrics {
	match := diagnosisMatch(state.WorkingDiagnosis, state.GoldDiagnosis)
	util := 0.0
	if state.MaxCostUSD > 0 {
		util = state.TotalCostUSD / state.MaxCostUSD
	}
	return CaseMetrics{
		DiagnosisMatch:  match,
		Accuracy:        boolToFloat(match),
		CostUSD:         state.TotalCostUSD,
		CostUtilization: util,
		MessageSteps:    messageSteps,
		RPMAlertCount:   len(state.RPMAlerts),
		UnderBudget:     state.TotalCostUSD <= state.MaxCostUSD,
	}
}

func diagnosisMatch(working, gold string) bool {
	if gold == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(working), strings.TrimSpace(gold))
}

func boolToFloat(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// ToInteractionRecord builds an eval store record from case metrics.
func (m CaseMetrics) ToInteractionRecord(caseID, scenario string) InteractionRecord {
	return InteractionRecord{
		ID:       caseID,
		Scenario: scenario,
		Success:  m.DiagnosisMatch && m.UnderBudget,
		Metrics: map[string]float64{
			"accuracy":         m.Accuracy,
			"cost_usd":         m.CostUSD,
			"cost_utilization": m.CostUtilization,
			"message_steps":    float64(m.MessageSteps),
			"rpm_alert_count":  float64(m.RPMAlertCount),
		},
		Metadata: map[string]any{
			"case_id":         caseID,
			"diagnosis_match": m.DiagnosisMatch,
			"under_budget":    m.UnderBudget,
		},
	}
}
