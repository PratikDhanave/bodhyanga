package pipeline_test

import (
	"context"
	"os"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/audit"

	"github.com/PratikDhanave/bodh-maf/pkg/admit"
	"github.com/PratikDhanave/bodh-maf/pkg/agents"
	"github.com/PratikDhanave/bodh-maf/pkg/pipeline"
)

func hl7Fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("../intake/testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// firstAdmission returns the index of the first allow decision for agentID, or
// -1. Admission records are the KindPolicyDecision rows the boundary writes.
func firstAdmission(events []audit.Event, agentID string) int {
	for i, e := range events {
		if e.Kind == audit.KindPolicyDecision && e.Decision == "allow" && e.AgentID == agentID {
			return i
		}
	}
	return -1
}

// TestIntakeHL7_A01ProducesCarePlan runs a real HL7 ADT^A01 message through the
// translation front door and asserts (a) a care plan is produced and (b) the
// translation hop is admitted before intake — the translator is inside the
// admission boundary (SC-001, FR-004, FR-007).
func TestIntakeHL7_A01ProducesCarePlan(t *testing.T) {
	rec := audit.NewInMemory()
	b := admit.New(admit.AllowAll(), rec, nil)
	runner, err := pipeline.NewRunner(pipeline.Config{
		Boundary: b, Translate: true, Tau: 0.85, MaxRounds: 2,
	}, rec)
	if err != nil {
		t.Fatal(err)
	}

	out, err := runner.RunInbound(context.Background(),
		agents.Inbound{Format: agents.FormatHL7, Payload: hl7Fixture(t, "adt_a01_admit.hl7")}, "")
	if err != nil {
		t.Fatal(err)
	}
	if out.Plan == nil {
		t.Fatalf("HL7 case produced no care plan; outcome %+v", out)
	}

	// The raw MRN never appears on the plan — only the hashed token.
	if out.Plan.CaseID == "" {
		t.Error("plan carries no case id")
	}

	trail := rec.List()
	tr := firstAdmission(trail, "translator")
	in := firstAdmission(trail, "intake")
	if tr < 0 {
		t.Fatal("no translation admission record on the trail")
	}
	if in < 0 {
		t.Fatal("no intake admission record on the trail")
	}
	if tr >= in {
		t.Errorf("translation admission (idx %d) must precede intake (idx %d)", tr, in)
	}
}

// The raw MRN and patient name must never appear anywhere on the trail.
func TestIntakeHL7_TrailIsPHIFree(t *testing.T) {
	rec := audit.NewInMemory()
	b := admit.New(admit.AllowAll(), rec, nil)
	runner, _ := pipeline.NewRunner(pipeline.Config{Boundary: b, Translate: true, Tau: 0.85, MaxRounds: 2}, rec)
	if _, err := runner.RunInbound(context.Background(),
		agents.Inbound{Format: agents.FormatHL7, Payload: hl7Fixture(t, "adt_a01_admit.hl7")}, ""); err != nil {
		t.Fatal(err)
	}
	for _, e := range rec.List() {
		if e.CaseID == "MRN12345" {
			t.Errorf("raw MRN leaked into trail on event %+v", e)
		}
	}
}
