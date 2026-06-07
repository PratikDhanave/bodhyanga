package governance

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	meshlib "github.com/microsoft/agent-governance-toolkit/agent-governance-golang/packages/agentmesh"

	"github.com/PratikDhanave/bodh/pkg/protocol"
)

// ============================================================================
// MaxContentLengthPolicy — previously 0% coverage
// ============================================================================

func TestMaxContentLengthPolicy_AllowsWithinLimit(t *testing.T) {
	p := MaxContentLengthPolicy{Max: 100}
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "presentation", "short content", nil)
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow, got %s (reason: %s)", res.Decision, res.Reason)
	}
	if res.Reason != "within content length limit" {
		t.Errorf("expected reason %q, got %q", "within content length limit", res.Reason)
	}
	if res.CheckedAt.IsZero() {
		t.Error("CheckedAt must be non-zero")
	}
}

func TestMaxContentLengthPolicy_DeniesTooLong(t *testing.T) {
	p := MaxContentLengthPolicy{Max: 10}
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "presentation", strings.Repeat("x", 11), nil)
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny, got %s", res.Decision)
	}
	if res.Reason != "content too long" {
		t.Errorf("expected reason %q, got %q", "content too long", res.Reason)
	}
	if res.CheckedAt.IsZero() {
		t.Error("CheckedAt must be non-zero")
	}
}

func TestMaxContentLengthPolicy_ExactlyAtLimit(t *testing.T) {
	p := MaxContentLengthPolicy{Max: 10}
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "presentation", strings.Repeat("a", 10), nil)
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow at exactly the limit, got %s", res.Decision)
	}
}

func TestMaxContentLengthPolicy_ZeroMax(t *testing.T) {
	p := MaxContentLengthPolicy{Max: 0}
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "presentation", "test", nil)
	_, err := p.Evaluate(context.Background(), msg)
	if err == nil {
		t.Error("expected error for Max=0")
	}
}

func TestMaxContentLengthPolicy_NegativeMax(t *testing.T) {
	p := MaxContentLengthPolicy{Max: -5}
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "presentation", "test", nil)
	_, err := p.Evaluate(context.Background(), msg)
	if err == nil {
		t.Error("expected error for negative Max")
	}
}

func TestMaxContentLengthPolicy_EmptyContent(t *testing.T) {
	p := MaxContentLengthPolicy{Max: 100}
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "presentation", "", nil)
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow for empty content, got %s", res.Decision)
	}
}

func TestMaxContentLengthPolicy_OneByteOverLimit(t *testing.T) {
	p := MaxContentLengthPolicy{Max: 5}
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "presentation", "123456", nil)
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny for 6 bytes with Max=5, got %s", res.Decision)
	}
}

// ============================================================================
// ResearchDisclaimerPolicy — previously 0% coverage
// ============================================================================

func TestResearchDisclaimerPolicy_AlwaysAllows(t *testing.T) {
	p := ResearchDisclaimerPolicy{}
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "presentation", "test content", nil)
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow, got %s", res.Decision)
	}
	if res.Reason != "research demo — not for clinical use" {
		t.Errorf("expected reason %q, got %q", "research demo — not for clinical use", res.Reason)
	}
	if res.CheckedAt.IsZero() {
		t.Error("CheckedAt must be non-zero")
	}
}

func TestResearchDisclaimerPolicy_AllowsAnyMessageType(t *testing.T) {
	p := ResearchDisclaimerPolicy{}
	for _, msgType := range []string{"presentation", "order_test", "diagnosis", "system", ""} {
		msg := protocol.NewMessage("user", "intake", protocol.RoleUser, msgType, "content", nil)
		res, err := p.Evaluate(context.Background(), msg)
		if err != nil {
			t.Fatalf("type %q: unexpected error: %v", msgType, err)
		}
		if res.Decision != DecisionAllow {
			t.Errorf("type %q: expected allow, got %s", msgType, res.Decision)
		}
	}
}

func TestResearchDisclaimerPolicy_IgnoresMetadata(t *testing.T) {
	p := ResearchDisclaimerPolicy{}
	msg := protocol.Message{Content: "anything"}
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow, got %s", res.Decision)
	}
}

// ============================================================================
// MaxDiagnosticCostPolicy — boost from 64.3% to 100%
// ============================================================================

func TestMaxDiagnosticCostPolicy_NonOrderTestType(t *testing.T) {
	p := MaxDiagnosticCostPolicy{}
	for _, msgType := range []string{"presentation", "diagnosis", "system", "ask_question", ""} {
		msg := protocol.NewMessage("tp", "cg", protocol.RoleAgent, msgType, "{}", nil)
		res, err := p.Evaluate(context.Background(), msg)
		if err != nil {
			t.Fatalf("type %q: unexpected error: %v", msgType, err)
		}
		if res.Decision != DecisionAllow {
			t.Errorf("type %q: expected allow for non-order_test, got %s", msgType, res.Decision)
		}
		if res.Reason != "not a test order" {
			t.Errorf("type %q: expected reason %q, got %q", msgType, "not a test order", res.Reason)
		}
	}
}

