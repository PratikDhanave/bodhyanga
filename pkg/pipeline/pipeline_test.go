package pipeline

import (
	"context"
	"testing"

	"github.com/microsoft/agent-framework-go/workflow"
	"github.com/microsoft/agent-framework-go/workflow/inproc"

	"github.com/PratikDhanave/bodh-maf/pkg/agents"
)

// runCase executes one case and returns the care plan plus any forced-progression events.
func runCase(t *testing.T, cfg Config, pc agents.PatientCase) (agents.CarePlan, []agents.ForcedProgressionEvent) {
	t.Helper()
	wf, err := New(cfg)
	if err != nil {
		t.Fatalf("build workflow: %v", err)
	}
	ctx := context.Background()
	run, err := inproc.Default.RunStreaming(ctx, wf, pc)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	defer func() { _ = run.Close(ctx) }()

	var (
		plan   agents.CarePlan
		got    bool
		forced []agents.ForcedProgressionEvent
	)
	for evt, err := range run.WatchStream(ctx) {
		if err != nil {
			t.Fatalf("stream: %v", err)
		}
		switch e := evt.(type) {
		case workflow.ExecutorFailedEvent:
			t.Fatalf("executor %s failed: %v", e.ExecutorID, e.Error)
		case workflow.ErrorEvent:
			t.Fatalf("workflow error: %v", e.Error)
		case agents.ForcedProgressionEvent:
			forced = append(forced, e)
		case workflow.OutputEvent:
			plan, got = e.Output.(agents.CarePlan)
			if !got {
				t.Fatalf("unexpected output type %T", e.Output)
			}
		}
	}
	if !got {
		t.Fatal("workflow produced no CarePlan output")
	}
	return plan, forced
}

func TestHappyPathLoopsThenDiagnoses(t *testing.T) {
	plan, forced := runCase(t, Config{}, agents.PatientCase{
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
	// Round 0 is symptom-only (confidence 0.55 ≤ τ) so the loop-back edge
	// MUST have fired at least once before tests confirmed the diagnosis.
	if len(plan.Steps) == 0 {
		t.Error("care plan has no steps")
	}
}

func TestForcedProgressionAtMaxRounds(t *testing.T) {
	plan, forced := runCase(t, Config{Tau: 0.99, MaxRounds: 2}, agents.PatientCase{
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
	// Forced plans must route to review, not a protocol plan.
	if plan.Steps[0] != "refer for specialist review" {
		t.Errorf("forced plan step[0] = %q, want specialist review", plan.Steps[0])
	}
}

func TestUnknownPresentationForcesReview(t *testing.T) {
	plan, forced := runCase(t, Config{}, agents.PatientCase{
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
	plan, _ := runCase(t, Config{
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
