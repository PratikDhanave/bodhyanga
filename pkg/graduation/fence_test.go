package graduation

import (
	"strings"
	"testing"
)

// TestEvaluate_AutopilotFencedByDefault is the regulatory-fence guarantee: even
// a flawless record cannot promote an agent from copilot into autopilot under
// the default criteria. Autonomy is a policy decision, not a statistical one.
func TestEvaluate_AutopilotFencedByDefault(t *testing.T) {
	d := Evaluate(StageCopilot, 1000, 1000, DefaultCriteria())
	if d.Outcome != OutcomeHold {
		t.Fatalf("autopilot must be fenced by default even with a perfect record, got %s", d.Outcome)
	}
	if d.To != StageCopilot {
		t.Errorf("fenced agent must stay at copilot, got %s", d.To)
	}
	if !strings.Contains(d.Reason, "authorization") {
		t.Errorf("fence reason should cite authorization, got %q", d.Reason)
	}
}

// TestEvaluate_AutopilotPromotesWhenAuthorized confirms the fence opens with an
// explicit AllowAutopilot, and only then if the statistical bar is also met.
func TestEvaluate_AutopilotPromotesWhenAuthorized(t *testing.T) {
	c := DefaultCriteria()
	c.AllowAutopilot = true

	d := Evaluate(StageCopilot, 1000, 1000, c)
	if d.Outcome != OutcomePromote || d.To != StageAutopilot {
		t.Fatalf("authorized + statistically-met copilot should promote to autopilot, got %s→%s (%s)",
			d.From, d.To, d.Reason)
	}

	// Authorization without the statistical evidence still holds.
	weak := Evaluate(StageCopilot, 60, 100, c)
	if weak.Outcome != OutcomeHold {
		t.Errorf("authorization must not bypass the Wilson gate, got %s", weak.Outcome)
	}
}

// TestEvaluate_FenceDoesNotAffectLowerPromotions confirms the fence is specific
// to the autopilot target — shadow→copilot still promotes under the default
// (AllowAutopilot=false) criteria.
func TestEvaluate_FenceDoesNotAffectLowerPromotions(t *testing.T) {
	d := Evaluate(StageShadow, 990, 1000, DefaultCriteria())
	if d.Outcome != OutcomePromote || d.To != StageCopilot {
		t.Fatalf("shadow→copilot must be unaffected by the autopilot fence, got %s→%s", d.From, d.To)
	}
}