func TestMaxDiagnosticCostPolicy_OrderTestNilMetadata(t *testing.T) {
	p := MaxDiagnosticCostPolicy{}
	msg := protocol.NewMessage("tp", "cg", protocol.RoleAgent, "order_test", "{}", nil)
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow for nil metadata, got %s", res.Decision)
	}
	if res.Reason != "no cost metadata" {
		t.Errorf("expected reason %q, got %q", "no cost metadata", res.Reason)
	}
}

func TestMaxDiagnosticCostPolicy_NoMaxCostUSD(t *testing.T) {
	p := MaxDiagnosticCostPolicy{}
	msg := protocol.NewMessage("tp", "cg", protocol.RoleAgent, "order_test", "{}", map[string]any{
		"test_cost_usd": 50.0,
	})
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow when no max_cost_usd, got %s", res.Decision)
	}
	if res.Reason != "no max_cost_usd constraint" {
		t.Errorf("expected reason %q, got %q", "no max_cost_usd constraint", res.Reason)
	}
}

func TestMaxDiagnosticCostPolicy_NoTestCostUSD(t *testing.T) {
	p := MaxDiagnosticCostPolicy{}
	msg := protocol.NewMessage("tp", "cg", protocol.RoleAgent, "order_test", "{}", map[string]any{
		"max_cost_usd": 100.0,
	})
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow when no test_cost_usd, got %s", res.Decision)
	}
	if res.Reason != "no test_cost_usd in metadata" {
		t.Errorf("expected reason %q, got %q", "no test_cost_usd in metadata", res.Reason)
	}
}

func TestMaxDiagnosticCostPolicy_NoSpentUSD(t *testing.T) {
	p := MaxDiagnosticCostPolicy{}
	msg := protocol.NewMessage("tp", "cg", protocol.RoleAgent, "order_test", "{}", map[string]any{
		"max_cost_usd":  100.0,
		"test_cost_usd": 50.0,
	})
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow when no spent_usd (treated as 0), got %s", res.Decision)
	}
	if res.Reason != "within diagnostic budget" {
		t.Errorf("expected reason %q, got %q", "within diagnostic budget", res.Reason)
	}
}

func TestMaxDiagnosticCostPolicy_WithinBudgetNew(t *testing.T) {
	p := MaxDiagnosticCostPolicy{}
	msg := protocol.NewMessage("tp", "cg", protocol.RoleAgent, "order_test", "{}", map[string]any{
		"max_cost_usd":  200.0,
		"test_cost_usd": 50.0,
		"spent_usd":     100.0,
	})
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow within budget, got %s", res.Decision)
	}
	if res.Reason != "within diagnostic budget" {
		t.Errorf("expected reason %q, got %q", "within diagnostic budget", res.Reason)
	}
}

func TestMaxDiagnosticCostPolicy_ExactlyAtBudgetNew(t *testing.T) {
	p := MaxDiagnosticCostPolicy{}
	msg := protocol.NewMessage("tp", "cg", protocol.RoleAgent, "order_test", "{}", map[string]any{
		"max_cost_usd":  200.0,
		"test_cost_usd": 100.0,
		"spent_usd":     100.0,
	})
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow at exactly budget, got %s", res.Decision)
	}
}

func TestMaxDiagnosticCostPolicy_OverBudgetNew(t *testing.T) {
	p := MaxDiagnosticCostPolicy{}
	msg := protocol.NewMessage("tp", "cg", protocol.RoleAgent, "order_test", "{}", map[string]any{
		"max_cost_usd":  200.0,
		"test_cost_usd": 101.0,
		"spent_usd":     100.0,
	})
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny over budget, got %s", res.Decision)
	}
	if res.Reason != "test would exceed diagnostic budget" {
		t.Errorf("expected reason %q, got %q", "test would exceed diagnostic budget", res.Reason)
	}
}

func TestMaxDiagnosticCostPolicy_MaxCostNonFloat64(t *testing.T) {
	p := MaxDiagnosticCostPolicy{}
	msg := protocol.NewMessage("tp", "cg", protocol.RoleAgent, "order_test", "{}", map[string]any{
		"max_cost_usd":  100,
		"test_cost_usd": 50.0,
	})
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow when max_cost_usd is not float64, got %s", res.Decision)
	}
	if res.Reason != "no max_cost_usd constraint" {
		t.Errorf("expected reason %q, got %q", "no max_cost_usd constraint", res.Reason)
	}
}

