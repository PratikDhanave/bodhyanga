package governance

import (
	"context"
	"sync"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/protocol"
)

// stubBreakGlassRecorder is a thread-safe in-process recorder used in tests.
// It implements BreakGlassRecorder without importing pkg/audit, avoiding the
// import cycle (pkg/audit → pkg/governance → pkg/audit).
type stubBreakGlassRecorder struct {
	mu     sync.Mutex
	events []BreakGlassEvent
	// errOnRecord, when non-nil, causes RecordBreakGlass to return that error.
	errOnRecord error
}

func (s *stubBreakGlassRecorder) RecordBreakGlass(e BreakGlassEvent) error {
	if s.errOnRecord != nil {
		return s.errOnRecord
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
	return nil
}

func (s *stubBreakGlassRecorder) list() []BreakGlassEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]BreakGlassEvent, len(s.events))
	copy(out, s.events)
	return out
}

// newBGPolicy is a test helper that wires a BreakGlassPolicy with an in-process
// stub recorder and returns both so tests can inspect emitted events.
func newBGPolicy() (BreakGlassPolicy, *stubBreakGlassRecorder) {
	rec := &stubBreakGlassRecorder{}
	return BreakGlassPolicy{Recorder: rec}, rec
}

// msgWith builds a minimal protocol.Message with the given metadata map.
func msgWith(metadata map[string]any) protocol.Message {
	return protocol.Message{
		ID:       "test-msg-id",
		From:     "gateway",
		To:       "intake",
		Type:     "presentation",
		Content:  "chief complaint",
		Metadata: metadata,
	}
}

// TestBreakGlass_PassthroughNormalMessages verifies that messages without
// break_glass=true in metadata pass through with an allow decision and
// that no audit event is recorded.
func TestBreakGlass_PassthroughNormalMessages(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		metadata map[string]any
	}{
		{name: "nil metadata", metadata: nil},
		{name: "empty metadata", metadata: map[string]any{}},
		{name: "break_glass false string", metadata: map[string]any{"break_glass": "false"}},
		{name: "break_glass bool true (not string)", metadata: map[string]any{"break_glass": true}},
		{name: "unrelated metadata", metadata: map[string]any{"case_id": "case-1", "tenant_id": "t1"}},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pol, rec := newBGPolicy()
			res, err := pol.Evaluate(context.Background(), msgWith(tc.metadata))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Decision != DecisionAllow {
				t.Errorf("expected allow, got %q (reason=%q)", res.Decision, res.Reason)
			}
			if events := rec.list(); len(events) != 0 {
				t.Errorf("expected 0 audit events for normal message, got %d", len(events))
			}
		})
	}
}

// TestBreakGlass_AllowsValidBreakGlass verifies that a message with
// break_glass=true, a non-empty reason, and a non-empty clinician ID
// is allowed and that a BreakGlassEvent is emitted to the recorder.
func TestBreakGlass_AllowsValidBreakGlass(t *testing.T) {
	t.Parallel()
	pol, rec := newBGPolicy()

	msg := msgWith(map[string]any{
		"break_glass":           "true",
		"break_glass_reason":    "ED trauma — patient unresponsive, no panel relationship",
		"break_glass_clinician": "dr-jones-npi-1234567890",
		"case_id":               "case-emergency-42",
		"patient_id":            "pt-trauma-7",
		"tenant_id":             "tenant-ed",
		"trace_id":              "trace-abc",
	})

	res, err := pol.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Errorf("expected allow, got %q (reason=%q)", res.Decision, res.Reason)
	}

	events := rec.list()
	if len(events) != 1 {
		t.Fatalf("expected 1 break-glass event, got %d", len(events))
	}
	e := events[0]
	if e.MessageID != msg.ID {
		t.Errorf("expected MessageID %q, got %q", msg.ID, e.MessageID)
	}
	if e.CaseID != "case-emergency-42" {
		t.Errorf("expected CaseID %q, got %q", "case-emergency-42", e.CaseID)
	}
	if e.PatientID != "pt-trauma-7" {
		t.Errorf("expected PatientID %q, got %q", "pt-trauma-7", e.PatientID)
	}
	if e.TenantID != "tenant-ed" {
		t.Errorf("expected TenantID %q, got %q", "tenant-ed", e.TenantID)
	}
	if e.TraceID != "trace-abc" {
		t.Errorf("expected TraceID %q, got %q", "trace-abc", e.TraceID)
	}
	if e.Clinician != "dr-jones-npi-1234567890" {
		t.Errorf("expected Clinician %q, got %q", "dr-jones-npi-1234567890", e.Clinician)
	}
}

