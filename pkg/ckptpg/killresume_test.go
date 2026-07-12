package ckptpg_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/microsoft/agent-framework-go/workflow"

	"github.com/PratikDhanave/bodh-maf/pkg/admit"
	"github.com/PratikDhanave/bodh-maf/pkg/agents"
	"github.com/PratikDhanave/bodh-maf/pkg/auditbridge"
	"github.com/PratikDhanave/bodh-maf/pkg/ckptpg"
	"github.com/PratikDhanave/bodh-maf/pkg/pipeline"
)

// workflowInfo builds a CheckpointInfo literal (helper shared with store_test).
func workflowInfo(sessionID, checkpointID string) workflow.CheckpointInfo {
	return workflow.CheckpointInfo{SessionID: sessionID, CheckpointID: checkpointID}
}

// TestKillResume is exit criterion 3 (SC-003): a case paused at physician
// review survives full process death. "Process death" is simulated the
// strongest way available in one test binary: the first runner, its workflow,
// its checkpoint manager, and its audit recorder are all discarded; the
// second runner is built from scratch and knows only the session ID and the
// Postgres DSN — exactly what a restarted deployment would have.
func TestKillResume(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	const session = "sess-killresume"

	newRunner := func(rec audit.Recorder) *pipeline.Runner {
		r, err := pipeline.NewRunner(pipeline.Config{
			Review:   true,
			Boundary: admit.New(admit.AllowAll(), rec, nil),
		}, rec)
		if err != nil {
			t.Fatal(err)
		}
		return r.WithCheckpointStore(ckptpg.New(testPool(t)))
	}

	// ---- Life 1: run until paused at review, then die. ----
	rec1 := audit.NewInMemory()
	out1, err := newRunner(rec1).Run(ctx, agents.PatientCase{
		CaseID: "case-kr", Symptoms: []string{"chest pain"},
	}, session)
	if err != nil {
		t.Fatal(err)
	}
	if !out1.Paused || out1.Plan != nil {
		t.Fatalf("life 1 should pause at review: %+v", out1)
	}
	var firstRequestID string
	for _, evt := range out1.Events {
		if e, ok := evt.(workflow.RequestInfoEvent); ok {
			firstRequestID = e.Request.RequestID
		}
	}
	if firstRequestID == "" {
		t.Fatal("life 1 surfaced no review request")
	}
	infos, err := s.RetrieveIndex(ctx, session, nil)
	if err != nil || len(infos) == 0 {
		t.Fatalf("no durable checkpoints after pause: %v %v", infos, err)
	}

	// ---- Life 2: fresh everything; resume by session ID and approve. ----
	rec2 := audit.NewInMemory()
	r2 := newRunner(rec2)
	var resumedRequestID string
	r2.Responder = func(req *workflow.ExternalRequest) (*workflow.ExternalResponse, error) {
		resumedRequestID = req.RequestID
		if _, ok := workflow.PortableValueAs[agents.ReviewRequest](req.Data); !ok {
			return nil, fmt.Errorf("unexpected request payload")
		}
		return req.CreateResponse(agents.ReviewDecision{Approved: true, ReviewerID: "dr-house"})
	}
	out2, err := r2.Resume(ctx, session)
	if err != nil {
		t.Fatal(err)
	}
	if out2.Plan == nil {
		t.Fatalf("resumed case did not complete: %+v", out2)
	}
	if out2.Plan.Diagnosis != "acute coronary syndrome" {
		t.Errorf("resumed diagnosis = %q", out2.Plan.Diagnosis)
	}
	// The pending request is REPUBLISHED, not re-created (same ID).
	if resumedRequestID != firstRequestID {
		t.Errorf("republished request ID %q != original %q", resumedRequestID, firstRequestID)
	}

	// ---- Trail continuity across both lifetimes (invariant 6). ----
	union := append(rec1.List(), rec2.List()...)
	if err := auditbridge.CheckReviewAnchored(union); err != nil {
		t.Errorf("invariant 4 over union: %v", err)
	}
	if err := auditbridge.CheckOrderedSubsequence(union, []auditbridge.Row{
		{Kind: audit.KindPolicyDecision, Decision: "allow", AgentID: "review_gate"},
		{Kind: audit.KindMessage, AgentID: "physician_review", Type: "review_requested"},
		{Kind: audit.KindReviewDecision, Decision: "approve"},
		{Kind: audit.KindPolicyDecision, Decision: "allow", AgentID: "care_plan"},
	}); err != nil {
		t.Errorf("cross-lifetime trail: %v", err)
	}
	if err := auditbridge.CheckPHIFree(union, "case-kr", "chest pain", "acute coronary syndrome"); err != nil {
		t.Errorf("invariant 3 over union: %v", err)
	}
}

// TestRejectAfterResume: the physician can also reject after a restart.
func TestRejectAfterResume(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	const session = "sess-reject-resume"

	rec1 := audit.NewInMemory()
	r1, err := pipeline.NewRunner(pipeline.Config{
		Review: true, Boundary: admit.New(admit.AllowAll(), rec1, nil),
	}, rec1)
	if err != nil {
		t.Fatal(err)
	}
	r1.WithCheckpointStore(ckptpg.New(testPool(t)))
	if out, err := r1.Run(ctx, agents.PatientCase{CaseID: "case-rr", Symptoms: []string{"fever", "cough"}}, session); err != nil || !out.Paused {
		t.Fatalf("life 1: %v %+v", err, out)
	}
	if infos, err := s.RetrieveIndex(ctx, session, nil); err != nil || len(infos) == 0 {
		t.Fatalf("no checkpoints: %v %v", infos, err)
	}

	rec2 := audit.NewInMemory()
	r2, err := pipeline.NewRunner(pipeline.Config{
		Review: true, Boundary: admit.New(admit.AllowAll(), rec2, nil),
	}, rec2)
	if err != nil {
		t.Fatal(err)
	}
	r2.WithCheckpointStore(ckptpg.New(testPool(t)))
	r2.Responder = func(req *workflow.ExternalRequest) (*workflow.ExternalResponse, error) {
		return req.CreateResponse(agents.ReviewDecision{Approved: false, ReviewerID: "dr-cuddy", Note: "needs imaging"})
	}
	out2, err := r2.Resume(ctx, session)
	if err != nil {
		t.Fatal(err)
	}
	if out2.Plan != nil || out2.Rejected == nil || out2.Rejected.ReviewerID != "dr-cuddy" {
		t.Fatalf("reject-after-resume outcome wrong: %+v", out2)
	}
}