func TestMaxDiagnosticCostPolicy_TestCostNonFloat64(t *testing.T) {
	p := MaxDiagnosticCostPolicy{}
	msg := protocol.NewMessage("tp", "cg", protocol.RoleAgent, "order_test", "{}", map[string]any{
		"max_cost_usd":  100.0,
		"test_cost_usd": "fifty",
	})
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow when test_cost_usd is not float64, got %s", res.Decision)
	}
	if res.Reason != "no test_cost_usd in metadata" {
		t.Errorf("expected reason %q, got %q", "no test_cost_usd in metadata", res.Reason)
	}
}

// ============================================================================
// BreakGlassPolicy — boost from 94.4% to 100%: recorder error path
// ============================================================================

func TestBreakGlassPolicy_RecorderError(t *testing.T) {
	rec := &stubBreakGlassRecorder{
		errOnRecord: errors.New("audit storage unavailable"),
	}
	p := BreakGlassPolicy{Recorder: rec}
	msg := msgWith(map[string]any{
		"break_glass":           "true",
		"break_glass_reason":    "ED trauma",
		"break_glass_clinician": "dr-jones",
	})
	_, err := p.Evaluate(context.Background(), msg)
	if err == nil {
		t.Error("expected error when recorder fails")
	}
	if !strings.Contains(err.Error(), "audit storage unavailable") {
		t.Errorf("expected error to contain recorder message, got %q", err.Error())
	}
}

func TestBreakGlassPolicy_BreakGlassWithMissingCorrelationHandles(t *testing.T) {
	pol, rec := newBGPolicy()
	msg := msgWith(map[string]any{
		"break_glass":           "true",
		"break_glass_reason":    "emergency",
		"break_glass_clinician": "dr-smith",
	})
	res, err := pol.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow, got %s", res.Decision)
	}
	events := rec.list()
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	e := events[0]
	if e.PatientID != "" {
		t.Errorf("expected empty PatientID, got %q", e.PatientID)
	}
	if e.CaseID != "" {
		t.Errorf("expected empty CaseID, got %q", e.CaseID)
	}
	if e.TraceID != "" {
		t.Errorf("expected empty TraceID, got %q", e.TraceID)
	}
	if e.TenantID != "" {
		t.Errorf("expected empty TenantID, got %q", e.TenantID)
	}
}

// ============================================================================
// CompositePolicy — edge cases
// ============================================================================

func TestCompositePolicy_EmptyPolicyList(t *testing.T) {
	comp := NewComposite()
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "presentation", "test", nil)
	res, err := comp.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow for empty policy list, got %s", res.Decision)
	}
	if res.Reason != "all policies allow" {
		t.Errorf("expected reason %q, got %q", "all policies allow", res.Reason)
	}
	if res.CheckedAt.IsZero() {
		t.Error("expected non-zero CheckedAt")
	}
}

func TestCompositePolicy_AllReject(t *testing.T) {
	d1 := &staticPolicy{decision: DecisionDeny, reason: "policy-1 denies"}
	d2 := &staticPolicy{decision: DecisionDeny, reason: "policy-2 denies"}
	d3 := &staticPolicy{decision: DecisionDeny, reason: "policy-3 denies"}
	comp := NewComposite(d1, d2, d3)
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "presentation", "test", nil)
	res, err := comp.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny, got %s", res.Decision)
	}
	if res.Reason != "policy-1 denies" {
		t.Errorf("expected reason from first policy, got %q", res.Reason)
	}
	if d2.called {
		t.Error("second policy should not be called after first deny")
	}
	if d3.called {
		t.Error("third policy should not be called after first deny")
	}
}

func TestCompositePolicy_SinglePolicyAllPass(t *testing.T) {
	a := &staticPolicy{decision: DecisionAllow, reason: "single pass"}
	comp := NewComposite(a)
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "presentation", "test", nil)
	res, err := comp.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow, got %s", res.Decision)
	}
	if res.Reason != "all policies allow" {
		t.Errorf("expected reason %q, got %q", "all policies allow", res.Reason)
	}
}

func TestCompositePolicy_SinglePolicyFail(t *testing.T) {
	d := &staticPolicy{decision: DecisionDeny, reason: "single fail"}
	comp := NewComposite(d)
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "presentation", "test", nil)
	res, err := comp.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny, got %s", res.Decision)
	}
	if res.Reason != "single fail" {
		t.Errorf("expected reason %q, got %q", "single fail", res.Reason)
	}
}

