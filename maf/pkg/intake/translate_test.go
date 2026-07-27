package intake

import (
	"testing"

	"github.com/PratikDhanave/bodh-maf/pkg/agents"
)

func TestTranslate_JSONRoundTrips(t *testing.T) {
	in := agents.Inbound{Format: agents.FormatJSON, Payload: []byte(`{"case_id":"c1","symptoms":["cough"],"history":["asthma"]}`)}
	pc, err := Translate(in)
	if err != nil {
		t.Fatalf("json translate: %v", err)
	}
	if pc.CaseID != "c1" || len(pc.Symptoms) != 1 || len(pc.History) != 1 {
		t.Errorf("round-trip mismatch: %+v", pc)
	}
}

func TestTranslate_MalformedJSON(t *testing.T) {
	in := agents.Inbound{Format: agents.FormatJSON, Payload: []byte(`{not json`)}
	assertReason(t, mustErr(Translate(in)), ReasonMalformed)
}

func TestTranslate_HL7Routed(t *testing.T) {
	in := agents.Inbound{Format: agents.FormatHL7, Payload: readFixture(t, "adt_a01_admit.hl7")}
	pc, err := Translate(in)
	if err != nil {
		t.Fatalf("hl7 translate: %v", err)
	}
	if pc.CaseID != "MRN12345" {
		t.Errorf("CaseID = %q, want MRN12345", pc.CaseID)
	}
}

func TestTranslate_UnknownFormat(t *testing.T) {
	in := agents.Inbound{Format: agents.Format("xml"), Payload: []byte("<x/>")}
	assertReason(t, mustErr(Translate(in)), ReasonUnsupportedType)
}

func mustErr(_ agents.PatientCase, err error) error { return err }
