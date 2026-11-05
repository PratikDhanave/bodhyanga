package pipeline_test

import (
	"errors"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/audit"
	"github.com/PratikDhanave/bodhyanga/pkg/governance"

	"github.com/PratikDhanave/bodhyanga-maf/internal/slicetest"
	"github.com/PratikDhanave/bodhyanga-maf/pkg/admit"
	"github.com/PratikDhanave/bodhyanga-maf/pkg/agents"
	"github.com/PratikDhanave/bodhyanga-maf/pkg/pipeline"
)

func admittedRun(t *testing.T, pol governance.Policy) (slicetest.Result, *audit.InMemory) {
	t.Helper()
	rec := audit.NewInMemory()
	wf, err := pipeline.New(pipeline.Config{Boundary: admit.New(pol, rec, nil)})
	if err != nil {
		t.Fatalf("build workflow: %v", err)
	}
	res := slicetest.Run(t, wf, agents.PatientCase{
		CaseID:   "case-acs",
		Symptoms: []string{"chest pain"},
	})
	return res, rec
}

func countDecisions(evs []audit.Event, decision string) int {
	n := 0
	for _, e := range evs {
		if e.Kind == audit.KindPolicyDecision && e.Decision == decision {
			n++
		}
	}
	return n
}

func TestAllowPolicyCompletesWithPerHopAllowRecords(t *testing.T) {
	res, rec := admittedRun(t, admit.AllowAll())

	if len(res.Failures) != 0 {
		t.Fatalf("unexpected failures under allow-all: %+v", res.Failures)
	}
	if res.Plan == nil || res.Plan.Diagnosis != "acute coronary syndrome" {
		t.Fatalf("allow-all case did not complete correctly: %+v", res.Plan)
	}
	// 6 hops for the canonical case (intake, planner, dx, planner, dx, care_plan).
	if got := countDecisions(rec.List(), string(governance.DecisionAllow)); got != 6 {
		t.Errorf("allow records = %d, want 6 (one per hop)", got)
	}
	// PHI rule: no raw case ID anywhere in the trail.
	for _, e := range rec.List() {
		if e.CaseID == "case-acs" {
			t.Fatalf("raw case ID leaked into audit event: %+v", e)
		}
		if e.CaseID != agents.HashCaseID("case-acs") {
			t.Errorf("event case token %q is not the case fingerprint", e.CaseID)
		}
	}
}

func TestDenyAllBlocksEverythingWithAuditedDenial(t *testing.T) {
	res, rec := admittedRun(t, admit.DenyAll())

	if res.Plan != nil {
		t.Fatal("deny-all produced a care plan — agent work executed")
	}
	if len(res.Failures) == 0 || !errors.Is(res.Failures[0].Error, admit.ErrDenied) {
		t.Fatalf("run did not halt with ErrDenied: %+v", res.Failures)
	}
	evs := rec.List()
	// Exactly one attempted hop (intake), exactly one denial, zero allows.
	if got := countDecisions(evs, string(governance.DecisionDeny)); got != 1 {
		t.Errorf("deny records = %d, want 1 (intake only)", got)
	}
	if got := countDecisions(evs, string(governance.DecisionAllow)); got != 0 {
		t.Errorf("allow records = %d, want 0 under deny-all", got)
	}
}

func TestDenySingleAgentHaltsAtThatAgent(t *testing.T) {
	res, rec := admittedRun(t, admit.DenyAgent("care_plan"))

	if res.Plan != nil {
		t.Fatal("care plan produced despite care_plan being denied")
	}
	if len(res.Failures) == 0 || res.Failures[0].ExecutorID != "care_plan" {
		t.Fatalf("halt not at care_plan: %+v", res.Failures)
	}
	evs := rec.List()
	// Upstream hops all ran: 5 allows (intake, planner, dx, planner, dx).
	if got := countDecisions(evs, string(governance.DecisionAllow)); got != 5 {
		t.Errorf("allow records = %d, want 5 upstream hops", got)
	}
	if got := countDecisions(evs, string(governance.DecisionDeny)); got != 1 {
		t.Errorf("deny records = %d, want 1 (care_plan)", got)
	}
}

func TestErroringPolicyEngineFailsClosed(t *testing.T) {
	res, rec := admittedRun(t, admit.Erroring())

	if res.Plan != nil {
		t.Fatal("care plan produced despite broken policy engine")
	}
	if len(res.Failures) == 0 || !errors.Is(res.Failures[0].Error, admit.ErrDenied) {
		t.Fatalf("broken policy engine did not fail closed: %+v", res.Failures)
	}
	if got := countDecisions(rec.List(), string(governance.DecisionDeny)); got != 1 {
		t.Errorf("deny records = %d, want 1", got)
	}
}