func TestCompositePolicy_ConflictingResults(t *testing.T) {
	a1 := &staticPolicy{decision: DecisionAllow, reason: "allow-1"}
	d1 := &staticPolicy{decision: DecisionDeny, reason: "deny-1"}
	a2 := &staticPolicy{decision: DecisionAllow, reason: "allow-2"}
	comp := NewComposite(a1, d1, a2)
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "presentation", "test", nil)
	res, err := comp.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny when one policy denies, got %s", res.Decision)
	}
	if res.Reason != "deny-1" {
		t.Errorf("expected deny reason from second policy, got %q", res.Reason)
	}
	if a2.called {
		t.Error("third policy should not be called after second deny")
	}
}

func TestCompositePolicy_ErrorFromMiddle(t *testing.T) {
	a := &staticPolicy{decision: DecisionAllow, reason: "ok"}
	e := &errorPolicy{err: errors.New("boom")}
	d := &staticPolicy{decision: DecisionDeny, reason: "never reached"}
	comp := NewComposite(a, e, d)
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "presentation", "test", nil)
	_, err := comp.Evaluate(context.Background(), msg)
	if err == nil {
		t.Error("expected error from middle policy")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected error to contain 'boom', got %q", err.Error())
	}
}

func TestCompositePolicy_ConcurrentEvaluation(t *testing.T) {
	p1 := MaxContentLengthPolicy{Max: 1000}
	p2 := RequireCaseIDPolicy{}
	comp := NewComposite(p1, p2)

	var wg sync.WaitGroup
	errs := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "presentation", "test",
				map[string]any{"case_id": "c1"})
			res, err := comp.Evaluate(context.Background(), msg)
			if err != nil {
				errs <- err
				return
			}
			if res.Decision != DecisionAllow {
				errs <- errors.New("expected allow in concurrent evaluation")
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}
}

// ============================================================================
// RateLimitPolicy — boost from 95% to 100%: agent limit exceeded path
// ============================================================================

func TestRateLimitPolicy_AgentLimitExceeded(t *testing.T) {
	p := NewRateLimitPolicy(RateLimitConfig{
		PerTenantRPS: 1000,
		PerAgentRPS:  1,
		BurstFactor:  1,
	})
	msg := msgForTenantAgent("tenant-x", "agent-limited")

	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("first request should be allowed, got %s", res.Decision)
	}

	res, err = p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Fatalf("second request should be denied (agent limit), got %s", res.Decision)
	}
	if !strings.Contains(res.Reason, "agent") {
		t.Errorf("deny reason should mention agent, got %q", res.Reason)
	}
}

func TestRateLimitPolicy_MissingTenantID(t *testing.T) {
	p := NewRateLimitPolicy(RateLimitConfig{
		PerTenantRPS: 1,
		PerAgentRPS:  1000,
		BurstFactor:  1,
	})
	msg := protocol.NewMessage("agent-z", "target", protocol.RoleAgent, "request", "payload", nil)

	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("first request should be allowed, got %s", res.Decision)
	}

	res, err = p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Fatalf("second request should be denied, got %s", res.Decision)
	}
}

func TestRateLimitPolicy_NonStringTenantID(t *testing.T) {
	p := NewRateLimitPolicy(RateLimitConfig{
		PerTenantRPS: 1,
		PerAgentRPS:  1000,
		BurstFactor:  1,
	})
	msg := protocol.NewMessage("agent-z", "target", protocol.RoleAgent, "request", "payload",
		map[string]any{"tenant_id": 12345})

	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("first request should be allowed, got %s", res.Decision)
	}
}

func TestRateLimitPolicy_AgentIsolation(t *testing.T) {
	p := NewRateLimitPolicy(RateLimitConfig{
		PerTenantRPS: 1000,
		PerAgentRPS:  1,
		BurstFactor:  1,
	})

	agentA := msgForTenantAgent("tenant-iso", "agent-a")
	agentB := msgForTenantAgent("tenant-iso", "agent-b")

	p.Evaluate(context.Background(), agentA)
	res, _ := p.Evaluate(context.Background(), agentA)
	if res.Decision != DecisionDeny {
		t.Fatalf("agent-a second request should be denied, got %s", res.Decision)
	}

	res, _ = p.Evaluate(context.Background(), agentB)
	if res.Decision != DecisionAllow {
		t.Fatalf("agent-b should be allowed (isolated from agent-a), got %s", res.Decision)
	}
}

func TestRateLimitPolicy_DefaultConfig(t *testing.T) {
	p := NewRateLimitPolicy(RateLimitConfig{
		PerTenantRPS: -1,
		PerAgentRPS:  -1,
		BurstFactor:  -1,
	})
	if p.cfg.PerTenantRPS != 100 {
		t.Errorf("expected default PerTenantRPS=100, got %f", p.cfg.PerTenantRPS)
	}
	if p.cfg.PerAgentRPS != 10 {
		t.Errorf("expected default PerAgentRPS=10, got %f", p.cfg.PerAgentRPS)
	}
	if p.cfg.BurstFactor != 3 {
		t.Errorf("expected default BurstFactor=3, got %d", p.cfg.BurstFactor)
	}
}

