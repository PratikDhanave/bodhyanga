package governance_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/PratikDhanave/bodh/pkg/protocol"
)

// makeMsg is a test helper that builds a protocol.Message with sensible
// defaults. Callers override fields by name after construction.
func makeMsg() protocol.Message {
	return protocol.Message{
		ID:        "msg-test-1",
		From:      "user",
		To:        "intake",
		Role:      protocol.RoleUser,
		Type:      "presentation",
		Content:   "patient presents with chest pain",
		CreatedAt: time.Now().UTC(),
		Metadata: map[string]any{
			"case_id":   "case-001",
			"tenant_id": "tenant-abc",
		},
	}
}

// ============================================================================
// NewOPAPolicy
// ============================================================================

func TestNewOPAPolicy_BadModule(t *testing.T) {
	_, err := governance.NewOPAPolicy("this is not rego", "data.bodh.x.result")
	if err == nil {
		t.Fatal("expected compile error for invalid Rego source")
	}
}

func TestNewOPAPolicy_ValidModule(t *testing.T) {
	module := `package bodh.test
import rego.v1
default result := {"allowed": true, "reason": "ok"}`
	p, err := governance.NewOPAPolicy(module, "data.bodh.test.result")
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}
	res, err := p.Evaluate(context.Background(), makeMsg())
	if err != nil {
		t.Fatalf("unexpected eval error: %v", err)
	}
	if res.Decision != governance.DecisionAllow {
		t.Errorf("want allow, got %s (%s)", res.Decision, res.Reason)
	}
}

func TestMustOPAPolicy_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for bad Rego module")
		}
	}()
	governance.MustOPAPolicy("not rego at all", "data.bodh.nope")
}

// ============================================================================
// NewCaseIDOPAPolicy — mirrors RequireCaseIDPolicy test cases
// ============================================================================

func TestCaseIDOPAPolicy_NonClinical(t *testing.T) {
	p, err := governance.NewCaseIDOPAPolicy()
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	msg := makeMsg()
	msg.Type = "system" // non-clinical
	msg.Metadata = nil

	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if res.Decision != governance.DecisionAllow {
		t.Errorf("want allow for non-clinical, got deny: %s", res.Reason)
	}
	if res.Reason != "non-clinical message" {
		t.Errorf("want reason %q, got %q", "non-clinical message", res.Reason)
	}
}

func TestCaseIDOPAPolicy_ClinicalMissingMetadata(t *testing.T) {
	p, err := governance.NewCaseIDOPAPolicy()
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	msg := makeMsg()
	msg.Type = "presentation"
	msg.Metadata = nil

	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if res.Decision != governance.DecisionDeny {
		t.Errorf("want deny when metadata nil, got allow")
	}
	if res.Reason != "missing metadata with case_id" {
		t.Errorf("want reason %q, got %q", "missing metadata with case_id", res.Reason)
	}
}

func TestCaseIDOPAPolicy_ClinicalMissingCaseID(t *testing.T) {
	p, err := governance.NewCaseIDOPAPolicy()
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	msg := makeMsg()
	msg.Type = "order_test"
	msg.Metadata = map[string]any{"tenant_id": "t1"} // no case_id

	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if res.Decision != governance.DecisionDeny {
		t.Errorf("want deny when case_id absent, got allow")
	}
	if res.Reason != "case_id required for clinical messages" {
		t.Errorf("want reason %q, got %q",
			"case_id required for clinical messages", res.Reason)
	}
}

func TestCaseIDOPAPolicy_ClinicalWithCaseID(t *testing.T) {
	p, err := governance.NewCaseIDOPAPolicy()
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	for _, msgType := range []string{
		"presentation", "case_state", "ask_question",
		"order_test", "diagnosis", "verify_reasoning",
	} {
		msg := makeMsg()
		msg.Type = msgType
		res, err := p.Evaluate(context.Background(), msg)
		if err != nil {
			t.Fatalf("type %s: eval: %v", msgType, err)
		}
		if res.Decision != governance.DecisionAllow {
			t.Errorf("type %s: want allow with case_id present, got deny: %s",
				msgType, res.Reason)
		}
		if res.Reason != "case_id present" {
			t.Errorf("type %s: want reason %q, got %q",
				msgType, "case_id present", res.Reason)
		}
	}
}

// ============================================================================
// NewDiagnosticCostOPAPolicy — mirrors MaxDiagnosticCostPolicy test cases
// ============================================================================

