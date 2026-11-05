package orchestration

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/audit"
)

// TestOrchestrator_RecordsMessageEventOnAgentInvocation asserts that a
// successful agent hop produces exactly one KindMessage event with the
// tenant/case/trace correlators copied from msg.Metadata. msg.Content
// (PHI) must NEVER appear in the recorded event.
func TestOrchestrator_RecordsMessageEventOnAgentInvocation(t *testing.T) {
	worker := &stubAgent{
		id:    "worker",
		name:  "worker",
		inbox: make(chan agent.Message, 1),
	}
	o, bus, _ := buildOrchestrator(t, allowAll{}, worker)
	rec := audit.NewInMemory()
	o.SetAuditRecorder(rec)
	o.Start(context.Background())

	bus.Publish(context.Background(), agent.NewMessage(
		"user", "worker", agent.RoleUser, "task", "PHI-PHI-PHI",
		map[string]any{
			"tenant_id":  "t-acme",
			"case_id":    "c-42",
			"patient_id": "p-99",
			"trace_id":   "tr-xyz",
		}))

	if _, ok := waitFor(worker.inbox, 500*time.Millisecond); !ok {
		t.Fatal("worker never received message")
	}

	// One KindMessage event is expected. allowAll is non-clinical so the
	// allow decision is NOT recorded (signal-rich audit log).
	events := waitForEvents(t, rec, 1, 500*time.Millisecond)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %+v", len(events), events)
	}
	e := events[0]
	if e.Kind != audit.KindMessage {
		t.Errorf("kind: got %s, want %s", e.Kind, audit.KindMessage)
	}
	if e.AgentID != "worker" || e.From != "user" || e.To != "worker" || e.Type != "task" {
		t.Errorf("routing fields wrong: %+v", e)
	}
	if e.TenantID != "t-acme" || e.CaseID != "c-42" || e.PatientID != "p-99" || e.TraceID != "tr-xyz" {
		t.Errorf("correlators not copied: %+v", e)
	}
	// Critical: no PHI may leak into the event. The audit struct has no
	// Content field; reflect-style assertion just defends against future
	// drift via the JSON representation.
	for _, field := range []string{e.Reason, e.ErrorText, e.Decision} {
		if strings.Contains(field, "PHI-PHI-PHI") {
			t.Errorf("PHI leaked into event field: %+v", e)
		}
	}
}

// TestOrchestrator_RecordsPolicyDenyEvent asserts that a deny decision
// produces one KindPolicyDecision event and zero KindMessage events
// (agent is short-circuited).
func TestOrchestrator_RecordsPolicyDenyEvent(t *testing.T) {
	worker := &stubAgent{
		id:    "worker",
		name:  "worker",
		inbox: make(chan agent.Message, 1),
	}
	o, bus, _ := buildOrchestrator(t, denyByType{deny: "blocked"}, worker)
	rec := audit.NewInMemory()
	o.SetAuditRecorder(rec)
	o.Start(context.Background())

	bus.Publish(context.Background(), agent.NewMessage(
		"user", "worker", agent.RoleUser, "blocked", "noop",
		map[string]any{"tenant_id": "t-1", "case_id": "c-1"}))

	events := waitForEvents(t, rec, 1, 500*time.Millisecond)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d: %+v", len(events), events)
	}
	e := events[0]
	if e.Kind != audit.KindPolicyDecision || e.Decision != "deny" {
		t.Errorf("expected deny policy event, got %+v", e)
	}
	if e.TenantID != "t-1" || e.CaseID != "c-1" {
		t.Errorf("correlators not copied: %+v", e)
	}
}

// TestOrchestrator_RecordsAgentErrorEvent asserts a handler error produces
// a KindAgentError event whose ErrorText is the err.Error() string.
func TestOrchestrator_RecordsAgentErrorEvent(t *testing.T) {
	worker := &stubAgent{
		id:   "worker",
		name: "worker",
		handler: func(_ context.Context, _ agent.Message, _ agent.Environment) ([]agent.Message, error) {
			return nil, errors.New("synthetic")
		},
	}
	o, bus, _ := buildOrchestrator(t, allowAll{}, worker)
	rec := audit.NewInMemory()
	o.SetAuditRecorder(rec)
	o.Start(context.Background())

	bus.Publish(context.Background(), agent.NewMessage(
		"user", "worker", agent.RoleUser, "task", "irrelevant",
		map[string]any{"tenant_id": "t-1", "case_id": "c-1"}))

	events := waitForEvents(t, rec, 2, 500*time.Millisecond)
	// Expect: KindMessage (handler invoked) + KindAgentError (handler failed).
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d: %+v", len(events), events)
	}
	if events[0].Kind != audit.KindMessage {
		t.Errorf("first event kind: got %s, want %s", events[0].Kind, audit.KindMessage)
	}
	if events[1].Kind != audit.KindAgentError {
		t.Errorf("second event kind: got %s, want %s", events[1].Kind, audit.KindAgentError)
	}
	if events[1].ErrorText != "synthetic" {
		t.Errorf("error text: got %q, want %q", events[1].ErrorText, "synthetic")
	}
	if events[1].CaseID != "c-1" || events[1].TenantID != "t-1" {
		t.Errorf("error event correlators wrong: %+v", events[1])
	}
}

