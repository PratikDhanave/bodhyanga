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

func TestNoControllerPersistentAsthmaRule(t *testing.T) {
	now := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		patient PatientRecord
		want    bool
	}{
		{
			name: "asthma without ICS controller fires",
			patient: PatientRecord{
				PatientID:  "p-asthma-1",
				Conditions: []string{"asthma"},
				Medications: []MedicationEntry{
					{Name: "Albuterol", Class: "bronchodilator_short_acting"},
				},
			},
			want: true,
		},
		{
			name: "asthma with ICS controller does not fire",
			patient: PatientRecord{
				PatientID:  "p-asthma-2",
				Conditions: []string{"asthma"},
				Medications: []MedicationEntry{
					{Name: "Fluticasone", Class: "inhaled_corticosteroid"},
					{Name: "Albuterol", Class: "bronchodilator_short_acting"},
				},
			},
			want: false,
		},
		{
			name:    "non-asthma patient does not fire",
			patient: PatientRecord{PatientID: "p-htn", Conditions: []string{"htn"}},
			want:    false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := noControllerPersistentAsthmaRule(c.patient, now)
			fired := len(got) > 0
			if fired != c.want {
				t.Errorf("fired=%v want=%v gaps=%+v", fired, c.want, got)
			}
			if fired && got[0].Rule != "no_controller_persistent_asthma" {
				t.Errorf("rule name: got %q", got[0].Rule)
			}
			if fired && got[0].Severity != "urgent" {
				t.Errorf("severity: got %q", got[0].Severity)
			}
		})
	}
}

func TestACTOverdueRule(t *testing.T) {
	now := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		patient PatientRecord
		want    bool
	}{
		{
			name: "asthma with no ACT date fires",
			patient: PatientRecord{
				PatientID:  "p-act-1",
				Conditions: []string{"asthma"},
			},
			want: true,
		},
		{
			name: "asthma with stale ACT (>180d) fires",
			patient: PatientRecord{
				PatientID:   "p-act-2",
				Conditions:  []string{"asthma"},
				LastACTDate: now.AddDate(0, 0, -200),
			},
			want: true,
		},
		{
			name: "asthma with recent ACT does not fire",
			patient: PatientRecord{
				PatientID:   "p-act-3",
				Conditions:  []string{"asthma"},
				LastACTDate: now.AddDate(0, 0, -30),
			},
			want: false,
		},
		{
			name:    "non-asthma patient does not fire",
			patient: PatientRecord{PatientID: "p-dm", Conditions: []string{"dm"}},
			want:    false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := actOverdueRule(c.patient, now)
			fired := len(got) > 0
			if fired != c.want {
				t.Errorf("fired=%v want=%v gaps=%+v", fired, c.want, got)
			}
			if fired && got[0].Rule != "act_overdue" {
				t.Errorf("rule name: got %q", got[0].Rule)
			}
			if fired && got[0].Severity != "warn" {
				t.Errorf("severity: got %q", got[0].Severity)
			}
		})
	}
}
