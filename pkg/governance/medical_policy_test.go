package governance

import (
	"context"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/protocol"
)

func TestRequireCaseIDPolicy_DeniesClinicalWithoutCaseID(t *testing.T) {
	p := RequireCaseIDPolicy{}
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "presentation", "{}", nil)
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision != DecisionDeny {
		t.Fatalf("expected deny, got %s", res.Decision)
	}
}

func TestRequireCaseIDPolicy_AllowsWithCaseID(t *testing.T) {
	p := RequireCaseIDPolicy{}
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "presentation", "{}", map[string]any{
		"case_id": "c-1",
	})
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("expected allow, got %s", res.Decision)
	}
}

func TestMaxDiagnosticCostPolicy_DeniesOverBudget(t *testing.T) {
	p := MaxDiagnosticCostPolicy{}
	msg := protocol.NewMessage("test_planner", "cost_guardian", protocol.RoleAgent, "order_test", "{}", map[string]any{
		"max_cost_usd":  100.0,
		"spent_usd":     90.0,
		"test_cost_usd": 50.0,
	})
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision != DecisionDeny {
		t.Fatalf("expected deny, got %s", res.Decision)
	}
}