func TestDiagnosticCostOPAPolicy_NotOrderTest(t *testing.T) {
	p, err := governance.NewDiagnosticCostOPAPolicy()
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	msg := makeMsg()
	msg.Type = "presentation"

	res, _ := p.Evaluate(context.Background(), msg)
	if res.Decision != governance.DecisionAllow {
		t.Errorf("want allow for non-order_test, got deny: %s", res.Reason)
	}
}

func TestDiagnosticCostOPAPolicy_WithinBudget(t *testing.T) {
	p, err := governance.NewDiagnosticCostOPAPolicy()
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	msg := makeMsg()
	msg.Type = "order_test"
	msg.Metadata = map[string]any{
		"case_id":       "c1",
		"max_cost_usd":  float64(200),
		"test_cost_usd": float64(50),
		"spent_usd":     float64(100),
	}

	res, _ := p.Evaluate(context.Background(), msg)
	if res.Decision != governance.DecisionAllow {
		t.Errorf("want allow within budget, got deny: %s", res.Reason)
	}
	if res.Reason != "within diagnostic budget" {
		t.Errorf("want reason %q, got %q", "within diagnostic budget", res.Reason)
	}
}

func TestDiagnosticCostOPAPolicy_ExceedsBudget(t *testing.T) {
	p, err := governance.NewDiagnosticCostOPAPolicy()
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	msg := makeMsg()
	msg.Type = "order_test"
	msg.Metadata = map[string]any{
		"case_id":       "c1",
		"max_cost_usd":  float64(200),
		"test_cost_usd": float64(150),
		"spent_usd":     float64(100), // 100+150 > 200
	}

	res, _ := p.Evaluate(context.Background(), msg)
	if res.Decision != governance.DecisionDeny {
		t.Errorf("want deny when over budget, got allow")
	}
	if res.Reason != "test would exceed diagnostic budget" {
		t.Errorf("want reason %q, got %q",
			"test would exceed diagnostic budget", res.Reason)
	}
}

func TestDiagnosticCostOPAPolicy_ExactlyAtBudget(t *testing.T) {
	p, err := governance.NewDiagnosticCostOPAPolicy()
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	msg := makeMsg()
	msg.Type = "order_test"
	msg.Metadata = map[string]any{
		"case_id":       "c1",
		"max_cost_usd":  float64(200),
		"test_cost_usd": float64(100),
		"spent_usd":     float64(100), // exactly 200 — allow (<=)
	}

	res, _ := p.Evaluate(context.Background(), msg)
	if res.Decision != governance.DecisionAllow {
		t.Errorf("want allow at exactly budget (<=), got deny: %s", res.Reason)
	}
}

func TestDiagnosticCostOPAPolicy_NoMaxCost(t *testing.T) {
	p, err := governance.NewDiagnosticCostOPAPolicy()
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	msg := makeMsg()
	msg.Type = "order_test"
	msg.Metadata = map[string]any{
		"case_id":       "c1",
		"test_cost_usd": float64(500), // no ceiling → allow
	}

	res, _ := p.Evaluate(context.Background(), msg)
	if res.Decision != governance.DecisionAllow {
		t.Errorf("want allow when no max_cost_usd, got deny: %s", res.Reason)
	}
}

// ============================================================================
// NewTenantOPAPolicy — mirrors RequireTenantPolicy test cases
// ============================================================================

func TestTenantOPAPolicy_Present(t *testing.T) {
	p, err := governance.NewTenantOPAPolicy()
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	res, _ := p.Evaluate(context.Background(), makeMsg())
	if res.Decision != governance.DecisionAllow {
		t.Errorf("want allow with tenant_id, got deny: %s", res.Reason)
	}
	if res.Reason != "tenant_id present" {
		t.Errorf("want reason %q, got %q", "tenant_id present", res.Reason)
	}
}

func TestTenantOPAPolicy_MissingMetadata(t *testing.T) {
	p, err := governance.NewTenantOPAPolicy()
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	msg := makeMsg()
	msg.Metadata = nil

	res, _ := p.Evaluate(context.Background(), msg)
	if res.Decision != governance.DecisionDeny {
		t.Errorf("want deny when metadata nil, got allow")
	}
	if res.Reason != "tenant_id required: no metadata" {
		t.Errorf("want reason %q, got %q",
			"tenant_id required: no metadata", res.Reason)
	}
}

