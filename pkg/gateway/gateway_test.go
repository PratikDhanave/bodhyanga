package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/PratikDhanave/bodh/pkg/protocol"
)

// stubPolicy is a controllable governance.Policy for exercising every Admit
// branch without depending on a specific shipped policy's rules.
type stubPolicy struct {
	decision governance.Decision
	err      error
}

func (s stubPolicy) Evaluate(_ context.Context, _ protocol.Message) (governance.PolicyResult, error) {
	if s.err != nil {
		return governance.PolicyResult{}, s.err
	}
	return governance.PolicyResult{Decision: s.decision, Reason: "stub-reason"}, nil
}

func countKind(events []audit.Event, k audit.Kind) int {
	n := 0
	for _, e := range events {
		if e.Kind == k {
			n++
		}
	}
	return n
}

func msg(typ string) protocol.Message {
	return protocol.Message{ID: "m1", From: "test", To: "agent", Type: typ, Content: "PHI here"}
}

func TestAdmit_NilPolicyNilRecorder_Admits(t *testing.T) {
	p := &Pipeline{AgentID: "agent"}
	if !p.Admit(context.Background(), msg("case_state")) {
		t.Fatal("zero-value pipeline must admit everything")
	}
}

func TestAdmit_AllowClinical_RecordsPolicyDecisionAndMessage(t *testing.T) {
	rec := audit.NewInMemory()
	p := &Pipeline{
		AgentID:       "diagnostician",
		Policy:        stubPolicy{decision: governance.DecisionAllow},
		Recorder:      rec,
		ClinicalTypes: DefaultClinicalTypes(),
	}
	if !p.Admit(context.Background(), msg("diagnosis")) {
		t.Fatal("allow should admit")
	}
	ev := rec.List()
	if got := countKind(ev, audit.KindPolicyDecision); got != 1 {
		t.Errorf("clinical allow should record 1 policy_decision, got %d", got)
	}
	if got := countKind(ev, audit.KindMessage); got != 1 {
		t.Errorf("admit should record 1 message event, got %d", got)
	}
}

func TestAdmit_AllowNonClinical_NoPolicyAudit(t *testing.T) {
	rec := audit.NewInMemory()
	p := &Pipeline{
		AgentID:       "inbox_triage",
		Policy:        stubPolicy{decision: governance.DecisionAllow},
		Recorder:      rec,
		ClinicalTypes: DefaultClinicalTypes(),
	}
	// "inbox_message" is not a clinical type → no allow-audit, but still a
	// KindMessage invocation event.
	p.Admit(context.Background(), msg("inbox_message"))
	ev := rec.List()
	if got := countKind(ev, audit.KindPolicyDecision); got != 0 {
		t.Errorf("non-clinical allow must NOT record a policy_decision, got %d", got)
	}
	if got := countKind(ev, audit.KindMessage); got != 1 {
		t.Errorf("expected 1 message event, got %d", got)
	}
}

func TestAdmit_Deny_AuditsAndBlocks(t *testing.T) {
	rec := audit.NewInMemory()
	p := &Pipeline{
		AgentID:       "agent",
		Policy:        stubPolicy{decision: governance.DecisionDeny},
		Recorder:      rec,
		ClinicalTypes: DefaultClinicalTypes(),
	}
	if p.Admit(context.Background(), msg("case_state")) {
		t.Fatal("deny must block")
	}
	ev := rec.List()
	if got := countKind(ev, audit.KindPolicyDecision); got != 1 {
		t.Errorf("deny should record 1 policy_decision, got %d", got)
	}
	if got := countKind(ev, audit.KindMessage); got != 0 {
		t.Errorf("blocked message must NOT record a KindMessage invocation, got %d", got)
	}
}

func TestAdmit_PolicyError_FailsClosed(t *testing.T) {
	rec := audit.NewInMemory()
	p := &Pipeline{
		AgentID:  "agent",
		Policy:   stubPolicy{err: errors.New("boom")},
		Recorder: rec,
	}
	if p.Admit(context.Background(), msg("case_state")) {
		t.Fatal("policy error must fail closed (block)")
	}
	if got := countKind(rec.List(), audit.KindMessage); got != 0 {
		t.Errorf("failed-closed message must not be invoked, got %d message events", got)
	}
}

func TestAdmit_ExtraMiddlewareBlocks_AuditsDeny(t *testing.T) {
	rec := audit.NewInMemory()
	blocked := FuncMiddleware{
		Label: "content_safety",
		Fn: func(_ context.Context, _ protocol.Message) (bool, string, error) {
			return false, "unsafe content", nil
		},
	}
	p := &Pipeline{
		AgentID:  "agent",
		Policy:   stubPolicy{decision: governance.DecisionAllow},
		Recorder: rec,
		Extra:    []Middleware{blocked},
	}
	if p.Admit(context.Background(), msg("case_state")) {
		t.Fatal("extra middleware block must stop admission")
	}
	ev := rec.List()
	if got := countKind(ev, audit.KindPolicyDecision); got != 1 {
		t.Errorf("middleware block should audit one deny, got %d", got)
	}
	if got := countKind(ev, audit.KindMessage); got != 0 {
		t.Errorf("blocked message must not be invoked, got %d", got)
	}
}

func TestAdmit_ExtraMiddlewareError_FailsClosed(t *testing.T) {
	p := &Pipeline{
		AgentID: "agent",
		Policy:  stubPolicy{decision: governance.DecisionAllow},
		Extra: []Middleware{FuncMiddleware{
			Label: "circuit_breaker",
			Fn: func(_ context.Context, _ protocol.Message) (bool, string, error) {
				return true, "", errors.New("breaker tripped")
			},
		}},
	}
	if p.Admit(context.Background(), msg("case_state")) {
		t.Fatal("middleware error must fail closed")
	}
}

func TestAdmit_ExtraMiddlewarePasses_Admits(t *testing.T) {
	pass := FuncMiddleware{
		Label: "noop",
		Fn: func(_ context.Context, _ protocol.Message) (bool, string, error) {
			return true, "", nil
		},
	}
	p := &Pipeline{AgentID: "agent", Extra: []Middleware{pass}}
	if !p.Admit(context.Background(), msg("case_state")) {
		t.Fatal("passing middleware with no policy should admit")
	}
}

// TestAdmit_AuditNeverCarriesContent guards the PHI invariant: no event the
// gateway records may contain msg.Content.
func TestAdmit_AuditNeverCarriesContent(t *testing.T) {
	rec := audit.NewInMemory()
	p := &Pipeline{
		AgentID:       "agent",
		Policy:        stubPolicy{decision: governance.DecisionAllow},
		Recorder:      rec,
		ClinicalTypes: DefaultClinicalTypes(),
	}
	secret := "SECRET-PHI-PAYLOAD"
	m := msg("diagnosis")
	m.Content = secret
	p.Admit(context.Background(), m)
	for _, e := range rec.List() {
		if e.Reason == secret || e.ErrorText == secret {
			t.Fatalf("audit event leaked msg.Content: %+v", e)
		}
	}
}
