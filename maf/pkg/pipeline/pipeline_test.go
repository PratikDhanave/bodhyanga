package pipeline_test

import (
	"testing"

	"github.com/PratikDhanave/bodhyanga-maf/internal/slicetest"
	"github.com/PratikDhanave/bodhyanga-maf/pkg/agents"
	"github.com/PratikDhanave/bodhyanga-maf/pkg/pipeline"
)

// runCase builds a workflow from cfg and runs one case, failing the test on
// executor failures — the right default for pipeline-semantics tests.
// Admission tests (admission_test.go) use slicetest.Run directly instead.
func runCase(t *testing.T, cfg pipeline.Config, pc agents.PatientCase) (agents.CarePlan, []agents.ForcedProgressionEvent) {
	t.Helper()
	wf, err := pipeline.New(cfg)
	if err != nil {
		t.Fatalf("build workflow: %v", err)
	}
	res := slicetest.Run(t, wf, pc)
	for _, f := range res.Failures {
		t.Fatalf("executor %s failed: %v", f.ExecutorID, f.Error)
	}
	if res.Plan == nil {
		t.Fatal("workflow produced no CarePlan output")
	}
	return *res.Plan, res.Forced
}

func TestHappyPathLoopsThenDiagnoses(t *testing.T) {
	plan, forced := runCase(t, pipeline.Config{}, agents.PatientCase{
		CaseID:   "case-acs",
		Symptoms: []string{"chest pain"},
	})

	if plan.Diagnosis != "acute coronary syndrome" {
		t.Errorf("diagnosis = %q, want acute coronary syndrome", plan.Diagnosis)
	}
	if plan.Forced {
		t.Error("plan marked forced; want confident diagnosis")
	}
	if len(forced) != 0 {
		t.Errorf("got %d forced-progression events, want 0", len(forced))
	}
	if len(plan.Steps) == 0 {
		t.Error("care plan has no steps")
	}
}

func TestForcedProgressionAtMaxRounds(t *testing.T) {
	plan, forced := runCase(t, pipeline.Config{Tau: 0.99, MaxRounds: 2}, agents.PatientCase{
		CaseID:   "case-stubborn",
		Symptoms: []string{"chest pain"},
	})

	if !plan.Forced {
		t.Error("plan not marked forced despite unreachable τ")
	}
	if len(forced) != 1 {
		t.Fatalf("got %d forced-progression events, want 1", len(forced))
	}
	if forced[0].Rounds != 2 {
		t.Errorf("forced at round %d, want 2", forced[0].Rounds)
	}
	if forced[0].CaseIDHash != agents.HashCaseID("case-stubborn") {
		t.Error("forced event does not carry the hashed case ID")
	}
	if forced[0].CaseIDHash == "case-stubborn" {
		t.Error("PHI rule violation: raw case ID on a domain event")
	}
	if plan.Steps[0] != "refer for specialist review" {
		t.Errorf("forced plan step[0] = %q, want specialist review", plan.Steps[0])
	}
}

func TestUnknownPresentationForcesReview(t *testing.T) {
	plan, forced := runCase(t, pipeline.Config{}, agents.PatientCase{
		CaseID:   "case-mystery",
		Symptoms: []string{"unexplained rash"},
	})

	if plan.Diagnosis != "undifferentiated presentation" {
		t.Errorf("diagnosis = %q, want undifferentiated presentation", plan.Diagnosis)
	}
	if !plan.Forced || len(forced) != 1 {
		t.Errorf("unknown presentation should force progression (forced=%v, events=%d)",
			plan.Forced, len(forced))
	}
}

func TestDeidentifySeamRunsBeforePipeline(t *testing.T) {
	deidentified := false
	plan, _ := runCase(t, pipeline.Config{
		Deidentify: func(pc agents.PatientCase) agents.PatientCase {
			deidentified = true
			pc.CaseID = "deid-" + agents.HashCaseID(pc.CaseID)
			return pc
		},
	}, agents.PatientCase{CaseID: "mrn-123456", Symptoms: []string{"fever", "cough"}})

	if !deidentified {
		t.Error("deidentify hook never ran")
	}
	if plan.CaseID == "mrn-123456" {
		t.Error("raw case ID leaked through the pipeline output")
	}
	if plan.Diagnosis != "community-acquired pneumonia" {
		t.Errorf("diagnosis = %q, want community-acquired pneumonia", plan.Diagnosis)
	}
}
