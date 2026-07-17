package intake

import (
	"testing"

	"github.com/PratikDhanave/bodh-maf/pkg/agents"
)

// Each canonical bad input maps to a DISTINCT PHI-free reason and yields no
// PatientCase (spec FR-005). The reasons must be distinguishable so an operator
// can tell a wrong-format feed from a corrupt one.
func TestRejectionTaxonomy_DistinctReasons(t *testing.T) {
	hl7 := func(b []byte) (agents.PatientCase, error) { return translateHL7(b) }
	fhir := func(b []byte) (agents.PatientCase, error) { return translateFHIR(b) }

	cases := []struct {
		name    string
		fixture string
		fn      func([]byte) (agents.PatientCase, error)
		want    RejectReason
	}{
		{"malformed HL7", "adt_malformed.hl7", hl7, ReasonMalformed},
		{"non-ADT HL7", "oru_r01_cbc.hl7", hl7, ReasonUnsupportedType},
		{"unsupported trigger A40", "adt_a40_merge.hl7", hl7, ReasonUnsupportedTrigger},
		{"missing PID", "adt_no_pid.hl7", hl7, ReasonMissingSegment},
		{"FHIR no patient", "bundle_no_patient.json", fhir, ReasonMissingResource},
		{"FHIR two patients", "bundle_two_patients.json", fhir, ReasonAmbiguousSubject},
		{"malformed FHIR", "bundle_malformed.json", fhir, ReasonMalformed},
	}

	seen := map[RejectReason]bool{}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pc, err := tc.fn(readFixture(t, tc.fixture))
			assertReason(t, err, tc.want)
			if pc.CaseID != "" || len(pc.History) != 0 || len(pc.Symptoms) != 0 {
				t.Errorf("a rejected input still produced a case: %+v", pc)
			}
		})
		seen[tc.want] = true
	}
	if len(seen) < 6 {
		t.Errorf("expected 6 distinct reasons exercised, got %d", len(seen))
	}
}
