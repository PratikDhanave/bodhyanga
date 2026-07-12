package pipeline_test

// One test per README exit criterion (spec FR-014, SC-006). Each is a thin
// wrapper over the machinery proven in the story-level tests; together they
// are the slice's go/no-go gate and run with no opt-in flags. Criterion 3's
// Postgres-backed variant lives in pkg/ckptpg (TestKillResume, testcontainers);
// here the same kill/resume mechanics are proven on MAF's durable file store
// so the gate never silently skips.

import (
	"context"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/eval"
	"github.com/microsoft/agent-framework-go/workflow"
	"github.com/microsoft/agent-framework-go/workflow/checkpoint"

	"github.com/PratikDhanave/bodh-maf/pkg/admit"
	"github.com/PratikDhanave/bodh-maf/pkg/agents"
	"github.com/PratikDhanave/bodh-maf/pkg/auditbridge"
	"github.com/PratikDhanave/bodh-maf/pkg/pipeline"
)

func TestExitCriterion1_AdmissionFidelity(t *testing.T) {
	denied, deniedRec := admittedRun(t, admit.DenyAll())
	if denied.Plan != nil || countDecisions(deniedRec.List(), "deny") != 1 {
		t.Fatal("deny-all did not block with exactly one audited denial")
	}
	allowed, _ := admittedRun(t, admit.AllowAll())
	if allowed.Plan == nil {
		t.Fatal("allow policy did not complete the same case")
	}
}

func TestExitCriterion2_AuditParity(t *testing.T) {
	rec := audit.NewInMemory()
	out := canonicalRun(t, rec)
	if out.Plan == nil {
		t.Fatal("canonical case incomplete")
	}
	evs := rec.List()
	for name, check := range map[string]func([]audit.Event) error{
		"hop completeness": auditbridge.CheckHopCompleteness,
		"deny terminal":    auditbridge.CheckDenyTerminal,
		"review anchored":  auditbridge.CheckReviewAnchored,
	} {
		if err := check(evs); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := auditbridge.CheckPHIFree(evs, "case-acs", "chest pain", "acute coronary syndrome"); err != nil {
		t.Errorf("PHI scan: %v", err)
	}
}

func TestExitCriterion3_DurableReview(t *testing.T) {
	store, err := checkpoint.NewFileSystemJSONStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const session = "sess-ec3"
	newRunner := func(rec audit.Recorder) *pipeline.Runner {
		r, err := pipeline.NewRunner(pipeline.Config{
			Review: true, Boundary: admit.New(admit.AllowAll(), rec, nil),
		}, rec)
		if err != nil {
			t.Fatal(err)
		}
		return r.WithCheckpointStore(store)
	}

	out1, err := newRunner(audit.NewInMemory()).Run(context.Background(), reviewCase, session)
	if err != nil || !out1.Paused {
		t.Fatalf("life 1 did not pause: %v %+v", err, out1)
	}

	r2 := newRunner(audit.NewInMemory())
	r2.Responder = decideWith(agents.ReviewDecision{Approved: true, ReviewerID: "dr-ec3"})
	out2, err := r2.Resume(context.Background(), session)
	if err != nil || out2.Plan == nil {
		t.Fatalf("resume did not complete: %v %+v", err, out2)
	}
}

func TestExitCriterion4_EngineSwap(t *testing.T) {
	ruleOut, _, err := runEngine(t, "")
	if err != nil || ruleOut.Plan == nil {
		t.Fatalf("rule engine: %v %+v", err, ruleOut)
	}
	llmOut, _, err := runEngine(t, confidentACS)
	if err != nil || llmOut.Plan == nil {
		t.Fatalf("llm engine: %v %+v", err, llmOut)
	}
	if ruleOut.Plan.Diagnosis != llmOut.Plan.Diagnosis {
		t.Errorf("engines disagree structurally: %q vs %q", ruleOut.Plan.Diagnosis, llmOut.Plan.Diagnosis)
	}
}

func TestExitCriterion5_EvalMirrorClosed(t *testing.T) {
	cases := []struct {
		name    string
		decide  agents.ReviewDecision
		success bool
		verdict string
	}{
		{"approved", agents.ReviewDecision{Approved: true, ReviewerID: "dr-a"}, true, "approve"},
		{"rejected", agents.ReviewDecision{Approved: false, ReviewerID: "dr-a"}, false, "reject"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := audit.NewInMemory()
			store := eval.NewInMemoryStore()
			r, err := pipeline.NewRunner(pipeline.Config{
				Review: true, Boundary: admit.New(admit.AllowAll(), rec, nil),
				EvalStore: store,
			}, rec)
			if err != nil {
				t.Fatal(err)
			}
			r.Responder = decideWith(tc.decide)
			out, err := r.Run(context.Background(), reviewCase, "")
			if err != nil {
				t.Fatal(err)
			}
			records := store.List()
			if len(records) != 1 {
				t.Fatalf("eval records = %d, want exactly 1 (no perpetual pending)", len(records))
			}
			got := records[0]
			if got.Success != tc.success || got.Metadata["review_decision"] != tc.verdict {
				t.Errorf("record wrong: success=%v decision=%v", got.Success, got.Metadata["review_decision"])
			}
			if got.Scenario != "mara-slice/"+agents.HashCaseID("case-acs") {
				t.Errorf("scenario %q not keyed on case token", got.Scenario)
			}
			if got.Metrics["rounds"] < 1 || got.Metrics["confidence"] <= 0 {
				t.Errorf("metrics missing: %+v", got.Metrics)
			}
			if out.Eval == nil {
				t.Error("outcome does not carry the eval record")
			}
		})
	}

	t.Run("forced progression is captured", func(t *testing.T) {
		rec := audit.NewInMemory()
		store := eval.NewInMemoryStore()
		r, err := pipeline.NewRunner(pipeline.Config{
			Tau: 0.99, MaxRounds: 2, Review: true,
			Boundary: admit.New(admit.AllowAll(), rec, nil), EvalStore: store,
		}, rec)
		if err != nil {
			t.Fatal(err)
		}
		r.Responder = decideWith(agents.ReviewDecision{Approved: true, ReviewerID: "dr-a"})
		if _, err := r.Run(context.Background(), reviewCase, ""); err != nil {
			t.Fatal(err)
		}
		recs := store.List()
		if len(recs) != 1 || recs[0].Metrics["forced"] != 1 || recs[0].Success {
			t.Fatalf("forced case record wrong: %+v", recs)
		}
	})

	// A paused (non-terminal) case must NOT produce a record.
	t.Run("paused case writes nothing", func(t *testing.T) {
		rec := audit.NewInMemory()
		store := eval.NewInMemoryStore()
		r, err := pipeline.NewRunner(pipeline.Config{
			Review: true, Boundary: admit.New(admit.AllowAll(), rec, nil), EvalStore: store,
		}, rec)
		if err != nil {
			t.Fatal(err)
		}
		if out, err := r.Run(context.Background(), reviewCase, ""); err != nil || !out.Paused {
			t.Fatalf("expected pause: %v", err)
		}
		if len(store.List()) != 0 {
			t.Fatal("paused case produced an eval record")
		}
	})
}

var _ = workflow.CheckpointInfo{} // keep workflow import for future criteria tweaks