// ============================================================================
// RedactNarrativePolicy — edge cases
// ============================================================================

func TestRedactNarrativePolicy_NoDestination(t *testing.T) {
	p := RedactNarrativePolicy{}
	msg := protocol.NewMessage("a", "b", protocol.RoleAgent, "presentation",
		`{"hpi":"some narrative"}`, map[string]any{})
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow for no destination, got %s", res.Decision)
	}
}

func TestRedactNarrativePolicy_EmptyJSONObject(t *testing.T) {
	p := RedactNarrativePolicy{}
	msg := protocol.NewMessage("a", "b", protocol.RoleAgent, "presentation",
		`{}`, map[string]any{"destination": "audit"})
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow, got %s", res.Decision)
	}
	if res.Reason != "no narrative fields: content unchanged" {
		t.Errorf("expected reason %q, got %q", "no narrative fields: content unchanged", res.Reason)
	}
}

// ============================================================================
// OPAPolicy — boost: empty reason from OPA result, successful MustOPAPolicy
// ============================================================================

func TestOPAPolicy_EmptyReasonFallback(t *testing.T) {
	module := `package bodh.test
import rego.v1
result := {"allowed": true, "reason": ""}`
	p, err := NewOPAPolicy(module, "data.bodh.test.result")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "test", "content", nil)
	res, evalErr := p.Evaluate(context.Background(), msg)
	if evalErr != nil {
		t.Fatalf("unexpected error: %v", evalErr)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow, got %s", res.Decision)
	}
	if res.Reason != "opa policy" {
		t.Errorf("expected fallback reason %q, got %q", "opa policy", res.Reason)
	}
}

func TestOPAPolicy_DenyEmptyReasonFallback(t *testing.T) {
	module := `package bodh.test
import rego.v1
result := {"allowed": false, "reason": ""}`
	p, err := NewOPAPolicy(module, "data.bodh.test.result")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "test", "content", nil)
	res, evalErr := p.Evaluate(context.Background(), msg)
	if evalErr != nil {
		t.Fatalf("unexpected error: %v", evalErr)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny, got %s", res.Decision)
	}
	if res.Reason != "opa policy" {
		t.Errorf("expected fallback reason %q, got %q", "opa policy", res.Reason)
	}
}

func TestOPAPolicy_MissingReasonKey(t *testing.T) {
	module := `package bodh.test
import rego.v1
result := {"allowed": true}`
	p, err := NewOPAPolicy(module, "data.bodh.test.result")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "test", "content", nil)
	res, evalErr := p.Evaluate(context.Background(), msg)
	if evalErr != nil {
		t.Fatalf("unexpected error: %v", evalErr)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow, got %s", res.Decision)
	}
	if res.Reason != "opa policy" {
		t.Errorf("expected fallback reason %q, got %q", "opa policy", res.Reason)
	}
}

func TestMustOPAPolicy_SuccessNew(t *testing.T) {
	module := `package bodh.test
import rego.v1
result := {"allowed": true, "reason": "ok"}`
	p := MustOPAPolicy(module, "data.bodh.test.result")
	if p == nil {
		t.Error("expected non-nil policy")
	}
}

// ============================================================================
// AgentMeshGate — admin ring name and additional edge cases
// ============================================================================

func TestRingName_Admin(t *testing.T) {
	gate := NewAgentMeshGate()
	gate.rings.Assign("admin_agent", meshlib.RingAdmin)
	name := ringName(gate.rings, "admin_agent")
	if name != "admin" {
		t.Errorf("expected %q, got %q", "admin", name)
	}
}

func TestRingName_Unknown(t *testing.T) {
	gate := NewAgentMeshGate()
	// Assign an agent to a ring value that is not one of the four known rings.
	gate.rings.Assign("future_agent", meshlib.Ring(99))
	name := ringName(gate.rings, "future_agent")
	if name != "unknown" {
		t.Errorf("expected %q for unknown ring, got %q", "unknown", name)
	}
}

func TestAgentMeshGate_UnknownRingAgent(t *testing.T) {
	gate := NewAgentMeshGate()
	msg := protocol.NewMessage("user", "unknown_agent", protocol.RoleUser, "presentation", "test", nil)
	res, err := gate.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow for unassigned agent, got %s (reason: %s)", res.Decision, res.Reason)
	}
}

func TestAgentMeshGate_EmptyTo(t *testing.T) {
	gate := NewAgentMeshGate()
	msg := protocol.NewMessage("user", "", protocol.RoleUser, "broadcast", "test", nil)
	res, err := gate.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow for empty To, got %s (reason: %s)", res.Decision, res.Reason)
	}
}

