package medical

import (
	"testing"
	"time"
)

func TestDefaultPanelRules(t *testing.T) {
	now := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	patients := []PatientRecord{
		{
			PatientID: "p1", Conditions: []string{"dm"},
			LastA1c: 9.5, LastA1cDate: now.AddDate(0, -1, 0),
			LastVisit: now.AddDate(0, -1, 0),
		},
		{
			PatientID: "p2", Conditions: []string{"htn"},
			LastSystolicBP: 162,
			LastVisit:      now.AddDate(0, -1, 0),
		},
		{
			PatientID: "p3", Conditions: []string{"chf"},
			LastWeightLb: 195,
			DischargedAt: now.AddDate(0, 0, -10),
			LastVisit:    now.AddDate(0, -2, 0),
		},
		{
			PatientID: "p4",
			LastVisit:  now.AddDate(-2, 0, 0),
		},
	}
	report := BuildPanelReport(patients, DefaultPanelRules(), now)
	wantRules := map[string]bool{
		"a1c_uncontrolled":   false,
		"bp_uncontrolled":    false,
		"chf_weight_alert":   false,
		"annual_visit_due":   false,
		"tcm_followup_open":  false,
	}
	for _, g := range report.Gaps {
		if _, ok := wantRules[g.Rule]; ok {
			wantRules[g.Rule] = true
		}
	}
	for rule, found := range wantRules {
		if !found {
			t.Errorf("expected rule %q to fire", rule)
		}
	}
	if report.PanelSize != 4 {
		t.Errorf("panel size: want 4, got %d", report.PanelSize)
	}
}
