package hitl

import (
	"testing"

	"github.com/PratikDhanave/bodh/pkg/medical"
	"github.com/PratikDhanave/bodh/pkg/protocol"
)

func baseReview() medical.ReviewRequest {
	return medical.ReviewRequest{
		ID:       "rev-1",
		NextTo:   "diagnostician",
		NextType: "case_state",
		Payload:  `{"original":true}`,
		Meta:     map[string]any{"case_id": "abc12345", "tenant_id": "t1"},
	}
}

func TestBuildResumeMessage_Approve(t *testing.T) {
	r := baseReview()
	r.Status = medical.ReviewApproved

	msg, ok := BuildResumeMessage(r, "dr-foo", "")
	if !ok {
		t.Fatalf("approved review should resume; got ok=false")
	}
	if msg.From != "human_review" {
		t.Errorf("From = %q, want human_review (honest provenance)", msg.From)
	}
	if msg.To != "diagnostician" || msg.Type != "case_state" {
		t.Errorf("routing = (%q,%q), want (diagnostician, case_state)", msg.To, msg.Type)
	}
	if msg.Role != protocol.RoleAgent {
		t.Errorf("Role = %q, want %q", msg.Role, protocol.RoleAgent)
	}
	if msg.Content != `{"original":true}` {
		t.Errorf("Content = %q, want original payload", msg.Content)
	}
	if msg.Metadata["hitl_review_id"] != "rev-1" || msg.Metadata["hitl_reviewer_id"] != "dr-foo" {
		t.Errorf("missing HITL breadcrumb metadata: %v", msg.Metadata)
	}
	if msg.Metadata["case_id"] != "abc12345" {
		t.Errorf("upstream metadata not preserved: %v", msg.Metadata)
	}
	if _, present := msg.Metadata["hitl_kind"]; present {
		t.Errorf("hitl_kind must not leak onto the resumed message")
	}
}

func TestBuildResumeMessage_ModifiedUsesEdit(t *testing.T) {
	r := baseReview()
	r.Status = medical.ReviewModified

	msg, ok := BuildResumeMessage(r, "dr-bar", `{"edited":true}`)
	if !ok {
		t.Fatalf("modified review should resume; got ok=false")
	}
	if msg.Content != `{"edited":true}` {
		t.Errorf("Content = %q, want the reviewer's edit", msg.Content)
	}
}

func TestBuildResumeMessage_ModifiedEmptyEditFallsBack(t *testing.T) {
	r := baseReview()
	r.Status = medical.ReviewModified

	msg, ok := BuildResumeMessage(r, "dr-bar", "")
	if !ok {
		t.Fatalf("modified review should resume; got ok=false")
	}
	if msg.Content != `{"original":true}` {
		t.Errorf("empty modification should fall back to original payload; got %q", msg.Content)
	}
}

func TestBuildResumeMessage_NonApprovingDoesNotResume(t *testing.T) {
	for _, st := range []medical.ReviewStatus{
		medical.ReviewRejected, medical.ReviewPending, medical.ReviewExpired,
	} {
		r := baseReview()
		r.Status = st
		if _, ok := BuildResumeMessage(r, "dr-foo", ""); ok {
			t.Errorf("status %q must not resume the pipeline", st)
		}
	}
}

func TestBuildResumeMessage_DoesNotMutateSourceMeta(t *testing.T) {
	r := baseReview()
	r.Status = medical.ReviewApproved

	_, _ = BuildResumeMessage(r, "dr-foo", "")
	if _, leaked := r.Meta["hitl_review_id"]; leaked {
		t.Errorf("BuildResumeMessage must not mutate the source review's Meta map")
	}
}
