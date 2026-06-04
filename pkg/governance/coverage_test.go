package governance

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/protocol"
)

// ---------------------------------------------------------------------------
// SMARTScopePolicy
// ---------------------------------------------------------------------------

func TestSMARTScopePolicy_NoMetadata(t *testing.T) {
	p := NewSMARTScopePolicy("iss", "http://localhost/jwks", "patient/Observation.read")
	msg := protocol.Message{Content: "test"}
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny, got %v", res.Decision)
	}
}

func TestSMARTScopePolicy_NoJWTToken(t *testing.T) {
	p := NewSMARTScopePolicy("iss", "http://localhost/jwks", "patient/Observation.read")
	msg := protocol.Message{
		Content:  "test",
		Metadata: map[string]any{},
	}
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny, got %v", res.Decision)
	}
}

func TestSMARTScopePolicy_EmptyJWTToken(t *testing.T) {
	p := NewSMARTScopePolicy("iss", "http://localhost/jwks", "patient/Observation.read")
	msg := protocol.Message{
		Content:  "test",
		Metadata: map[string]any{"jwt_token": ""},
	}
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny, got %v", res.Decision)
	}
}

func TestSMARTScopePolicy_InvalidToken(t *testing.T) {
	// Serve an empty JWKS so validation fails.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"keys":[]}`))
	}))
	defer srv.Close()

	p := NewSMARTScopePolicy("iss", srv.URL, "patient/Observation.read")
	msg := protocol.Message{
		Content:  "test",
		Metadata: map[string]any{"jwt_token": "invalid.jwt.token"},
	}
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny for invalid token, got %v", res.Decision)
	}
}

// ---------------------------------------------------------------------------
// OPA Policy: MustOPAPolicy panic + malformed Rego
// ---------------------------------------------------------------------------

func TestMustOPAPolicy_Panics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for invalid Rego")
		}
	}()
	MustOPAPolicy("this is not valid rego!!!", "data.x")
}

func TestNewOPAPolicy_MalformedRego(t *testing.T) {
	_, err := NewOPAPolicy("invalid rego {{{", "data.x")
	if err == nil {
		t.Error("expected error for malformed Rego")
	}
}

func TestOPAPolicy_EmptyResultSet(t *testing.T) {
	// A module that never defines the queried rule.
	module := `package bodh.test
import rego.v1
other_rule := true`
	p, err := NewOPAPolicy(module, "data.bodh.test.nonexistent")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	msg := protocol.Message{Content: "test"}
	res, evalErr := p.Evaluate(context.Background(), msg)
	if evalErr != nil {
		t.Fatalf("unexpected error: %v", evalErr)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny for empty result, got %v", res.Decision)
	}
}

func TestOPAPolicy_NonObjectResult(t *testing.T) {
	// A rule that returns a string, not an object.
	module := `package bodh.test
import rego.v1
result := "not an object"`
	p, err := NewOPAPolicy(module, "data.bodh.test.result")
	if err != nil {
		t.Fatalf("compile error: %v", err)
	}
	msg := protocol.Message{Content: "test"}
	res, evalErr := p.Evaluate(context.Background(), msg)
	if evalErr != nil {
		t.Fatalf("unexpected error: %v", evalErr)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny for non-object, got %v", res.Decision)
	}
}

// ---------------------------------------------------------------------------
// RateLimitPolicy: NewRateLimitPolicy with zero config
// ---------------------------------------------------------------------------

func TestNewRateLimitPolicy_ZeroConfig(t *testing.T) {
	p := NewRateLimitPolicy(RateLimitConfig{})
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

func TestRateLimitPolicy_RateLimitExceeded(t *testing.T) {
	// Very low limit to force exhaustion.
	p := NewRateLimitPolicy(RateLimitConfig{
		PerTenantRPS: 1,
		PerAgentRPS:  1,
		BurstFactor:  1,
	})

	msg := protocol.Message{
		From: "agent1",
		Metadata: map[string]any{
			"tenant_id": "t1",
		},
	}

	// First call should succeed.
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow on first call, got %v", res.Decision)
	}

	// Rapid second call should be denied.
	res, err = p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny on rapid second call, got %v", res.Decision)
	}
}

// ---------------------------------------------------------------------------
// RedactNarrativePolicy: non-JSON content, hasNarrativeType
// ---------------------------------------------------------------------------

func TestRedactNarrativePolicy_NonJSON(t *testing.T) {
	p := RedactNarrativePolicy{}
	msg := protocol.Message{
		Content:  "plain text, not JSON",
		Metadata: map[string]any{"destination": "audit"},
	}
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow for non-JSON, got %v", res.Decision)
	}
}

func TestRedactNarrativePolicy_NoNarrativeFields(t *testing.T) {
	p := RedactNarrativePolicy{}
	content, _ := json.Marshal(map[string]any{"diagnosis": "asthma"})
	msg := protocol.Message{
		Content:  string(content),
		Metadata: map[string]any{"destination": "audit"},
	}
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow, got %v", res.Decision)
	}
}

func TestHasNarrativeType_Positive(t *testing.T) {
	for _, typ := range []string{"presentation", "Diagnosis", "case_state", "PLAN"} {
		if !hasNarrativeType(typ) {
			t.Errorf("expected true for %q", typ)
		}
	}
}

func TestHasNarrativeType_Negative(t *testing.T) {
	if hasNarrativeType("tool_call") {
		t.Error("expected false for tool_call")
	}
}

// ---------------------------------------------------------------------------
// BreakGlassPolicy: empty reason
// ---------------------------------------------------------------------------

