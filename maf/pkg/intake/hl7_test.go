package intake

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return b
}

// The A01 fixture's PID-3 carries MRN12345 and DG1 an I50.9 heart-failure dx.
func TestTranslateHL7_A01AdmitYieldsPatientCase(t *testing.T) {
	pc, err := translateHL7(readFixture(t, "adt_a01_admit.hl7"))
	if err != nil {
		t.Fatalf("well-formed A01 rejected: %v", err)
	}
	if pc.CaseID != "MRN12345" {
		t.Errorf("CaseID = %q, want MRN12345 (from PID-3)", pc.CaseID)
	}
	if len(pc.History) == 0 {
		t.Errorf("History empty; expected the DG1 primary diagnosis")
	}
	if len(pc.Symptoms) != 0 {
		t.Errorf("Symptoms should be empty for an ADT feed, got %v", pc.Symptoms)
	}
}

func TestTranslateHL7_A08UpdateAccepted(t *testing.T) {
	if _, err := translateHL7(readFixture(t, "adt_a08_update.hl7")); err != nil {
		t.Errorf("A08 update rejected: %v", err)
	}
}

func TestTranslateHL7_NonADTIsUnsupportedType(t *testing.T) {
	_, err := translateHL7(readFixture(t, "oru_r01_cbc.hl7"))
	assertReason(t, err, ReasonUnsupportedType)
}

func TestTranslateHL7_MalformedIsMalformed(t *testing.T) {
	_, err := translateHL7(readFixture(t, "adt_malformed.hl7"))
	// A malformed message is either unparseable (malformed) or parses without
	// a usable PID (missing_segment) — both are PHI-free categories, never a
	// raw parser error.
	var re *RejectError
	if !errors.As(err, &re) {
		t.Fatalf("want *RejectError, got %v", err)
	}
	if re.Reason != ReasonMalformed && re.Reason != ReasonMissingSegment && re.Reason != ReasonUnsupportedType {
		t.Errorf("unexpected reason %q for malformed input", re.Reason)
	}
}

// A rejection must never leak the raw MRN or patient name into its message.
func TestTranslateHL7_RejectionCarriesNoPHI(t *testing.T) {
	_, err := translateHL7(readFixture(t, "oru_r01_cbc.hl7"))
	if err == nil {
		t.Fatal("expected a rejection")
	}
	msg := err.Error()
	for _, phi := range []string{"MRN", "DOE", "JOHN"} {
		if strings.Contains(strings.ToUpper(msg), phi) {
			t.Errorf("rejection message leaked PHI %q: %q", phi, msg)
		}
	}
}

func assertReason(t *testing.T, err error, want RejectReason) {
	t.Helper()
	var re *RejectError
	if !errors.As(err, &re) {
		t.Fatalf("want *RejectError(%s), got %v", want, err)
	}
	if re.Reason != want {
		t.Errorf("reason = %q, want %q", re.Reason, want)
	}
}
