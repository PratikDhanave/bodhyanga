package medical

import (
	"encoding/json"
	"strings"
	"time"
)

// DischargeSummary is the payload for transitional care management (TCM).
type DischargeSummary struct {
	CaseID       string            `json:"case_id"`
	PatientID    string            `json:"patient_id"`
	DischargedAt time.Time         `json:"discharged_at"`
	PrimaryDx    string            `json:"primary_diagnosis"`
	Disposition  string            `json:"disposition,omitempty"` // home, snf, hospice
	HomeMeds     []MedicationEntry `json:"home_meds,omitempty"`
	NewMeds      []MedicationEntry `json:"new_meds,omitempty"`
	StoppedMeds  []string          `json:"stopped_meds,omitempty"`
	FollowUpDays []int             `json:"follow_up_days,omitempty"` // e.g. [2,7,30]
	HighRisk     bool              `json:"high_risk,omitempty"`
}

// MedReconciliation summarizes med-list deltas detected against an old list.
type MedReconciliation struct {
	Added    []MedicationEntry `json:"added,omitempty"`
	Stopped  []string          `json:"stopped,omitempty"`
	Kept     []MedicationEntry `json:"kept,omitempty"`
	Conflict []string          `json:"conflicts,omitempty"`
}

// EncodeDischarge / DecodeDischarge move discharge payloads on the bus.
func EncodeDischarge(d DischargeSummary) string {
	b, _ := json.Marshal(d)
	return string(b)
}

func DecodeDischarge(content string) (DischargeSummary, error) {
	var d DischargeSummary
	if err := json.Unmarshal([]byte(content), &d); err != nil {
		return DischargeSummary{}, err
	}
	return d, nil
}

// ReconcileMedications compares two med lists and flags simple class conflicts.
// Rule scope: detects duplicate-class entries and flags an empty new list when
// chronic meds were on the home list.
func ReconcileMedications(home, current []MedicationEntry) MedReconciliation {
	rec := MedReconciliation{}
	homeByName := indexByName(home)
	currentByName := indexByName(current)

	for name, med := range currentByName {
		if _, ok := homeByName[name]; ok {
			rec.Kept = append(rec.Kept, med)
		} else {
			rec.Added = append(rec.Added, med)
		}
	}
	for name := range homeByName {
		if _, ok := currentByName[name]; !ok {
			rec.Stopped = append(rec.Stopped, name)
		}
	}

	classCount := map[string]int{}
	for _, m := range current {
		if m.Class != "" {
			classCount[m.Class]++
		}
	}
	for cls, n := range classCount {
		if n > 1 {
			rec.Conflict = append(rec.Conflict, "duplicate class: "+cls)
		}
	}
	return rec
}

func indexByName(list []MedicationEntry) map[string]MedicationEntry {
	out := map[string]MedicationEntry{}
	for _, m := range list {
		out[strings.ToLower(m.Name)] = m
	}
	return out
}