func TestAgentMeshGate_UserRecipientBypassesRingCheck(t *testing.T) {
	gate := NewAgentMeshGate()
	msg := protocol.NewMessage("diagnostician", "user", protocol.RoleAgent, "presentation", "test", nil)
	res, err := gate.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow when To=user, got %s (reason: %s)", res.Decision, res.Reason)
	}
}

func TestAgentMeshGate_EmptyType(t *testing.T) {
	gate := NewAgentMeshGate()
	msg := protocol.NewMessage("user", "audio_transcriber", protocol.RoleUser, "", "test", nil)
	res, err := gate.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow for empty Type, got %s (reason: %s)", res.Decision, res.Reason)
	}
}

func TestAgentMeshGate_RestrictedAgentAllowedAction(t *testing.T) {
	gate := NewAgentMeshGate()
	msg := protocol.NewMessage("user", "inbox_triage", protocol.RoleUser, "inbox_message", "test", nil)
	res, err := gate.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow for restricted agent with permitted action, got %s (reason: %s)", res.Decision, res.Reason)
	}
}

func TestAgentMeshGate_RestrictedAgentDeniedAction(t *testing.T) {
	gate := NewAgentMeshGate()
	msg := protocol.NewMessage("user", "inbox_triage", protocol.RoleUser, "presentation", "test", nil)
	res, err := gate.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny for restricted agent performing disallowed action, got %s", res.Decision)
	}
	if !strings.Contains(res.Reason, "restricted") {
		t.Errorf("expected reason to mention ring name, got %q", res.Reason)
	}
}

func TestAgentMeshGate_KillSwitchActivateAndVerify(t *testing.T) {
	gate := NewAgentMeshGate()
	_, err := gate.KillSwitches().Activate(
		meshlib.GlobalKillSwitchScope(),
		meshlib.KillSwitchReasonSecurityIncident,
		"test halt",
	)
	if err != nil {
		t.Fatalf("activate: %v", err)
	}

	msg := protocol.NewMessage("user", "diagnostician", protocol.RoleUser, "presentation", "test", nil)
	res, evalErr := gate.Evaluate(context.Background(), msg)
	if evalErr != nil {
		t.Fatalf("unexpected error: %v", evalErr)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny with active global kill switch, got %s", res.Decision)
	}
	if !strings.Contains(res.Reason, "global kill switch") {
		t.Errorf("expected reason to mention global kill switch, got %q", res.Reason)
	}
}

// ============================================================================
// RequireCaseIDPolicy — additional edge cases
// ============================================================================

func TestRequireCaseIDPolicy_NonStringCaseID(t *testing.T) {
	p := RequireCaseIDPolicy{}
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "presentation", "{}", map[string]any{
		"case_id": 12345,
	})
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny for non-string case_id, got %s", res.Decision)
	}
	if res.Reason != "case_id required for clinical messages" {
		t.Errorf("expected reason %q, got %q", "case_id required for clinical messages", res.Reason)
	}
}

func TestRequireCaseIDPolicy_EmptyMetadataMap(t *testing.T) {
	p := RequireCaseIDPolicy{}
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "presentation", "{}", map[string]any{})
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny for empty metadata map (no case_id), got %s", res.Decision)
	}
}

func TestRequireCaseIDPolicy_AllClinicalTypes(t *testing.T) {
	p := RequireCaseIDPolicy{}
	clinicalTypes := []string{"presentation", "case_state", "ask_question", "order_test", "diagnosis", "verify_reasoning"}
	for _, ct := range clinicalTypes {
		msg := protocol.NewMessage("user", "intake", protocol.RoleUser, ct, "{}", map[string]any{
			"case_id": "c-test",
		})
		res, err := p.Evaluate(context.Background(), msg)
		if err != nil {
			t.Fatalf("type %q: unexpected error: %v", ct, err)
		}
		if res.Decision != DecisionAllow {
			t.Errorf("type %q: expected allow with case_id, got %s", ct, res.Decision)
		}
		if res.Reason != "case_id present" {
			t.Errorf("type %q: expected reason %q, got %q", ct, "case_id present", res.Reason)
		}
	}
}

func TestRequireCaseIDPolicy_NonClinicalTypes(t *testing.T) {
	p := RequireCaseIDPolicy{}
	nonClinical := []string{"system", "observer", "heartbeat", "tool_call", ""}
	for _, ct := range nonClinical {
		msg := protocol.NewMessage("user", "intake", protocol.RoleUser, ct, "{}", nil)
		res, err := p.Evaluate(context.Background(), msg)
		if err != nil {
			t.Fatalf("type %q: unexpected error: %v", ct, err)
		}
		if res.Decision != DecisionAllow {
			t.Errorf("type %q: expected allow for non-clinical, got %s", ct, res.Decision)
		}
		if res.Reason != "non-clinical message" {
			t.Errorf("type %q: expected reason %q, got %q", ct, "non-clinical message", res.Reason)
		}
	}
}

