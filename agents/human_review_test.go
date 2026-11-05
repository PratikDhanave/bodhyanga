package agents

import (
	"context"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
	"github.com/PratikDhanave/bodhyanga/pkg/hitl"
	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

func envelopedMsg(payload string) agent.Message {
	return agent.NewMessage("cdm_planner", "human_review", agent.RoleAgent,
		"review_request", payload, map[string]any{
			"case_id":         "c1",
			"patient_id":      "p1",
			"hitl_kind":       "care_plan_review",
			"hitl_next_to":    "engagement",
			"hitl_next_type":  "care_plan_ready",
			"hitl_priority":   "high",
			"hitl_subject":    "Approve plan",
			"hitl_sla_minutes": 60.0,
		})
}

func TestHumanReviewQueuesPending(t *testing.T) {
	q := hitl.NewQueue()
	a := &HumanReviewAgent{Queue: q}
	out, err := a.HandleMessage(context.Background(), envelopedMsg(`{"plan":"x"}`), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage err: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("gate must emit no follow-ups while pending, got %d", len(out))
	}
	pending := q.Pending()
	if len(pending) != 1 {
		t.Fatalf("want 1 pending, got %d", len(pending))
	}
	r := pending[0]
	if r.Kind != "care_plan_review" || r.NextTo != "engagement" || r.NextType != "care_plan_ready" {
		t.Errorf("envelope not parsed: %+v", r)
	}
	if r.PatientID != "p1" || r.CaseID != "c1" {
		t.Errorf("correlators not lifted: %+v", r)
	}
	// Envelope keys must have been stripped from forwardable meta.
	for _, k := range []string{"hitl_kind", "hitl_next_to", "hitl_next_type",
		"hitl_priority", "hitl_subject", "hitl_sla_minutes"} {
		if _, ok := r.Meta[k]; ok {
			t.Errorf("envelope key %s leaked into forward meta", k)
		}
	}
}

func TestHumanReviewApproveResumes(t *testing.T) {
	q := hitl.NewQueue()
	a := &HumanReviewAgent{Queue: q}
	_, _ = a.HandleMessage(context.Background(), envelopedMsg(`{"plan":"x"}`), newFakeEnv())
	r := q.Pending()[0]

	updated, resume, ok := a.ApplyDecision(r.ID, "md-7", "approve", "looks good", "")
	if !ok || updated.Status != medical.ReviewApproved {
		t.Fatalf("apply approve failed: ok=%v status=%s", ok, updated.Status)
	}
	if len(resume) != 1 {
		t.Fatalf("want 1 resume message, got %d", len(resume))
	}
	m := resume[0]
	if m.To != "engagement" || m.Type != "care_plan_ready" {
		t.Errorf("resume routing wrong: %s/%s", m.To, m.Type)
	}
	if id, _ := m.Metadata["hitl_review_id"].(string); id != r.ID {
		t.Errorf("resume should carry hitl_review_id audit breadcrumb")
	}
	if rid, _ := m.Metadata["hitl_reviewer_id"].(string); rid != "md-7" {
		t.Errorf("resume should carry hitl_reviewer_id: %v", m.Metadata["hitl_reviewer_id"])
	}
}

func TestHumanReviewRejectEmitsNothing(t *testing.T) {
	q := hitl.NewQueue()
	a := &HumanReviewAgent{Queue: q}
	_, _ = a.HandleMessage(context.Background(), envelopedMsg(`{"plan":"x"}`), newFakeEnv())
	r := q.Pending()[0]

	updated, resume, ok := a.ApplyDecision(r.ID, "md-7", "reject", "bad fit", "")
	if !ok || updated.Status != medical.ReviewRejected {
		t.Fatalf("apply reject failed: ok=%v status=%s", ok, updated.Status)
	}
	if len(resume) != 0 {
		t.Errorf("reject must not republish; got %d messages", len(resume))
	}
}

func TestHumanReviewMissingEnvelopeDrops(t *testing.T) {
	q := hitl.NewQueue()
	a := &HumanReviewAgent{Queue: q}
	msg := agent.NewMessage("upstream", "human_review", agent.RoleAgent,
		"review_request", `{}`, map[string]any{"case_id": "c1"})
	out, _ := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if len(out) != 0 {
		t.Errorf("expected drop on missing envelope, got %d", len(out))
	}
	if len(q.Pending()) != 0 {
		t.Errorf("expected nothing queued on missing envelope")
	}
}
