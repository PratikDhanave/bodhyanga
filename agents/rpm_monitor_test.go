package agents

import (
	"context"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

func TestRPMMonitorIDNameCapabilities(t *testing.T) {
	a := &RPMMonitorAgent{}
	if a.ID() != "rpm_monitor" {
		t.Errorf("ID=%q", a.ID())
	}
	if a.Name() == "" {
		t.Error("Name must be non-empty")
	}
	caps := a.Capabilities()
	if len(caps) == 0 {
		t.Error("Capabilities must be non-empty")
	}
}

func TestRPMMonitorWithObservations(t *testing.T) {
	a := &RPMMonitorAgent{}
	state := medical.CaseState{
		CaseID: "c1",
		RPMObservations: []medical.RPMObservation{
			{Metric: "respiratory_rate", Value: 22},
			{Metric: "spo2", Value: 91},
			{Metric: "on_o2", Value: 1},
			{Metric: "copd", Value: 1},
			{Metric: "systolic_bp", Value: 160},
			{Metric: "heart_rate", Value: 110},
			{Metric: "consciousness_score", Value: 1},
			{Metric: "temperature", Value: 38.5},
		},
	}
	msg := agent.NewMessage("intake", "rpm_monitor", agent.RoleAgent,
		"case_state", medical.EncodeState(state), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
	if out[0].To != "diagnostic_supervisor" {
		t.Errorf("To=%s", out[0].To)
	}
	outState, _ := medical.DecodeState(out[0].Content)
	if len(outState.RPMAlerts) == 0 {
		t.Error("expected RPM alerts")
	}
}

func TestRPMMonitorNoObservations(t *testing.T) {
	a := &RPMMonitorAgent{}
	state := medical.CaseState{CaseID: "c1"}
	msg := agent.NewMessage("intake", "rpm_monitor", agent.RoleAgent,
		"case_state", medical.EncodeState(state), map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1, got %d", len(out))
	}
}

func TestRPMMonitorInvalidJSON(t *testing.T) {
	a := &RPMMonitorAgent{}
	msg := agent.NewMessage("intake", "rpm_monitor", agent.RoleAgent,
		"case_state", "bad json", map[string]any{})
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
}

func TestNEWS2InputFromRPM(t *testing.T) {
	obs := []medical.RPMObservation{
		{Metric: "respiratory_rate", Value: 22},
		{Metric: "spo2", Value: 91},
		{Metric: "on_o2", Value: 1},
		{Metric: "copd", Value: 1},
		{Metric: "systolic_bp", Value: 160},
		{Metric: "heart_rate", Value: 110},
		{Metric: "consciousness_score", Value: 0},
		{Metric: "temperature", Value: 38.5},
	}
	input := news2InputFromRPM(obs)
	if input.RespiratoryRate != 22 {
		t.Errorf("resp rate=%d", input.RespiratoryRate)
	}
	if input.SpO2 != 91 {
		t.Errorf("spo2=%v", input.SpO2)
	}
	if !input.OnO2 {
		t.Error("on_o2 should be true")
	}
	if !input.COPD {
		t.Error("copd should be true")
	}
	if input.SystolicBP != 160 {
		t.Errorf("systolic=%d", input.SystolicBP)
	}
	if input.HeartRate != 110 {
		t.Errorf("hr=%d", input.HeartRate)
	}
	if input.Consciousness != "alert" {
		t.Errorf("consciousness=%s", input.Consciousness)
	}
	if input.Temperature != 38.5 {
		t.Errorf("temp=%f", input.Temperature)
	}
}

func TestNEWS2InputFromRPMConfused(t *testing.T) {
	obs := []medical.RPMObservation{
		{Metric: "consciousness_score", Value: 1},
	}
	input := news2InputFromRPM(obs)
	if input.Consciousness != "confused" {
		t.Errorf("consciousness=%s, want confused", input.Consciousness)
	}
}

func TestNEWS2InputFromRPMDefaults(t *testing.T) {
	input := news2InputFromRPM(nil)
	if input.RespiratoryRate != 16 {
		t.Errorf("default resp rate=%d", input.RespiratoryRate)
	}
	if input.SpO2 != 98 {
		t.Errorf("default spo2=%v", input.SpO2)
	}
	if input.SystolicBP != 120 {
		t.Errorf("default systolic=%d", input.SystolicBP)
	}
	if input.HeartRate != 75 {
		t.Errorf("default hr=%d", input.HeartRate)
	}
	if input.Consciousness != "alert" {
		t.Errorf("default consciousness=%s", input.Consciousness)
	}
	if input.Temperature != 37.0 {
		t.Errorf("default temp=%f", input.Temperature)
	}
}