// ============================================================================
// RequireTenantPolicy — edge cases
// ============================================================================

func TestRequireTenantPolicy_EmptyStringNew(t *testing.T) {
	p := RequireTenantPolicy{}
	msg := protocol.NewMessage("user", "x", protocol.RoleUser, "any", "{}", map[string]any{
		"tenant_id": "",
	})
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny for empty tenant_id, got %s", res.Decision)
	}
}

func TestRequireTenantPolicy_NonStringTenantID(t *testing.T) {
	p := RequireTenantPolicy{}
	msg := protocol.NewMessage("user", "x", protocol.RoleUser, "any", "{}", map[string]any{
		"tenant_id": 42,
	})
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny for non-string tenant_id, got %s", res.Decision)
	}
}

// ============================================================================
// RequireScopePolicy — edge cases
// ============================================================================

func TestRequireScopePolicy_EmptyRequiredMap(t *testing.T) {
	p := RequireScopePolicy{Required: map[string]string{}}
	msg := protocol.NewMessage("user", "x", protocol.RoleUser, "cdm_request", "{}", nil)
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow for empty required map, got %s", res.Decision)
	}
}

func TestRequireScopePolicy_EmptyScopesString(t *testing.T) {
	p := RequireScopePolicy{Required: map[string]string{
		"cdm_request": "user/CarePlan.write",
	}}
	msg := protocol.NewMessage("user", "x", protocol.RoleUser, "cdm_request", "{}", map[string]any{
		"scopes": "",
	})
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny for empty scopes string, got %s", res.Decision)
	}
}

func TestRequireScopePolicy_ScopeNotString(t *testing.T) {
	p := RequireScopePolicy{Required: map[string]string{
		"cdm_request": "user/CarePlan.write",
	}}
	msg := protocol.NewMessage("user", "x", protocol.RoleUser, "cdm_request", "{}", map[string]any{
		"scopes": 12345,
	})
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny for non-string scopes, got %s", res.Decision)
	}
}

// ============================================================================
// Policy timeout simulation via context cancellation
// ============================================================================

func TestCompositePolicy_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := MaxContentLengthPolicy{Max: 100}
	comp := NewComposite(p, RequireCaseIDPolicy{})
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "presentation", "test",
		map[string]any{"case_id": "c1"})

	res, err := comp.Evaluate(ctx, msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow even with cancelled ctx, got %s", res.Decision)
	}
}

func TestCompositePolicy_TimeoutPolicy(t *testing.T) {
	slow := &slowPolicy{delay: 100 * time.Millisecond}
	comp := NewComposite(slow)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := comp.Evaluate(ctx, protocol.Message{})
	if err == nil {
		t.Error("expected error from timed-out policy")
	}
}

// ============================================================================
// Error wrapping and message formatting
// ============================================================================

func TestPolicyResult_DecisionStringValues(t *testing.T) {
	if DecisionAllow != "allow" {
		t.Errorf("DecisionAllow should be %q, got %q", "allow", DecisionAllow)
	}
	if DecisionDeny != "deny" {
		t.Errorf("DecisionDeny should be %q, got %q", "deny", DecisionDeny)
	}
}

func TestPolicyResult_ReasonFormatting(t *testing.T) {
	r := deny("test deny reason")
	if r.Reason != "test deny reason" {
		t.Errorf("expected reason %q, got %q", "test deny reason", r.Reason)
	}
	if r.Decision != DecisionDeny {
		t.Errorf("expected deny decision, got %s", r.Decision)
	}
	if r.CheckedAt.IsZero() {
		t.Error("CheckedAt must be non-zero for deny")
	}
}

func TestAllowHelperSetsUTCTimestamp(t *testing.T) {
	r := allow("test reason")
	if r.CheckedAt.Location() != time.UTC {
		t.Error("CheckedAt should be in UTC")
	}
	if r.Decision != DecisionAllow {
		t.Errorf("expected allow, got %s", r.Decision)
	}
}

func TestDenyHelperSetsUTCTimestamp(t *testing.T) {
	r := deny("test reason")
	if r.CheckedAt.Location() != time.UTC {
		t.Error("CheckedAt should be in UTC")
	}
	if r.Decision != DecisionDeny {
		t.Errorf("expected deny, got %s", r.Decision)
	}
}

// ============================================================================
// messageToOPAInput — edge cases
// ============================================================================

func TestMessageToOPAInput_NilMetadata(t *testing.T) {
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "test", "content", nil)
	input := messageToOPAInput(msg)
	if input["metadata"] != nil {
		t.Errorf("expected nil metadata in OPA input, got %v", input["metadata"])
	}
}

