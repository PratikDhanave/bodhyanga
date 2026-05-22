package medical

import (
	"encoding/json"
	"time"
)

// ADTEventType enumerates admit/discharge/transfer kinds we care about.
type ADTEventType string

const (
	ADTAdmit     ADTEventType = "admit"
	ADTDischarge ADTEventType = "discharge"
	ADTTransfer  ADTEventType = "transfer"
)

// ADTEvent is a single hospital admit/discharge/transfer message.
type ADTEvent struct {
	EventID    string       `json:"event_id"`
	PatientID  string       `json:"patient_id"`
	Type       ADTEventType `json:"type"`
	Facility   string       `json:"facility,omitempty"`
	Unit       string       `json:"unit,omitempty"`
	OccurredAt time.Time    `json:"occurred_at"`
	PrimaryDx  string       `json:"primary_diagnosis,omitempty"`
}

// EncodeADT / DecodeADT marshal events for the bus.
func EncodeADT(e ADTEvent) string {
	b, _ := json.Marshal(e)
	return string(b)
}

func DecodeADT(content string) (ADTEvent, error) {
	var e ADTEvent
	if err := json.Unmarshal([]byte(content), &e); err != nil {
		return ADTEvent{}, err
	}
	return e, nil
}

// ReadmissionRisk is a 0..1 score with a human-readable band.
type ReadmissionRisk struct {
	Score float64 `json:"score"`
	Band  string  `json:"band"` // low / medium / high
}

// ScoreReadmissionRisk is a transparent rule-based score (LACE-inspired demo).
// Inputs come from the patient registry, not from PHI logs.
// Rule weights: age, condition burden, prior discharge, BP/A1c control.
func ScoreReadmissionRisk(p PatientRecord, now time.Time) ReadmissionRisk {
	score := 0.0
	if p.Age >= 75 {
		score += 0.25
	} else if p.Age >= 65 {
		score += 0.15
	}
	score += float64(len(p.Conditions)) * 0.08
	if !p.DischargedAt.IsZero() && now.Sub(p.DischargedAt) < 30*24*time.Hour {
		score += 0.25
	}
	if p.LastSystolicBP >= 160 {
		score += 0.1
	}
	if p.LastA1c >= 9 {
		score += 0.1
	}
	if score > 1 {
		score = 1
	}
	band := "low"
	switch {
	case score >= 0.6:
		band = "high"
	case score >= 0.3:
		band = "medium"
	}
	return ReadmissionRisk{Score: score, Band: band}
}