func TestTenantOPAPolicy_EmptyTenantID(t *testing.T) {
	p, err := governance.NewTenantOPAPolicy()
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	msg := makeMsg()
	msg.Metadata = map[string]any{"tenant_id": ""}

	res, _ := p.Evaluate(context.Background(), msg)
	if res.Decision != governance.DecisionDeny {
		t.Errorf("want deny for empty tenant_id, got allow")
	}
	if res.Reason != "tenant_id required" {
		t.Errorf("want reason %q, got %q", "tenant_id required", res.Reason)
	}
}

func TestTenantOPAPolicy_MissingTenantID(t *testing.T) {
	p, err := governance.NewTenantOPAPolicy()
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	msg := makeMsg()
	msg.Metadata = map[string]any{"case_id": "c1"} // tenant_id absent

	res, _ := p.Evaluate(context.Background(), msg)
	if res.Decision != governance.DecisionDeny {
		t.Errorf("want deny when tenant_id key absent, got allow")
	}
}

// ============================================================================
// NewContentLengthOPAPolicy
// ============================================================================

func TestContentLengthOPAPolicy_WithinLimit(t *testing.T) {
	p, err := governance.NewContentLengthOPAPolicy(100)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	msg := makeMsg()
	msg.Content = strings.Repeat("a", 50)

	res, _ := p.Evaluate(context.Background(), msg)
	if res.Decision != governance.DecisionAllow {
		t.Errorf("want allow within limit, got deny: %s", res.Reason)
	}
	if res.Reason != "within content length limit" {
		t.Errorf("want reason %q, got %q", "within content length limit", res.Reason)
	}
}

func TestContentLengthOPAPolicy_ExceedsLimit(t *testing.T) {
	p, err := governance.NewContentLengthOPAPolicy(10)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	msg := makeMsg()
	msg.Content = strings.Repeat("x", 11)

	res, _ := p.Evaluate(context.Background(), msg)
	if res.Decision != governance.DecisionDeny {
		t.Errorf("want deny when content exceeds limit, got allow")
	}
	if res.Reason != "content too long" {
		t.Errorf("want reason %q, got %q", "content too long", res.Reason)
	}
}

func TestContentLengthOPAPolicy_ExactlyAtLimit(t *testing.T) {
	p, err := governance.NewContentLengthOPAPolicy(10)
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	msg := makeMsg()
	msg.Content = strings.Repeat("x", 10) // exactly at limit → allow

	res, _ := p.Evaluate(context.Background(), msg)
	if res.Decision != governance.DecisionAllow {
		t.Errorf("want allow at exactly limit (<=), got deny: %s", res.Reason)
	}
}

func TestContentLengthOPAPolicy_InvalidMax(t *testing.T) {
	_, err := governance.NewContentLengthOPAPolicy(0)
	if err == nil {
		t.Fatal("expected error for maxBytes=0")
	}
	_, err = governance.NewContentLengthOPAPolicy(-1)
	if err == nil {
		t.Fatal("expected error for maxBytes=-1")
	}
}

// ============================================================================
// NewScopeOPAPolicy — mirrors RequireScopePolicy test cases
// ============================================================================

func TestScopeOPAPolicy_TypeNotGated(t *testing.T) {
	p, err := governance.NewScopeOPAPolicy(map[string]string{
		"cdm_request": "user/CarePlan.write",
	})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	msg := makeMsg()
	msg.Type = "presentation" // not in required map

	res, _ := p.Evaluate(context.Background(), msg)
	if res.Decision != governance.DecisionAllow {
		t.Errorf("want allow for ungated type, got deny: %s", res.Reason)
	}
	if res.Reason != "no scope required for type" {
		t.Errorf("want reason %q, got %q", "no scope required for type", res.Reason)
	}
}

func TestScopeOPAPolicy_ScopePresent(t *testing.T) {
	p, err := governance.NewScopeOPAPolicy(map[string]string{
		"cdm_request": "user/CarePlan.write",
	})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	msg := makeMsg()
	msg.Type = "cdm_request"
	msg.Metadata = map[string]any{
		"scopes": "openid user/CarePlan.write user/Observation.read",
	}

	res, _ := p.Evaluate(context.Background(), msg)
	if res.Decision != governance.DecisionAllow {
		t.Errorf("want allow when scope present, got deny: %s", res.Reason)
	}
	if !strings.Contains(res.Reason, "scope present") {
		t.Errorf("want reason to contain %q, got %q", "scope present", res.Reason)
	}
}

