package audit

import (
	"path/filepath"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/governance"
	"github.com/PratikDhanave/bodh/pkg/protocol"
)

func TestInMemoryRecord(t *testing.T) {
	r := NewInMemory()
	msg := protocol.NewMessage("user", "intake", protocol.RoleUser, "presentation", "{}",
		map[string]any{"case_id": "c1", "patient_id": "p1", "tenant_id": "t1", "trace_id": "tr1"})
	if err := r.Record(FromMessage("intake", msg)); err != nil {
		t.Fatal(err)
	}
	if err := r.Record(FromPolicyDecision(msg, governance.PolicyResult{
		Decision: governance.DecisionDeny, Reason: "missing case_id",
	})); err != nil {
		t.Fatal(err)
	}
	events := r.List()
	if len(events) != 2 {
		t.Fatalf("want 2 events, got %d", len(events))
	}
	if events[0].Kind != KindMessage || events[0].PatientID != "p1" {
		t.Errorf("message event wrong: %+v", events[0])
	}
	if events[1].Kind != KindPolicyDecision || events[1].Decision != "deny" {
		t.Errorf("policy event wrong: %+v", events[1])
	}
}

func TestFileStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	s, err := OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for i := 0; i < 3; i++ {
		if err := s.Record(Event{Kind: KindMessage, AgentID: "intake", PatientID: "p1"}); err != nil {
			t.Fatal(err)
		}
	}
	got := s.List()
	if len(got) != 3 {
		t.Fatalf("want 3 records persisted, got %d", len(got))
	}
	for i, e := range got {
		if e.ID == "" {
			t.Errorf("record %d missing ID", i)
		}
		if e.At.IsZero() {
			t.Errorf("record %d missing At", i)
		}
	}
}

func TestFromReviewDecision(t *testing.T) {
	e := FromReviewDecision("rev-1", "md-7", "approve", "looks good", "p1", "c1")
	if e.Kind != KindReviewDecision || e.MessageID != "rev-1" || e.ReviewerID != "md-7" {
		t.Errorf("review event wrong: %+v", e)
	}
}
