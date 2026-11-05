package fhir

import (
	"testing"
	"time"

	"github.com/PratikDhanave/bodhyanga/pkg/medical"
)

func TestObservationToRPM(t *testing.T) {
	o := Observation{
		Code:     CodeableConcept{Text: "systolic blood pressure"},
		Subject:  Reference{Reference: "Patient/p1"},
		ValueQty: &Quantity{Value: 168, Unit: "mmHg"},
	}
	rpm, ok := ObservationToRPM(o)
	if !ok || rpm.Metric != "systolic_bp" || rpm.Value != 168 {
		t.Fatalf("ObservationToRPM unexpected: ok=%v %+v", ok, rpm)
	}
}

func TestEncounterToADTDischarge(t *testing.T) {
	e := Encounter{
		ID:      "enc-1",
		Status:  "finished",
		Subject: Reference{Reference: "Patient/p1"},
		Period:  Period{Start: "2026-05-01T08:00:00Z", End: "2026-05-05T10:00:00Z"},
	}
	ev, ok := EncounterToADT(e)
	if !ok || ev.Type != medical.ADTDischarge || ev.PatientID != "p1" {
		t.Fatalf("EncounterToADT unexpected: ok=%v %+v", ok, ev)
	}
	want, _ := time.Parse(time.RFC3339, "2026-05-05T10:00:00Z")
	if !ev.OccurredAt.Equal(want) {
		t.Errorf("OccurredAt: want %v got %v", want, ev.OccurredAt)
	}
}

func TestPatientToRecord(t *testing.T) {
	p := Patient{ID: "p1", Gender: "female", BirthDate: "1960-01-01"}
	rec := PatientToRecord(p)
	if rec.PatientID != "p1" || rec.Sex != "female" {
		t.Errorf("fields: %+v", rec)
	}
	if rec.Age < 60 {
		t.Errorf("age: %d", rec.Age)
	}
}