func TestScopeOPAPolicy_ScopeAbsent(t *testing.T) {
	p, err := governance.NewScopeOPAPolicy(map[string]string{
		"cdm_request": "user/CarePlan.write",
	})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	msg := makeMsg()
	msg.Type = "cdm_request"
	msg.Metadata = map[string]any{
		"scopes": "openid user/Observation.read", // missing CarePlan.write
	}

	res, _ := p.Evaluate(context.Background(), msg)
	if res.Decision != governance.DecisionDeny {
		t.Errorf("want deny when required scope absent, got allow")
	}
	if !strings.Contains(res.Reason, "required scope missing") {
		t.Errorf("want reason to contain %q, got %q", "required scope missing", res.Reason)
	}
}

func TestScopeOPAPolicy_NoMetadata(t *testing.T) {
	p, err := governance.NewScopeOPAPolicy(map[string]string{
		"cdm_request": "user/CarePlan.write",
	})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	msg := makeMsg()
	msg.Type = "cdm_request"
	msg.Metadata = nil

	res, _ := p.Evaluate(context.Background(), msg)
	if res.Decision != governance.DecisionDeny {
		t.Errorf("want deny when metadata nil and scope required, got allow")
	}
}

func TestScopeOPAPolicy_EmptyRequired(t *testing.T) {
	// Empty required map → no types gated → always allow.
	p, err := governance.NewScopeOPAPolicy(map[string]string{})
	if err != nil {
		t.Fatalf("construct: %v", err)
	}
	msg := makeMsg()
	msg.Type = "cdm_request"

	res, _ := p.Evaluate(context.Background(), msg)
	if res.Decision != governance.DecisionAllow {
		t.Errorf("want allow when required map is empty, got deny: %s", res.Reason)
	}
}

// ============================================================================
// OPAPolicy composes with CompositePolicy
// ============================================================================

func TestOPAPolicy_ComposesWithComposite(t *testing.T) {
	caseID, err := governance.NewCaseIDOPAPolicy()
	if err != nil {
		t.Fatalf("case_id policy: %v", err)
	}
	tenant, err := governance.NewTenantOPAPolicy()
	if err != nil {
		t.Fatalf("tenant policy: %v", err)
	}
	cost, err := governance.NewDiagnosticCostOPAPolicy()
	if err != nil {
		t.Fatalf("cost policy: %v", err)
	}

	composite := governance.NewComposite(caseID, tenant, cost)

	// Happy path — all policies allow.
	msg := makeMsg()
	res, err := composite.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("composite eval: %v", err)
	}
	if res.Decision != governance.DecisionAllow {
		t.Errorf("want composite allow, got deny: %s", res.Reason)
	}

	// First deny short-circuits — remove case_id.
	msg.Metadata = map[string]any{"tenant_id": "t1"} // no case_id
	res, err = composite.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("composite eval deny: %v", err)
	}
	if res.Decision != governance.DecisionDeny {
		t.Errorf("want composite deny when case_id absent, got allow")
	}
}

// ============================================================================
// OPAPolicy parallel to Go policy — equivalence tests
// ============================================================================

// TestOPAEquivalence_CaseID checks that OPAPolicy and RequireCaseIDPolicy
// produce the same Decision for a representative set of inputs.
func TestOPAEquivalence_CaseID(t *testing.T) {
	opaPolicy, err := governance.NewCaseIDOPAPolicy()
	if err != nil {
		t.Fatalf("construct OPA policy: %v", err)
	}
	goPolicy := governance.RequireCaseIDPolicy{}

	cases := []struct {
		name string
		msg  protocol.Message
	}{
		{"clinical with case_id", makeMsg()},
		{
			"clinical without case_id",
			func() protocol.Message {
				m := makeMsg()
				m.Metadata = map[string]any{"tenant_id": "t1"}
				return m
			}(),
		},
		{
			"non-clinical no metadata",
			func() protocol.Message {
				m := makeMsg()
				m.Type = "observer"
				m.Metadata = nil
				return m
			}(),
		},
		{
			"clinical nil metadata",
			func() protocol.Message {
				m := makeMsg()
				m.Metadata = nil
				return m
			}(),
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			opaRes, err := opaPolicy.Evaluate(context.Background(), tc.msg)
			if err != nil {
				t.Fatalf("opa eval: %v", err)
			}
			goRes, err := goPolicy.Evaluate(context.Background(), tc.msg)
			if err != nil {
				t.Fatalf("go eval: %v", err)
			}
			if opaRes.Decision != goRes.Decision {
				t.Errorf("OPA=%s Go=%s for %q (opa_reason=%q go_reason=%q)",
					opaRes.Decision, goRes.Decision, tc.name,
					opaRes.Reason, goRes.Reason)
			}
		})
	}
}
