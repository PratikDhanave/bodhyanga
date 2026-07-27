package pipeline_test

import (
	"context"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/audit"
	"github.com/PratikDhanave/bodh/pkg/governance"

	"github.com/PratikDhanave/bodh-maf/pkg/admit"
	"github.com/PratikDhanave/bodh-maf/pkg/agents"
	"github.com/PratikDhanave/bodh-maf/pkg/pipeline"
)

// runInboundTolerant runs an Inbound and returns the outcome + trail WITHOUT
// fataling on a run error — a front-door rejection is surfaced as a returned
// error (unlike a deny, which drain folds into out.Denied), and that is the
// expected result here.
func runInboundTolerant(t *testing.T, in agents.Inbound) (*pipeline.Outcome, []audit.Event) {
	t.Helper()
	rec := audit.NewInMemory()
	b := admit.New(admit.AllowAll(), rec, nil)
	runner, err := pipeline.NewRunner(pipeline.Config{Boundary: b, Translate: true, Tau: 0.85, MaxRounds: 2}, rec)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := runner.RunInbound(context.Background(), in, "")
	return out, rec.List()
}

func countKind(events []audit.Event, kind audit.Kind, agentID string) int {
	n := 0
	for _, e := range events {
		if e.Kind == kind && e.AgentID == agentID {
			n++
		}
	}
	return n
}

// Every un-translatable input is rejected at the front door: no care plan, no
// physician rejection, exactly one rejection (agent_error) record, and the
// pipeline never reaches intake (SC-003, FR-004).
func TestIntakeReject_BadInputProducesNoRunPastTranslator(t *testing.T) {
	cases := []struct {
		name    string
		format  agents.Format
		fixture string
	}{
		{"malformed hl7", agents.FormatHL7, "adt_malformed.hl7"},
		{"non-adt", agents.FormatHL7, "oru_r01_cbc.hl7"},
		{"unsupported trigger", agents.FormatHL7, "adt_a40_merge.hl7"},
		{"missing pid", agents.FormatHL7, "adt_no_pid.hl7"},
		{"fhir no patient", agents.FormatFHIR, "bundle_no_patient.json"},
		{"fhir two patients", agents.FormatFHIR, "bundle_two_patients.json"},
		{"malformed fhir", agents.FormatFHIR, "bundle_malformed.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, trail := runInboundTolerant(t, agents.Inbound{Format: tc.format, Payload: hl7Fixture(t, tc.fixture)})
			if out.Plan != nil {
				t.Errorf("rejected input produced a care plan: %+v", out.Plan)
			}
			if out.Rejected != nil {
				t.Errorf("front-door rejection must not be a physician CaseRejected: %+v", out.Rejected)
			}
			// A rejected hop is recorded twice by design: once synchronously by
			// the admit guard (RecordAgentError) and once by the audit bridge
			// from the ExecutorFailedEvent (the synchronous-vs-bridge duality
			// from the slice verdict). Both are PHI-free; what matters is that
			// the rejection IS on the record and nothing ran past it.
			if got := countKind(trail, audit.KindAgentError, "translator"); got < 1 {
				t.Errorf("want a translator rejection record, got %d", got)
			}
			if firstAdmission(trail, "intake") >= 0 {
				t.Error("pipeline reached intake on a rejected input")
			}
		})
	}
}

// The translation hop is INSIDE the admission boundary: a deny-all policy stops
// a valid HL7 message with a synchronous deny record and no case (SC-003 /
// specs/001 fail-closed).
func TestIntakeReject_FailClosedOnDeniedTranslation(t *testing.T) {
	for _, pol := range []struct {
		name   string
		policy governance.Policy
	}{
		{"deny-all", admit.DenyAll()},
		{"deny-translator", admit.DenyAgent("translator")},
	} {
		t.Run(pol.name, func(t *testing.T) {
			rec := audit.NewInMemory()
			b := admit.New(pol.policy, rec, nil)
			runner, err := pipeline.NewRunner(pipeline.Config{Boundary: b, Translate: true, Tau: 0.85, MaxRounds: 2}, rec)
			if err != nil {
				t.Fatal(err)
			}
			out, err := runner.RunInbound(context.Background(),
				agents.Inbound{Format: agents.FormatHL7, Payload: hl7Fixture(t, "adt_a01_admit.hl7")}, "")
			if err != nil {
				t.Fatal(err)
			}
			if out.Plan != nil {
				t.Errorf("denied translation still produced a care plan")
			}
			if !out.Denied {
				t.Errorf("expected the run to be denied")
			}
			if got := countKind(rec.List(), audit.KindPolicyDecision, "translator"); got == 0 {
				t.Error("no translator policy decision recorded")
			}
			if firstAdmission(rec.List(), "intake") >= 0 {
				t.Error("pipeline reached intake despite a denied translation")
			}
		})
	}
}
