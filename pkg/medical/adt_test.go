package medical

import (
	"testing"
	"time"
)

func TestScoreReadmissionRisk(t *testing.T) {
	now := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	low := ScoreReadmissionRisk(PatientRecord{Age: 40}, now)
	if low.Band != "low" {
		t.Errorf("expected low band, got %+v", low)
	}
	high := ScoreReadmissionRisk(PatientRecord{
		Age:            78,
		Conditions:     []string{"chf", "htn", "dm"},
		LastSystolicBP: 170,
		LastA1c:        10,
		DischargedAt:   now.AddDate(0, 0, -5),
	}, now)
	if high.Band != "high" {
		t.Errorf("expected high band, got %+v", high)
	}
	if high.Score > 1 {
		t.Errorf("score must be clipped to 1, got %v", high.Score)
	}
}

func TestADTRoundTrip(t *testing.T) {
	e := ADTEvent{EventID: "1", PatientID: "p1", Type: ADTDischarge, OccurredAt: time.Now()}
	got, err := DecodeADT(EncodeADT(e))
	if err != nil {
		t.Fatal(err)
	}
	if got.PatientID != "p1" || got.Type != ADTDischarge {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}