func TestBreakGlassPolicy_EmptyReason(t *testing.T) {
	p := BreakGlassPolicy{}
	msg := protocol.Message{
		Metadata: map[string]any{
			"break_glass":           "true",
			"break_glass_reason":    "",
			"break_glass_clinician": "Dr. Smith",
		},
	}
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny for empty reason, got %v", res.Decision)
	}
}

func TestBreakGlassPolicy_EmptyClinician(t *testing.T) {
	p := BreakGlassPolicy{}
	msg := protocol.Message{
		Metadata: map[string]any{
			"break_glass":           "true",
			"break_glass_reason":    "emergency",
			"break_glass_clinician": "",
		},
	}
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny for empty clinician, got %v", res.Decision)
	}
}

// ---------------------------------------------------------------------------
// buildScopeModule: trailing spaces in scope
// ---------------------------------------------------------------------------

func TestBuildScopeModule_WithScopes(t *testing.T) {
	mod := buildScopeModule(map[string]string{
		"cdm_request": "user/CarePlan.write",
		"read_event":  "patient/Observation.read",
	})
	if mod == "" {
		t.Error("expected non-empty module")
	}
}

// ---------------------------------------------------------------------------
// Part2ConsentPolicy: empty metadata
// ---------------------------------------------------------------------------

func TestPart2ConsentPolicy_NoMetadata(t *testing.T) {
	p := Part2ConsentPolicy{}
	msg := protocol.Message{Content: "test"}
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow (no SUD marker), got %v", res.Decision)
	}
}

// ---------------------------------------------------------------------------
// AgentMeshGate: ringName coverage
// ---------------------------------------------------------------------------

func TestRingName_Unassigned(t *testing.T) {
	gate := NewAgentMeshGate()
	name := ringName(gate.rings, "nonexistent_agent")
	if name != "unassigned" {
		t.Errorf("expected unassigned, got %q", name)
	}
}

func TestRingName_AllRings(t *testing.T) {
	gate := NewAgentMeshGate()

	tests := []struct {
		agentID  string
		expected string
	}{
		{"intake_agent", "standard"},
		{"inbox_triage", "restricted"},
		{"audio_transcriber", "sandboxed"},
	}
	for _, tt := range tests {
		name := ringName(gate.rings, tt.agentID)
		if name != tt.expected {
			t.Errorf("agent %q: expected %q, got %q", tt.agentID, tt.expected, name)
		}
	}
}

// ---------------------------------------------------------------------------
// RequireScopePolicy with trailing spaces
// ---------------------------------------------------------------------------

func TestRequireScopePolicy_TrailingSpaces(t *testing.T) {
	p := RequireScopePolicy{
		Required: map[string]string{
			"cdm_request": "user/CarePlan.write",
		},
	}
	msg := protocol.Message{
		Type:     "cdm_request",
		Metadata: map[string]any{"scopes": "  user/CarePlan.write  other/scope  "},
	}
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow with trailing spaces, got %v (reason: %s)", res.Decision, res.Reason)
	}
}

// ---------------------------------------------------------------------------
// CompositePolicy: error propagation
// ---------------------------------------------------------------------------

func TestCompositePolicy_ErrorFromChild(t *testing.T) {
	errPolicy := errorPolicy{err: fmt.Errorf("policy broke")}
	comp := NewComposite(&errPolicy)
	msg := protocol.Message{}
	_, err := comp.Evaluate(context.Background(), msg)
	if err == nil {
		t.Error("expected error from child policy")
	}
}

type errorPolicy struct {
	err error
}

func (e *errorPolicy) Evaluate(_ context.Context, _ protocol.Message) (PolicyResult, error) {
	return PolicyResult{}, e.err
}

// ---------------------------------------------------------------------------
// CompositePolicy: deny with zero CheckedAt
// ---------------------------------------------------------------------------

func TestCompositePolicy_DenyBackfillsCheckedAt(t *testing.T) {
	zeroTimeDeny := &zeroTimeDenyPolicy{}
	comp := NewComposite(zeroTimeDeny)
	msg := protocol.Message{}
	res, err := comp.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny, got %v", res.Decision)
	}
	if res.CheckedAt.IsZero() {
		t.Error("expected CheckedAt to be backfilled")
	}
}

type zeroTimeDenyPolicy struct{}

func (z *zeroTimeDenyPolicy) Evaluate(_ context.Context, _ protocol.Message) (PolicyResult, error) {
	return PolicyResult{Decision: DecisionDeny, Reason: "test deny"}, nil
}

// ---------------------------------------------------------------------------
// RedactNarrativePolicy: log-bound with nil metadata guard
// ---------------------------------------------------------------------------

func TestRedactNarrativePolicy_NonLogBoundDestination(t *testing.T) {
	p := RedactNarrativePolicy{}
	msg := protocol.Message{
		Content:  `{"hpi":"patient reports chest pain"}`,
		Metadata: map[string]any{"destination": "agent"},
	}
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow for non-log-bound, got %v", res.Decision)
	}
}

// ---------------------------------------------------------------------------
// RequireScopePolicy: no metadata at all
// ---------------------------------------------------------------------------

func TestRequireScopePolicy_NoMetadata(t *testing.T) {
	p := RequireScopePolicy{
		Required: map[string]string{"cdm_request": "user/CarePlan.write"},
	}
	msg := protocol.Message{Type: "cdm_request"}
	res, err := p.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatal(err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny, got %v", res.Decision)
	}
}

// ---------------------------------------------------------------------------
// OPA NewContentLengthOPAPolicy: negative max
// ---------------------------------------------------------------------------

func TestNewContentLengthOPAPolicy_NegativeMax(t *testing.T) {
	_, err := NewContentLengthOPAPolicy(-1)
	if err == nil {
		t.Error("expected error for negative max")
	}
}
