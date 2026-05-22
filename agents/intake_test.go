package agents

import (
	"context"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

func TestIntakeBuildsCaseStateAndRoutesToRPM(t *testing.T) {
	pres := medical.PatientPresentation{
		ChiefComplaint: "Cough",
		HPI:            "5 days productive cough",
		Demographics:   "58M",
		Vitals:         "T 38.9",
		Condition:      "cap",
	}
	msg := agent.NewMessage("user", "intake", agent.RoleUser, "presentation",
		medical.EncodePresentation(pres),
		map[string]any{
			"case_id":        "c1",
			"max_cost_usd":   500.0,
			"gold_diagnosis": "Community-acquired pneumonia",
		})

	a := &IntakeAgent{}
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage err: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 output message, got %d", len(out))
	}
	if out[0].To != "rpm_monitor" {
		t.Errorf("want To=rpm_monitor, got %q", out[0].To)
	}
	if out[0].Type != "case_state" {
		t.Errorf("want Type=case_state, got %q", out[0].Type)
	}

	state, err := medical.DecodeState(out[0].Content)
	if err != nil {
		t.Fatalf("DecodeState: %v", err)
	}
	if state.CaseID != "c1" {
		t.Errorf("case_id: want c1, got %q", state.CaseID)
	}
	if state.MaxCostUSD != 500 {
		t.Errorf("max_cost_usd: want 500, got %v", state.MaxCostUSD)
	}
	if state.Condition != "cap" {
		t.Errorf("condition: want cap, got %q", state.Condition)
	}
	if state.GoldDiagnosis != "Community-acquired pneumonia" {
		t.Errorf("gold: %q", state.GoldDiagnosis)
	}
	// Note: Answers / TestResults are JSON `omitempty`, so a fresh state with
	// no answers serialises to "no key" and decodes back as nil. Downstream
	// agents (questioner, test_planner) re-initialize them on use.
}

func TestIntakeReturnsErrorOnInvalidPresentation(t *testing.T) {
	msg := agent.NewMessage("user", "intake", agent.RoleUser, "presentation",
		"not json", map[string]any{"case_id": "c1"})
	a := &IntakeAgent{}
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Errorf("expected error on invalid JSON, got nil")
	}
	if len(out) != 0 {
		t.Errorf("expected no output on error, got %d", len(out))
	}
}
