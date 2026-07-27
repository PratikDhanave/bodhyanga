package pipeline_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodh-maf/pkg/agents"
)

// TestIntake_TrailNeverCarriesPHI runs every success and rejection fixture
// through the front door and asserts that no raw identifier, patient name, or
// clinical narrative from any fixture appears anywhere in the audit trail —
// identifiers survive only as HashCaseID fingerprints (SC-004, FR-009).
func TestIntake_TrailNeverCarriesPHI(t *testing.T) {
	inputs := []struct {
		format  agents.Format
		fixture string
	}{
		// success paths
		{agents.FormatHL7, "adt_a01_admit.hl7"},
		{agents.FormatHL7, "adt_a08_update.hl7"},
		{agents.FormatFHIR, "bundle_one_condition.json"},
		{agents.FormatFHIR, "bundle_multi_condition.json"},
		{agents.FormatFHIR, "bundle_no_condition.json"},
		// rejection paths
		{agents.FormatHL7, "adt_malformed.hl7"},
		{agents.FormatHL7, "oru_r01_cbc.hl7"},
		{agents.FormatHL7, "adt_a40_merge.hl7"},
		{agents.FormatHL7, "adt_no_pid.hl7"},
		{agents.FormatFHIR, "bundle_no_patient.json"},
		{agents.FormatFHIR, "bundle_two_patients.json"},
	}

	// Raw identifiers, names, and narrative present in the fixtures — none may
	// ever appear in an audit event.
	forbidden := []string{
		"MRN12345", "PT-MRN-778", "PT-MRN-902", "PT-MRN-555", "PT-MRN-111", "PT-MRN-222",
		"DOE", "ROE", "POE", "JOHN", "JANE", "RICHARD", "PAT", "WELBY", "MARCUS",
		"Heart failure", "diabetes", "hypertension",
	}

	for _, in := range inputs {
		out, trail := runInboundTolerant(t, agents.Inbound{Format: in.format, Payload: hl7Fixture(t, in.fixture)})
		_ = out
		blob, err := json.Marshal(trail)
		if err != nil {
			t.Fatal(err)
		}
		hay := string(blob)
		for _, bad := range forbidden {
			if strings.Contains(hay, bad) {
				t.Errorf("fixture %s leaked %q into the audit trail", in.fixture, bad)
			}
		}
	}
}
