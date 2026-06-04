package agents

import (
	"context"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

func TestCostGuardianIDNameCapabilities(t *testing.T) {
	a := &CostGuardianAgent{}
	if a.ID() != "cost_guardian" {
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

func TestCostGuardianApproveUnderBudget(t *testing.T) {
	a := &CostGuardianAgent{}
	state := medical.CaseState{
		CaseID:       "c1",
		TotalCostUSD: 200,
		MaxCostUSD:   500,
	}
	msg := agent.NewMessage("test_planner", "cost_guardian", agent.RoleAgent,
		"case_state", medical.EncodeState(state), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 output, got %d", len(out))
	}
	if out[0].Type != "tests_complete" {
		t.Errorf("Type=%s, want tests_complete", out[0].Type)
	}
	if out[0].To != "diagnostic_supervisor" {
		t.Errorf("To=%s, want diagnostic_supervisor", out[0].To)
	}
}

func TestCostGuardianDenyOverBudget(t *testing.T) {
	a := &CostGuardianAgent{}
	state := medical.CaseState{
		CaseID:       "c1",
		TotalCostUSD: 600,
		MaxCostUSD:   500,
	}
	msg := agent.NewMessage("test_planner", "cost_guardian", agent.RoleAgent,
		"case_state", medical.EncodeState(state), map[string]any{})
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 output, got %d", len(out))
	}
	if out[0].Type != "tests_revise" {
		t.Errorf("Type=%s, want tests_revise", out[0].Type)
	}
	if out[0].To != "diagnostic_supervisor" {
		t.Errorf("To=%s, want diagnostic_supervisor", out[0].To)
	}
}

func TestCostGuardianInvalidJSON(t *testing.T) {
	a := &CostGuardianAgent{}
	msg := agent.NewMessage("test_planner", "cost_guardian", agent.RoleAgent,
		"case_state", "not json", map[string]any{})
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
}
