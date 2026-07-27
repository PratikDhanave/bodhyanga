package pipeline_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/microsoft/agent-framework-go/workflow"

	"github.com/PratikDhanave/bodh-maf/pkg/admit"
	"github.com/PratikDhanave/bodh-maf/pkg/agents"
	"github.com/PratikDhanave/bodh-maf/pkg/auditbridge"
	"github.com/PratikDhanave/bodh-maf/pkg/pipeline"
)

func reviewRunner(t *testing.T, rec audit.Recorder) *pipeline.Runner {
	t.Helper()
	r, err := pipeline.NewRunner(pipeline.Config{
		Review:   true,
		Boundary: admit.New(admit.AllowAll(), rec, nil),
	}, rec)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func decideWith(dec agents.ReviewDecision) func(*workflow.ExternalRequest) (*workflow.ExternalResponse, error) {
	return func(req *workflow.ExternalRequest) (*workflow.ExternalResponse, error) {
		if _, ok := workflow.PortableValueAs[agents.ReviewRequest](req.Data); !ok {
			return nil, fmt.Errorf("unexpected request payload %v", req.PortInfo.RequestType)
		}
		return req.CreateResponse(dec)
	}
}

var reviewCase = agents.PatientCase{CaseID: "case-acs", Symptoms: []string{"chest pain"}}

func TestReviewPausesWithoutResponder(t *testing.T) {
	rec := audit.NewInMemory()
	out, err := reviewRunner(t, rec).Run(context.Background(), reviewCase, "")
	if err != nil {
		t.Fatal(err)
	}
	if !out.Paused || out.Plan != nil || out.Rejected != nil {
		t.Fatalf("want paused outcome, got %+v", out)
	}
	// The surfaced request must be PHI-safe: hashed token, no raw ID.
	for _, evt := range out.Events {
		if e, ok := evt.(workflow.RequestInfoEvent); ok {
			req, ok := workflow.PortableValueAs[agents.ReviewRequest](e.Request.Data)
			if !ok {
				t.Fatal("request payload is not a ReviewRequest")
			}
			if req.CaseToken != agents.HashCaseID("case-acs") {
				t.Errorf("review request token %q is not the fingerprint", req.CaseToken)
			}
			return
		}
	}
	t.Fatal("no RequestInfoEvent observed")
}

func TestApprovedReviewProducesPlanWithAnchoredAudit(t *testing.T) {
	rec := audit.NewInMemory()
	r := reviewRunner(t, rec)
	r.Responder = decideWith(agents.ReviewDecision{Approved: true, ReviewerID: "dr-house"})

	out, err := r.Run(context.Background(), reviewCase, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Plan == nil || out.Paused {
		t.Fatalf("approved review did not complete: %+v", out)
	}
	evs := rec.List()
	if err := auditbridge.CheckReviewAnchored(evs); err != nil {
		t.Errorf("invariant 4: %v", err)
	}
	if err := auditbridge.CheckOrderedSubsequence(evs, []auditbridge.Row{
		{Kind: audit.KindReviewDecision, Decision: "approve"},
		{Kind: audit.KindPolicyDecision, Decision: "allow", AgentID: "care_plan"},
	}); err != nil {
		t.Errorf("approval must precede care_plan admission: %v", err)
	}
}

func TestRejectedReviewEndsWithoutPlan(t *testing.T) {
	rec := audit.NewInMemory()
	r := reviewRunner(t, rec)
	r.Responder = decideWith(agents.ReviewDecision{Approved: false, ReviewerID: "dr-house", Note: "insufficient evidence"})

	out, err := r.Run(context.Background(), reviewCase, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Plan != nil || out.Rejected == nil {
		t.Fatalf("rejected review outcome wrong: %+v", out)
	}
	if out.Rejected.ReviewerID != "dr-house" {
		t.Errorf("rejection not attributed: %+v", out.Rejected)
	}
	if err := auditbridge.CheckOrderedSubsequence(rec.List(), []auditbridge.Row{
		{Kind: audit.KindReviewDecision, Decision: "reject"},
	}); err != nil {
		t.Errorf("reject row missing: %v", err)
	}
}

func TestAnonymousApprovalIsStaleThenRetried(t *testing.T) {
	rec := audit.NewInMemory()
	r := reviewRunner(t, rec)
	attempt := 0
	r.Responder = func(req *workflow.ExternalRequest) (*workflow.ExternalResponse, error) {
		attempt++
		if attempt == 1 {
			return req.CreateResponse(agents.ReviewDecision{Approved: true}) // anonymous
		}
		return req.CreateResponse(agents.ReviewDecision{Approved: true, ReviewerID: "dr-house"})
	}

	out, err := r.Run(context.Background(), reviewCase, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Plan == nil {
		t.Fatalf("valid second approval did not complete the case: %+v", out)
	}
	if attempt != 2 {
		t.Fatalf("responder attempts = %d, want 2 (anonymous discarded, re-asked)", attempt)
	}
	if err := auditbridge.CheckOrderedSubsequence(rec.List(), []auditbridge.Row{
		{Kind: audit.KindReviewDecision, Decision: "reject", Type: ""}, // stale_response
		{Kind: audit.KindReviewDecision, Decision: "approve"},
	}); err != nil {
		t.Errorf("stale-then-approve rows missing: %v", err)
	}
	if err := auditbridge.CheckReviewAnchored(rec.List()); err != nil {
		t.Errorf("invariant 4: %v", err)
	}
}
