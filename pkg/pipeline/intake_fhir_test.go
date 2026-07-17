package pipeline_test

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/audit"

	"github.com/PratikDhanave/bodh-maf/pkg/admit"
	"github.com/PratikDhanave/bodh-maf/pkg/agents"
	"github.com/PratikDhanave/bodh-maf/pkg/pipeline"
)

// runInbound is a small helper: one Inbound through a translating pipeline.
func runInbound(t *testing.T, in agents.Inbound) (*pipeline.Outcome, []audit.Event) {
	t.Helper()
	rec := audit.NewInMemory()
	b := admit.New(admit.AllowAll(), rec, nil)
	runner, err := pipeline.NewRunner(pipeline.Config{
		Boundary: b, Translate: true, Tau: 0.85, MaxRounds: 2,
	}, rec)
	if err != nil {
		t.Fatal(err)
	}
	out, err := runner.RunInbound(context.Background(), in, "")
	if err != nil {
		t.Fatal(err)
	}
	return out, rec.List()
}

func TestIntakeFHIR_BundleProducesCarePlan(t *testing.T) {
	out, trail := runInbound(t, agents.Inbound{
		Format: agents.FormatFHIR, Payload: hl7Fixture(t, "bundle_one_condition.json"),
	})
	if out.Plan == nil {
		t.Fatalf("FHIR bundle produced no care plan; outcome %+v", out)
	}
	if firstAdmission(trail, "translator") < 0 || firstAdmission(trail, "intake") < 0 {
		t.Error("expected translation + intake admission records on the trail")
	}
}

// pipelineShape captures the trail SHAPE (Kind/Decision/AgentID/Type per row)
// of the format-agnostic pipeline — every hop EXCEPT the front-door translator,
// which is the only part that differs by input format. The result is SORTED
// and translator rows are dropped by identity (not by index): the slice's BSP
// super-step batching does not fix the global interleaving of message vs
// admission rows (session verdict: "parity holds per hop, not global
// interleaving"), so a translator row can land on either side of the intake
// admission. Excluding it by agent id — rather than windowing from an index —
// makes the comparison position-independent and deterministic.
func pipelineShape(events []audit.Event) []string {
	var shape []string
	for _, e := range events {
		if e.AgentID == "translator" {
			continue
		}
		shape = append(shape, fmt.Sprintf("%s/%s/%s/%s", e.Kind, e.Decision, e.AgentID, e.Type))
	}
	sort.Strings(shape)
	return shape
}

// The SAME clinical facts (one condition, no symptoms → undifferentiated) fed
// as HL7 and as FHIR yield structurally identical trails from intake onward —
// the graph below the front door is format-agnostic (SC-002, SC-005, FR-007).
func TestIntakeParity_HL7AndFHIRSameTrailShape(t *testing.T) {
	_, hl7Trail := runInbound(t, agents.Inbound{
		Format: agents.FormatHL7, Payload: hl7Fixture(t, "adt_a01_admit.hl7"),
	})
	_, fhirTrail := runInbound(t, agents.Inbound{
		Format: agents.FormatFHIR, Payload: hl7Fixture(t, "bundle_one_condition.json"),
	})

	hs := pipelineShape(hl7Trail)
	fs := pipelineShape(fhirTrail)
	if len(hs) == 0 || len(fs) == 0 {
		t.Fatal("empty trail shape")
	}
	if len(hs) != len(fs) {
		t.Fatalf("trail multisets differ in size: hl7=%d fhir=%d\nhl7=%v\nfhir=%v", len(hs), len(fs), hs, fs)
	}
	for i := range hs {
		if hs[i] != fs[i] {
			t.Errorf("trail structure diverges: hl7=%q fhir=%q\nhl7=%v\nfhir=%v", hs[i], fs[i], hs, fs)
		}
	}
}
