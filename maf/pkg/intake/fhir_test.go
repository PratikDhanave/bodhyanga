package intake

import (
	"strings"
	"testing"
)

func TestTranslateFHIR_OneConditionYieldsHistory(t *testing.T) {
	pc, err := translateFHIR(readFixture(t, "bundle_one_condition.json"))
	if err != nil {
		t.Fatalf("one-condition bundle rejected: %v", err)
	}
	if pc.CaseID != "PT-MRN-778" {
		t.Errorf("CaseID = %q, want PT-MRN-778 (Patient.id)", pc.CaseID)
	}
	if len(pc.History) != 1 {
		t.Fatalf("History = %v, want exactly one condition", pc.History)
	}
	if pc.History[0] != "i50.9" { // ConditionCode lowercases the coding.code
		t.Errorf("History[0] = %q, want i50.9", pc.History[0])
	}
}

// Every Condition must contribute — none silently dropped (FR-002).
func TestTranslateFHIR_MultiConditionKeepsAll(t *testing.T) {
	pc, err := translateFHIR(readFixture(t, "bundle_multi_condition.json"))
	if err != nil {
		t.Fatalf("multi-condition bundle rejected: %v", err)
	}
	if len(pc.History) != 2 {
		t.Errorf("History = %v, want two conditions", pc.History)
	}
}

// A Patient with no clinical resources is undifferentiated, not an error (FR-008).
func TestTranslateFHIR_NoConditionIsUndifferentiated(t *testing.T) {
	pc, err := translateFHIR(readFixture(t, "bundle_no_condition.json"))
	if err != nil {
		t.Fatalf("no-condition bundle should be accepted, got %v", err)
	}
	if pc.CaseID == "" {
		t.Error("CaseID empty; Patient.id should still map")
	}
	if len(pc.History) != 0 || len(pc.Symptoms) != 0 {
		t.Errorf("expected undifferentiated case, got history=%v symptoms=%v", pc.History, pc.Symptoms)
	}
}

// A recognized Observation contributes a symptom (FR-002).
func TestTranslateFHIR_ObservationSeedsSymptoms(t *testing.T) {
	pc, err := translateFHIR(readFixture(t, "bundle_observation.json"))
	if err != nil {
		t.Fatalf("observation bundle rejected: %v", err)
	}
	if len(pc.Symptoms) != 1 || pc.Symptoms[0] != "heart_rate" {
		t.Errorf("Symptoms = %v, want [heart_rate]", pc.Symptoms)
	}
}

// A structurally invalid resource (id not a string) is malformed, not silently
// dropped.
func TestTranslateFHIR_BadInnerResourceIsMalformed(t *testing.T) {
	_, err := translateFHIR(readFixture(t, "bundle_bad_patient.json"))
	assertReason(t, err, ReasonMalformed)
}

func TestTranslateFHIR_NoPatientIsMissingResource(t *testing.T) {
	_, err := translateFHIR(readFixture(t, "bundle_no_patient.json"))
	assertReason(t, err, ReasonMissingResource)
}

func TestTranslateFHIR_TwoPatientsIsAmbiguousSubject(t *testing.T) {
	_, err := translateFHIR(readFixture(t, "bundle_two_patients.json"))
	assertReason(t, err, ReasonAmbiguousSubject)
}

func TestTranslateFHIR_MalformedIsMalformed(t *testing.T) {
	_, err := translateFHIR(readFixture(t, "bundle_malformed.json"))
	assertReason(t, err, ReasonMalformed)
}

// Rejections must not leak the raw MRN or patient name.
func TestTranslateFHIR_RejectionCarriesNoPHI(t *testing.T) {
	_, err := translateFHIR(readFixture(t, "bundle_two_patients.json"))
	if err == nil {
		t.Fatal("expected a rejection")
	}
	msg := strings.ToUpper(err.Error())
	for _, phi := range []string{"MRN", "DOE", "JOHN", "JANE"} {
		if strings.Contains(msg, phi) {
			t.Errorf("rejection leaked PHI %q: %q", phi, err.Error())
		}
	}
}
