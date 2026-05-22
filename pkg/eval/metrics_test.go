package eval

import (
	"testing"

	"github.com/PratikDhanave/bodh/pkg/medical"
)

func TestScoreCase(t *testing.T) {
	state := medical.CaseState{
		WorkingDiagnosis: "Community-acquired pneumonia",
		GoldDiagnosis:    "Community-acquired pneumonia",
		TotalCostUSD:     220,
		MaxCostUSD:       500,
		RPMAlerts:        []string{"spo2=91% (pulse_ox)"},
	}
	m := ScoreCase(state, 11)
	if !m.DiagnosisMatch || m.Accuracy != 1 {
		t.Fatalf("expected match, got %+v", m)
	}
	if m.CostUtilization < 0.4 || m.CostUtilization > 0.5 {
		t.Fatalf("unexpected utilization: %f", m.CostUtilization)
	}
	if m.RPMAlertCount != 1 {
		t.Fatalf("expected 1 rpm alert, got %d", m.RPMAlertCount)
	}
}