// TestBreakGlass_DeniesBreakGlassWithoutReason verifies that a break-glass
// request missing break_glass_reason is denied with the canonical reason string.
func TestBreakGlass_DeniesBreakGlassWithoutReason(t *testing.T) {
	t.Parallel()
	pol, rec := newBGPolicy()

	msg := msgWith(map[string]any{
		"break_glass": "true",
		// break_glass_reason intentionally absent
		"break_glass_clinician": "dr-jones-npi-1234567890",
	})

	res, err := pol.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny, got %q (reason=%q)", res.Decision, res.Reason)
	}
	const wantReason = "break_glass requires break_glass_reason and break_glass_clinician metadata"
	if res.Reason != wantReason {
		t.Errorf("reason mismatch\n  got:  %q\n  want: %q", res.Reason, wantReason)
	}
	// No audit event should be recorded for a denied break-glass attempt.
	if events := rec.list(); len(events) != 0 {
		t.Errorf("expected 0 audit events for denied break-glass, got %d", len(events))
	}
}

// TestBreakGlass_DeniesBreakGlassWithoutClinician verifies that a break-glass
// request missing break_glass_clinician is denied with the canonical reason
// string.
func TestBreakGlass_DeniesBreakGlassWithoutClinician(t *testing.T) {
	t.Parallel()
	pol, rec := newBGPolicy()

	msg := msgWith(map[string]any{
		"break_glass":        "true",
		"break_glass_reason": "ED trauma — patient unresponsive",
		// break_glass_clinician intentionally absent
	})

	res, err := pol.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionDeny {
		t.Errorf("expected deny, got %q (reason=%q)", res.Decision, res.Reason)
	}
	const wantReason = "break_glass requires break_glass_reason and break_glass_clinician metadata"
	if res.Reason != wantReason {
		t.Errorf("reason mismatch\n  got:  %q\n  want: %q", res.Reason, wantReason)
	}
	if events := rec.list(); len(events) != 0 {
		t.Errorf("expected 0 audit events for denied break-glass, got %d", len(events))
	}
}

// TestBreakGlass_AuditEventEmitted is an explicit contract test that verifies:
//  1. Exactly one BreakGlassEvent is recorded for a valid break-glass.
//  2. The event is populated with the correct routing and correlation fields.
//  3. The Reason contains the clinician identifier for post-hoc review.
//  4. The At timestamp is non-zero (set by the policy before calling RecordBreakGlass).
func TestBreakGlass_AuditEventEmitted(t *testing.T) {
	t.Parallel()
	pol, rec := newBGPolicy()

	clinicianID := "dr-smith-npi-9876543210"
	msg := msgWith(map[string]any{
		"break_glass":           "true",
		"break_glass_reason":    "trauma activation — no panel relationship",
		"break_glass_clinician": clinicianID,
		"case_id":               "case-bg-test",
	})

	res, err := pol.Evaluate(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != DecisionAllow {
		t.Fatalf("expected allow decision, got %q", res.Decision)
	}

	events := rec.list()
	if len(events) != 1 {
		t.Fatalf("expected exactly 1 break-glass event, got %d", len(events))
	}
	e := events[0]

	// MessageID must reference the triggering message.
	if e.MessageID != msg.ID {
		t.Errorf("MessageID: got %q, want %q", e.MessageID, msg.ID)
	}

	// Clinician must be set so the post-hoc reviewer knows who triggered the override.
	if e.Clinician != clinicianID {
		t.Errorf("Clinician: got %q, want %q", e.Clinician, clinicianID)
	}

	// At must be non-zero — set by the policy before calling RecordBreakGlass.
	if e.At.IsZero() {
		t.Error("BreakGlassEvent.At must not be zero")
	}

	// Reason must contain the clinician ID so audit log readers can identify
	// the accessor without needing to join another table.
	if e.Reason == "" {
		t.Error("BreakGlassEvent.Reason must not be empty")
	}
	found := false
	for i := 0; i <= len(e.Reason)-len(clinicianID); i++ {
		if e.Reason[i:i+len(clinicianID)] == clinicianID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected Reason to contain clinician ID %q, got %q", clinicianID, e.Reason)
	}

	// CaseID must be propagated from metadata for SIEM join-ability.
	if e.CaseID != "case-bg-test" {
		t.Errorf("CaseID: got %q, want %q", e.CaseID, "case-bg-test")
	}
}
