package pipeline_test

import (
	"context"
	"errors"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/audit"

	"github.com/PratikDhanave/bodhyanga-maf/pkg/admit"
	"github.com/PratikDhanave/bodhyanga-maf/pkg/agents"
	"github.com/PratikDhanave/bodhyanga-maf/pkg/auditbridge"
	"github.com/PratikDhanave/bodhyanga-maf/pkg/pipeline"
)

func canonicalRun(t *testing.T, rec audit.Recorder) *pipeline.Outcome {
	t.Helper()
	r, err := pipeline.NewRunner(pipeline.Config{
		Boundary: admit.New(admit.AllowAll(), rec, nil),
	}, rec)
	if err != nil {
		t.Fatalf("runner: %v", err)
	}
	out, err := r.Run(context.Background(), agents.PatientCase{
		CaseID:   "case-acs",
		Symptoms: []string{"chest pain"},
	}, "")
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	return out
}

// TestCanonicalTrailMatchesContract asserts the ordered trail from
// contracts/audit-trail.md for the canonical happy-path case (1 loop-back).
func TestCanonicalTrailMatchesContract(t *testing.T) {
	rec := audit.NewInMemory()
	out := canonicalRun(t, rec)
	if out.Plan == nil {
		t.Fatal("canonical case did not complete")
	}

	// MAF's super-step (BSP) execution batches stream events: admission rows
	// are synchronous (real time) while the bridge's execution rows arrive a
	// super-step later. Causal order is therefore asserted over the
	// admission rows; execution rows are asserted per hop (invariant 1) and
	// by count. See contracts/audit-trail.md § BSP ordering.
	allow := func(agent, typ string) auditbridge.Row {
		return auditbridge.Row{Kind: audit.KindPolicyDecision, Decision: "allow", AgentID: agent, Type: typ}
	}
	expectedAdmissions := []auditbridge.Row{
		allow("intake", admit.TypePatientCase),
		allow("test_planner", admit.TypeDiagnosticQuery),
		allow("diagnostician", admit.TypeWorkup),
		allow("test_planner", admit.TypeDiagnosticQuery), // loop-back
		allow("diagnostician", admit.TypeWorkup),
		allow("care_plan", admit.TypeDiagnosisResult),
	}

	evs := rec.List()
	if err := auditbridge.CheckOrderedSubsequence(evs, expectedAdmissions); err != nil {
		t.Errorf("admission causal order: %v", err)
	}
	// Execution-row counts: one per admitted hop, plus exactly one finalize.
	wantExec := map[string]int{
		"intake/patient_case": 1, "test_planner/diagnostic_query": 2,
		"diagnostician/workup": 2, "care_plan/diagnosis_result": 1,
	}
	gotExec := map[string]int{}
	finalized := 0
	for _, e := range evs {
		if e.Kind != audit.KindMessage {
			continue
		}
		if e.Type == "case_finalized" {
			finalized++
			continue
		}
		if e.AgentID != "" {
			gotExec[e.AgentID+"/"+e.Type]++
		}
	}
	for k, want := range wantExec {
		if gotExec[k] != want {
			t.Errorf("execution rows for %s = %d, want %d", k, gotExec[k], want)
		}
	}
	if finalized != 1 {
		t.Errorf("case_finalized rows = %d, want 1", finalized)
	}
	if err := auditbridge.CheckHopCompleteness(evs); err != nil {
		t.Errorf("invariant 1: %v", err)
	}
	if err := auditbridge.CheckDenyTerminal(evs); err != nil {
		t.Errorf("invariant 2: %v", err)
	}
	if err := auditbridge.CheckReviewAnchored(evs); err != nil {
		t.Errorf("invariant 4: %v", err)
	}
}

// TestTrailIsPHIFree scans every field of every event for the raw case ID,
// symptom text, diagnosis text, and care-plan content (invariant 3).
func TestTrailIsPHIFree(t *testing.T) {
	rec := audit.NewInMemory()
	out := canonicalRun(t, rec)

	forbidden := []string{"case-acs", "chest pain", "hypertension",
		"acute coronary syndrome", "aspirin", "troponin"}
	if err := auditbridge.CheckPHIFree(rec.List(), forbidden...); err != nil {
		t.Fatalf("invariant 3: %v", err)
	}
	// The output itself still carries clinical content — the trail is what
	// must be clean, not the care plan.
	if out.Plan == nil || out.Plan.Diagnosis == "" {
		t.Fatal("sanity: plan should exist with a diagnosis")
	}
}

// TestDenyTrailIsCompleteAndTerminal covers the deny-all variant of the
// contract: one deny row, zero execution rows.
func TestDenyTrailIsCompleteAndTerminal(t *testing.T) {
	rec := audit.NewInMemory()
	r, err := pipeline.NewRunner(pipeline.Config{
		Boundary: admit.New(admit.DenyAll(), rec, nil),
	}, rec)
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(context.Background(), agents.PatientCase{CaseID: "x", Symptoms: []string{"fever"}}, "")
	if err != nil {
		t.Fatal(err)
	}
	if !out.Denied || out.Plan != nil {
		t.Fatalf("deny-all outcome wrong: denied=%v plan=%v", out.Denied, out.Plan)
	}
	evs := rec.List()
	if err := auditbridge.CheckDenyTerminal(evs); err != nil {
		t.Errorf("invariant 2: %v", err)
	}
	if err := auditbridge.CheckOrderedSubsequence(evs, []auditbridge.Row{
		{Kind: audit.KindPolicyDecision, Decision: "deny", AgentID: "intake"},
	}); err != nil {
		t.Errorf("deny row missing: %v", err)
	}
	for _, e := range evs {
		if e.Kind == audit.KindMessage && e.AgentID != "" &&
			e.Type != "checkpoint" && e.Type != "case_finalized" {
			t.Fatalf("execution row present under deny-all: %+v", e)
		}
	}
}

// erroringRecorder proves fail-open at the run level: audit down, care flows.
type erroringRecorder struct{}

func (erroringRecorder) Record(audit.Event) error { return errors.New("sink down") }
func (erroringRecorder) List() []audit.Event      { return nil }

func TestAuditSinkDownCareContinues(t *testing.T) {
	rec := erroringRecorder{}
	r, err := pipeline.NewRunner(pipeline.Config{
		Boundary: admit.New(admit.AllowAll(), rec, nil),
	}, rec)
	if err != nil {
		t.Fatal(err)
	}
	out, err := r.Run(context.Background(), agents.PatientCase{
		CaseID: "case-acs", Symptoms: []string{"chest pain"},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Plan == nil {
		t.Fatal("care flow blocked by audit sink failure (fail-open violated)")
	}
}
