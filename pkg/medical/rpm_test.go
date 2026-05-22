package medical

import "testing"

func TestEvaluateRPMAlerts_CHF(t *testing.T) {
	obs := []RPMObservation{
		{Device: "bp_cuff", Metric: "systolic_bp", Value: 168, Unit: "mmHg"},
		{Device: "scale", Metric: "weight_lb", Value: 192, Unit: "lb"},
		{Device: "pulse_ox", Metric: "spo2", Value: 91, Unit: "%"},
	}
	alerts := EvaluateRPMAlerts(obs)
	if len(alerts) < 3 {
		t.Fatalf("expected at least 3 alerts, got %v", alerts)
	}
}