// TestOrchestrator_RecordsClinicalAllowOnly asserts that an allow decision
// for a clinical message type (case_state) is recorded, while an allow
// decision for a non-clinical type is NOT (signal-rich audit log).
func TestOrchestrator_RecordsClinicalAllowOnly(t *testing.T) {
	clinical := &stubAgent{id: "clinical", name: "clinical", inbox: make(chan agent.Message, 1)}
	other := &stubAgent{id: "other", name: "other", inbox: make(chan agent.Message, 1)}
	o, bus, _ := buildOrchestrator(t, allowAll{}, clinical, other)
	rec := audit.NewInMemory()
	o.SetAuditRecorder(rec)
	o.Start(context.Background())

	// Clinical type → produces policy-allow + message events (2).
	bus.Publish(context.Background(), agent.NewMessage(
		"user", "clinical", agent.RoleUser, "case_state", "{}",
		map[string]any{"case_id": "c-1"}))
	// Non-clinical type → produces only the message event (1).
	bus.Publish(context.Background(), agent.NewMessage(
		"user", "other", agent.RoleUser, "task", "noop", nil))

	_, _ = waitFor(clinical.inbox, 500*time.Millisecond)
	_, _ = waitFor(other.inbox, 500*time.Millisecond)
	events := waitForEvents(t, rec, 3, 500*time.Millisecond)

	allowCount := 0
	for _, e := range events {
		if e.Kind == audit.KindPolicyDecision && e.Decision == "allow" {
			allowCount++
			if e.Type != "case_state" {
				t.Errorf("only clinical allows should be recorded; got allow for %s", e.Type)
			}
		}
	}
	if allowCount != 1 {
		t.Errorf("expected exactly 1 clinical allow recorded, got %d (events=%+v)", allowCount, events)
	}
}

// TestOrchestrator_NilRecorderIsZeroOverhead asserts that the orchestrator
// works fine when no recorder is configured (backward-compat for cmd/demo).
func TestOrchestrator_NilRecorderIsZeroOverhead(t *testing.T) {
	worker := &stubAgent{id: "worker", name: "worker", inbox: make(chan agent.Message, 1)}
	o, bus, _ := buildOrchestrator(t, allowAll{}, worker)
	// Note: no SetAuditRecorder call.
	o.Start(context.Background())

	bus.Publish(context.Background(), agent.NewMessage(
		"user", "worker", agent.RoleUser, "task", "noop", nil))
	if _, ok := waitFor(worker.inbox, 500*time.Millisecond); !ok {
		t.Fatal("nil recorder must not block message delivery")
	}
}

// TestOrchestrator_RecorderErrorIsFailOpen asserts that a Record() error
// does NOT prevent the agent from being invoked. Audit is operational;
// blocking the clinical pipeline because the sink is down is the wrong
// trade-off.
func TestOrchestrator_RecorderErrorIsFailOpen(t *testing.T) {
	worker := &stubAgent{id: "worker", name: "worker", inbox: make(chan agent.Message, 1)}
	o, bus, _ := buildOrchestrator(t, allowAll{}, worker)
	o.SetAuditRecorder(&erroringRecorder{})
	o.Start(context.Background())

	bus.Publish(context.Background(), agent.NewMessage(
		"user", "worker", agent.RoleUser, "task", "noop", nil))
	if _, ok := waitFor(worker.inbox, 500*time.Millisecond); !ok {
		t.Fatal("recorder errors must not block message delivery")
	}
}

// erroringRecorder returns an error on every Record. The orchestrator must
// log and continue, never block.
type erroringRecorder struct{}

func (erroringRecorder) Record(audit.Event) error { return errors.New("sink down") }
func (erroringRecorder) List() []audit.Event      { return nil }

// waitForEvents polls until the recorder holds at least want events or the
// deadline elapses. Events are recorded from the bus delivery goroutine, so
// a sleep-free read can race with delivery.
func waitForEvents(t *testing.T, rec audit.Recorder, want int, d time.Duration) []audit.Event {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		ev := rec.List()
		if len(ev) >= want {
			return ev
		}
		time.Sleep(5 * time.Millisecond)
	}
	return rec.List()
}
