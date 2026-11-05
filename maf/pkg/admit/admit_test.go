package admit

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/audit"
	"github.com/PratikDhanave/bodhyanga/pkg/governance"
	"github.com/PratikDhanave/bodhyanga/pkg/protocol"
)

// capturingRecorder records events; optionally fails every write.
type capturingRecorder struct {
	mu     sync.Mutex
	events []audit.Event
	fail   bool
}

func (r *capturingRecorder) Record(e audit.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return errors.New("audit sink down")
	}
	r.events = append(r.events, e)
	return nil
}

func (r *capturingRecorder) List() []audit.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]audit.Event(nil), r.events...)
}

func TestGuardAllowRunsHandlerAfterAllowRecord(t *testing.T) {
	rec := &capturingRecorder{}
	b := New(AllowAll(), rec, nil)

	ran := false
	err := b.Guard(context.Background(), "diagnostician", TypeWorkup, "abcd1234", func() error {
		// The allow record must already exist when the handler runs.
		if n := len(rec.List()); n != 1 {
			t.Errorf("handler saw %d audit events, want 1 (allow before execution)", n)
		}
		ran = true
		return nil
	})
	if err != nil || !ran {
		t.Fatalf("Guard = %v, ran = %v; want nil, true", err, ran)
	}
	e := rec.List()[0]
	if e.Kind != audit.KindPolicyDecision || e.Decision != string(governance.DecisionAllow) ||
		e.AgentID != "diagnostician" || e.Type != TypeWorkup || e.CaseID != "abcd1234" {
		t.Errorf("allow event malformed: %+v", e)
	}
}

func TestGuardDenyBlocksHandlerAndRecordsSynchronously(t *testing.T) {
	rec := &capturingRecorder{}
	b := New(DenyAll(), rec, nil)

	err := b.Guard(context.Background(), "intake", TypePatientCase, "abcd1234", func() error {
		t.Fatal("handler executed after denial")
		return nil
	})
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("Guard = %v, want ErrDenied", err)
	}
	// Synchronous ordering: the deny record exists the instant Guard returns.
	evs := rec.List()
	if len(evs) != 1 || evs[0].Decision != string(governance.DecisionDeny) || evs[0].Reason != "deny_all" {
		t.Fatalf("deny record missing/malformed: %+v", evs)
	}
}

func TestPolicyErrorIsDenial(t *testing.T) {
	rec := &capturingRecorder{}
	b := New(Erroring(), rec, nil)

	err := b.Admit(context.Background(), "intake", TypePatientCase, "abcd1234")
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("Admit = %v, want ErrDenied (fail-closed on policy error)", err)
	}
	evs := rec.List()
	if len(evs) != 1 || evs[0].Reason != "policy_error" {
		t.Fatalf("policy-error denial not audited as policy_error: %+v", evs)
	}
}

func TestUnlabeledMessageTypeIsDenied(t *testing.T) {
	rec := &capturingRecorder{}
	b := New(AllowAll(), rec, nil)

	if err := b.Admit(context.Background(), "intake", "mystery_type", "abcd1234"); !errors.Is(err, ErrDenied) {
		t.Fatalf("Admit(unlabeled) = %v, want ErrDenied", err)
	}
	if evs := rec.List(); len(evs) != 1 || evs[0].Decision != string(governance.DecisionDeny) {
		t.Fatalf("unlabeled-type denial not audited: %+v", evs)
	}
}

func TestAuditFailureIsFailOpen(t *testing.T) {
	rec := &capturingRecorder{fail: true}

	// Allow path: handler still runs despite audit failure.
	ran := false
	if err := New(AllowAll(), rec, nil).Guard(context.Background(), "intake", TypePatientCase, "x", func() error {
		ran = true
		return nil
	}); err != nil || !ran {
		t.Fatalf("allow path blocked by audit failure: err=%v ran=%v", err, ran)
	}

	// Deny path: still denied despite audit failure.
	if err := New(DenyAll(), rec, nil).Admit(context.Background(), "intake", TypePatientCase, "x"); !errors.Is(err, ErrDenied) {
		t.Fatalf("deny path altered by audit failure: %v", err)
	}
}

func TestHandlerErrorIsAuditedAndPropagated(t *testing.T) {
	rec := &capturingRecorder{}
	b := New(AllowAll(), rec, nil)

	boom := errors.New("handler exploded")
	err := b.Guard(context.Background(), "care_plan", TypeDiagnosisResult, "abcd1234", func() error {
		return boom
	})
	// The full error still propagates to the caller (control flow is unchanged)...
	if !errors.Is(err, boom) {
		t.Fatalf("Guard = %v, want handler error propagated", err)
	}
	evs := rec.List()
	// ...but an unmarked error's message is redacted before it reaches the trail
	// (SafeErrorText guard). See TestSafeErrorText for the passthrough case.
	if len(evs) != 2 || evs[1].Kind != audit.KindAgentError {
		t.Fatalf("agent_error record missing/malformed: %+v", evs)
	}
	if evs[1].ErrorText == "handler exploded" || !strings.Contains(evs[1].ErrorText, "redacted") {
		t.Fatalf("agent_error ErrorText not redacted: %q", evs[1].ErrorText)
	}
}

func TestEnvelopeIsPHIFree(t *testing.T) {
	env, err := buildEnvelope("diagnostician", TypeWorkup, "abcd1234")
	if err != nil {
		t.Fatal(err)
	}
	if env.Content != "" {
		t.Error("envelope carries content — PHI channel open")
	}
	if env.Metadata["case_id"] != "abcd1234" {
		t.Error("envelope missing opaque case token")
	}
	if env.To != "diagnostician" || env.Type != TypeWorkup {
		t.Errorf("envelope routing wrong: %+v", env)
	}
}

func TestDenyAgentPolicyTargetsOneAgent(t *testing.T) {
	pol := DenyAgent("care_plan")
	ctx := context.Background()

	if res, _ := pol.Evaluate(ctx, mustEnvelope(t, "care_plan", TypeDiagnosisResult)); res.Decision != governance.DecisionDeny {
		t.Error("DenyAgent did not deny its target")
	}
	if res, _ := pol.Evaluate(ctx, mustEnvelope(t, "intake", TypePatientCase)); res.Decision != governance.DecisionAllow {
		t.Error("DenyAgent denied a non-target agent")
	}
}

func mustEnvelope(t *testing.T, agentID, msgType string) protocol.Message {
	t.Helper()
	env, err := buildEnvelope(agentID, msgType, "tok")
	if err != nil {
		t.Fatal(err)
	}
	return env
}