func TestMessageToOPAInput_EmptyMetadata(t *testing.T) {
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "test", "content", map[string]any{})
	input := messageToOPAInput(msg)
	md, ok := input["metadata"].(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any metadata, got %T", input["metadata"])
	}
	if len(md) != 0 {
		t.Errorf("expected empty metadata map, got %v", md)
	}
}

func TestMessageToOPAInput_AllFieldsPopulated(t *testing.T) {
	msg := protocol.NewMessage("sender", "receiver", protocol.RoleAgent, "my_type", "my_content",
		map[string]any{"key": "value"})
	input := messageToOPAInput(msg)

	if input["from"] != "sender" {
		t.Errorf("expected from=sender, got %v", input["from"])
	}
	if input["to"] != "receiver" {
		t.Errorf("expected to=receiver, got %v", input["to"])
	}
	if input["type"] != "my_type" {
		t.Errorf("expected type=my_type, got %v", input["type"])
	}
	if input["content"] != "my_content" {
		t.Errorf("expected content=my_content, got %v", input["content"])
	}
	if input["role"] != "agent" {
		t.Errorf("expected role=agent, got %v", input["role"])
	}
	md := input["metadata"].(map[string]any)
	if md["key"] != "value" {
		t.Errorf("expected metadata key=value, got %v", md["key"])
	}
}

// ============================================================================
// buildScopeModule — edge cases
// ============================================================================

func TestBuildScopeModule_EmptyMap(t *testing.T) {
	mod := buildScopeModule(map[string]string{})
	if mod == "" {
		t.Error("expected non-empty module for empty required map")
	}
	if !strings.Contains(mod, "package bodh.governance.scope") {
		t.Error("module should contain the package declaration")
	}
}

func TestBuildScopeModule_SingleEntry(t *testing.T) {
	mod := buildScopeModule(map[string]string{
		"cdm_request": "user/CarePlan.write",
	})
	if !strings.Contains(mod, `"cdm_request"`) {
		t.Error("module should contain the key")
	}
	if !strings.Contains(mod, `"user/CarePlan.write"`) {
		t.Error("module should contain the value")
	}
}

func TestBuildScopeModule_SortedKeys(t *testing.T) {
	mod := buildScopeModule(map[string]string{
		"z_type": "scope_z",
		"a_type": "scope_a",
		"m_type": "scope_m",
	})
	aIdx := strings.Index(mod, `"a_type"`)
	mIdx := strings.Index(mod, `"m_type"`)
	zIdx := strings.Index(mod, `"z_type"`)
	if aIdx >= mIdx || mIdx >= zIdx {
		t.Errorf("expected keys in sorted order: a(%d) < m(%d) < z(%d)", aIdx, mIdx, zIdx)
	}
}

// ============================================================================
// Part2PolicyName constant
// ============================================================================

func TestPart2PolicyName_Value(t *testing.T) {
	if Part2PolicyName != "42CFRPart2ConsentPolicy" {
		t.Errorf("expected %q, got %q", "42CFRPart2ConsentPolicy", Part2PolicyName)
	}
}

// ============================================================================
// hasSUDMarker — additional edge cases
// ============================================================================

func TestHasSUDMarker_EmptyString(t *testing.T) {
	if hasSUDMarker("") {
		t.Error("expected false for empty string")
	}
}

func TestHasSUDMarker_NoMatch(t *testing.T) {
	if hasSUDMarker("hypertension") {
		t.Error("expected false for non-SUD condition")
	}
}

func TestHasSUDMarker_SubstringMatch(t *testing.T) {
	if !hasSUDMarker("severe_opioid_use_disorder_chronic") {
		t.Error("expected true for compound SUD condition")
	}
}

// ============================================================================
// Test helpers used in this file
// ============================================================================

// staticPolicy is a test policy that always returns the configured decision.
type staticPolicy struct {
	decision Decision
	reason   string
	called   bool
}

func (s *staticPolicy) Evaluate(_ context.Context, _ protocol.Message) (PolicyResult, error) {
	s.called = true
	return PolicyResult{
		Decision:  s.decision,
		Reason:    s.reason,
		CheckedAt: time.Now().UTC(),
	}, nil
}

// slowPolicy simulates a policy that takes time to evaluate and respects context.
type slowPolicy struct {
	delay time.Duration
}

func (s *slowPolicy) Evaluate(ctx context.Context, _ protocol.Message) (PolicyResult, error) {
	select {
	case <-time.After(s.delay):
		return PolicyResult{Decision: DecisionAllow, Reason: "slow pass", CheckedAt: time.Now().UTC()}, nil
	case <-ctx.Done():
		return PolicyResult{}, ctx.Err()
	}
}
