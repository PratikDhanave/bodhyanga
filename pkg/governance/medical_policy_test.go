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

func TestRequireTenantPolicy(t *testing.T) {
	p := RequireTenantPolicy{}
	deny := protocol.NewMessage("user", "x", protocol.RoleUser, "any", "{}", nil)
	if got, _ := p.Evaluate(context.Background(), deny); got.Decision != DecisionDeny {
		t.Errorf("missing meta should deny, got %s", got.Decision)
	}
	deny2 := protocol.NewMessage("user", "x", protocol.RoleUser, "any", "{}", map[string]any{})
	if got, _ := p.Evaluate(context.Background(), deny2); got.Decision != DecisionDeny {
		t.Errorf("missing tenant_id should deny")
	}
	ok := protocol.NewMessage("user", "x", protocol.RoleUser, "any", "{}", map[string]any{"tenant_id": "t-1"})
	if got, _ := p.Evaluate(context.Background(), ok); got.Decision != DecisionAllow {
		t.Errorf("tenant_id present should allow, got %s", got.Decision)
	}
}

func TestRequireScopePolicy(t *testing.T) {
	p := RequireScopePolicy{Required: map[string]string{"cdm_request": "user/CarePlan.write"}}
	unrelated := protocol.NewMessage("user", "x", protocol.RoleUser, "unrelated_type", "{}", nil)
	if got, _ := p.Evaluate(context.Background(), unrelated); got.Decision != DecisionAllow {
		t.Errorf("type with no required scope should allow")
	}
	deny := protocol.NewMessage("user", "x", protocol.RoleUser, "cdm_request", "{}", map[string]any{"scopes": "user/Patient.read user/Observation.read"})
	if got, _ := p.Evaluate(context.Background(), deny); got.Decision != DecisionDeny {
		t.Errorf("missing required scope should deny, got %s", got.Decision)
	}
	ok := protocol.NewMessage("user", "x", protocol.RoleUser, "cdm_request", "{}", map[string]any{"scopes": "user/Patient.read user/CarePlan.write"})
	if got, _ := p.Evaluate(context.Background(), ok); got.Decision != DecisionAllow {
		t.Errorf("scope present should allow, got %s", got.Decision)
	}
}
