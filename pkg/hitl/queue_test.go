package hitl

import (
	"testing"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

func TestQueueLifecycle(t *testing.T) {
	q := NewQueue()
	a := q.Submit(medical.ReviewRequest{Kind: "care_plan_review", PatientID: "p1", NextTo: "engagement", NextType: "care_plan_ready", Payload: "{}"})
	b := q.Submit(medical.ReviewRequest{Kind: "diagnosis_review", PatientID: "p2", NextTo: "diagnostic_supervisor", NextType: "diagnosis", Payload: "{}"})
	if a.Status != medical.ReviewPending || b.Status != medical.ReviewPending {
		t.Fatalf("expected both pending, got %s + %s", a.Status, b.Status)
	}
	if pending := q.Pending(); len(pending) != 2 {
		t.Errorf("want 2 pending, got %d", len(pending))
	}

	got, ok := q.Decide(a.ID, "md-1", "approve", "looks good", "")
	if !ok || got.Status != medical.ReviewApproved {
		t.Fatalf("approve failed: ok=%v status=%s", ok, got.Status)
	}
	if _, ok := q.Decide(a.ID, "md-1", "approve", "", ""); ok {
		t.Errorf("re-deciding an already-decided request should fail")
	}
	if _, ok := q.Decide(b.ID, "md-1", "weird", "", ""); ok {
		t.Errorf("invalid decision should fail")
	}
	if _, ok := q.Decide(b.ID, "md-1", "reject", "wrong patient", ""); !ok {
		t.Errorf("reject failed")
	}
	if pending := q.Pending(); len(pending) != 0 {
		t.Errorf("expected 0 pending after both decided, got %d", len(pending))
	}

	byStatus := q.CountByStatus()
	if byStatus[medical.ReviewApproved] != 1 || byStatus[medical.ReviewRejected] != 1 {
		t.Errorf("status histogram wrong: %+v", byStatus)
	}
	byKind := q.CountByKind()
	if byKind["care_plan_review"] != 1 || byKind["diagnosis_review"] != 1 {
		t.Errorf("kind histogram wrong: %+v", byKind)
	}
}

func TestQueueExpire(t *testing.T) {
	q := NewQueue()
	now := time.Now()
	q.Submit(medical.ReviewRequest{Kind: "k1", NextTo: "x", NextType: "t", Payload: "{}", SLAAt: now.Add(-time.Minute)})
	q.Submit(medical.ReviewRequest{Kind: "k2", NextTo: "x", NextType: "t", Payload: "{}", SLAAt: now.Add(time.Hour)})
	q.Submit(medical.ReviewRequest{Kind: "k3", NextTo: "x", NextType: "t", Payload: "{}"}) // no SLA
	n := q.Expire(now)
	if n != 1 {
		t.Errorf("want 1 expired, got %d", n)
	}
	if q.CountByStatus()[medical.ReviewExpired] != 1 {
		t.Errorf("expected one expired, got %+v", q.CountByStatus())
	}
}
