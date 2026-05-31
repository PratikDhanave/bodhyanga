package agents

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/PratikDhanave/bodh/pkg/agent"
	"github.com/PratikDhanave/bodh/pkg/medical"
)

// seedPHQ9Registry builds a small roster that exercises the phq9_overdue rule.
func seedPHQ9Registry() *medical.PatientRegistry {
	now := time.Now()
	reg := medical.NewPatientRegistry()

	// pt-dep-1: depression diagnosis, no PHQ-9 ever → gap expected.
	reg.Upsert(medical.PatientRecord{
		PatientID:  "pt-dep-1",
		Conditions: []string{"depression"},
		LastVisit:  now.AddDate(0, -1, 0),
	})

	// pt-dep-2: major_depressive_disorder, PHQ-9 done 100 days ago → gap expected.
	reg.Upsert(medical.PatientRecord{
		PatientID:   "pt-dep-2",
		Conditions:  []string{"major_depressive_disorder"},
		LastVisit:   now.AddDate(0, -1, 0),
		LastPHQ9Date: now.AddDate(0, 0, -100),
	})

	// pt-dep-3: depression, PHQ-9 done 30 days ago → no gap.
	reg.Upsert(medical.PatientRecord{
		PatientID:   "pt-dep-3",
		Conditions:  []string{"depression"},
		LastVisit:   now.AddDate(0, -1, 0),
		LastPHQ9Date: now.AddDate(0, 0, -30),
	})

	// pt-nodep: no depression condition → no gap.
	reg.Upsert(medical.PatientRecord{
		PatientID:  "pt-nodep",
		Conditions: []string{"htn"},
		LastVisit:  now.AddDate(0, -1, 0),
	})

	return reg
}

// TestPHQ9OverdueRuleUnit tests the phq9_overdue PanelRule directly.
func TestPHQ9OverdueRuleUnit(t *testing.T) {
	now := time.Now()

	cases := []struct {
		name      string
		patient   medical.PatientRecord
		wantGap   bool
		wantRule  string
	}{
		{
			name: "depression_no_phq9",
			patient: medical.PatientRecord{
				PatientID: "p1", Conditions: []string{"depression"},
			},
			wantGap:  true,
			wantRule: "phq9_overdue",
		},
		{
			name: "major_depressive_disorder_no_phq9",
			patient: medical.PatientRecord{
				PatientID: "p2", Conditions: []string{"major_depressive_disorder"},
			},
			wantGap:  true,
			wantRule: "phq9_overdue",
		},
		{
			name: "depression_phq9_overdue_100_days",
			patient: medical.PatientRecord{
				PatientID:   "p3",
				Conditions:  []string{"depression"},
				LastPHQ9Date: now.AddDate(0, 0, -100),
			},
			wantGap:  true,
			wantRule: "phq9_overdue",
		},
		{
			name: "depression_phq9_recent_30_days",
			patient: medical.PatientRecord{
				PatientID:   "p4",
				Conditions:  []string{"depression"},
				LastPHQ9Date: now.AddDate(0, 0, -30),
			},
			wantGap: false,
		},
		{
			name: "depression_phq9_exactly_90_days",
			patient: medical.PatientRecord{
				PatientID:   "p5",
				Conditions:  []string{"depression"},
				LastPHQ9Date: now.AddDate(0, 0, -90),
			},
			// exactly 90 days is not strictly greater than 90 days so no gap
			wantGap: false,
		},
		{
			name: "no_depression_condition",
			patient: medical.PatientRecord{
				PatientID:  "p6",
				Conditions: []string{"htn", "dm"},
			},
			wantGap: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Access the rule via DefaultPanelRules to avoid exporting it.
			// We run BuildPanelReport with only this patient.
			rules := []medical.PanelRule{}
			for _, r := range medical.DefaultPanelRules() {
				// We want only phq9_overdue; run all and filter gaps by rule name.
				_ = r
			}
			// Run all default rules and filter to phq9_overdue.
			report := medical.BuildPanelReport([]medical.PatientRecord{tc.patient}, medical.DefaultPanelRules(), now)

			var got []medical.CareGap
			for _, g := range report.Gaps {
				if g.Rule == "phq9_overdue" {
					got = append(got, g)
				}
			}
			_ = rules

			if tc.wantGap && len(got) == 0 {
				t.Errorf("case %q: expected phq9_overdue gap, got none", tc.name)
			}
			if !tc.wantGap && len(got) > 0 {
				t.Errorf("case %q: expected no phq9_overdue gap, got %+v", tc.name, got)
			}
			if tc.wantGap && len(got) > 0 && got[0].Rule != tc.wantRule {
				t.Errorf("case %q: gap rule = %q, want %q", tc.name, got[0].Rule, tc.wantRule)
			}
			if tc.wantGap && len(got) > 0 {
				if got[0].Detail != "PHQ-9 depression screening overdue (>90 days)" {
					t.Errorf("case %q: unexpected detail %q", tc.name, got[0].Detail)
				}
				if got[0].Severity != "warn" {
					t.Errorf("case %q: severity = %q, want warn", tc.name, got[0].Severity)
				}
			}
		})
	}
}

// TestPanelManagerPHQ9DirectMode exercises the panel manager end-to-end with
// a registry that has patients triggering phq9_overdue.
func TestPanelManagerPHQ9DirectMode(t *testing.T) {
	reg := seedPHQ9Registry()
	a := &PanelManagerAgent{Registry: reg, HITL: false}
	msg := agent.NewMessage("user", "panel_manager", agent.RoleUser,
		"panel_request", `{"condition":"all"}`, map[string]any{})

	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage err: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected at least one output message")
	}
	if out[0].Type != "panel_report" {
		t.Fatalf("first output must be panel_report, got %q", out[0].Type)
	}

	// Decode the report and verify at least two phq9_overdue gaps (pt-dep-1, pt-dep-2).
	var report medical.PanelReport
	if err := json.Unmarshal([]byte(out[0].Content), &report); err != nil {
		t.Fatalf("decode panel_report: %v", err)
	}

	var phq9Gaps []medical.CareGap
	for _, g := range report.Gaps {
		if g.Rule == "phq9_overdue" {
			phq9Gaps = append(phq9Gaps, g)
		}
	}
	if len(phq9Gaps) < 2 {
		t.Errorf("expected ≥2 phq9_overdue gaps, got %d: %+v", len(phq9Gaps), phq9Gaps)
	}

	// Verify pt-dep-3 (recent PHQ-9) and pt-nodep (no depression) did not fire.
	for _, g := range phq9Gaps {
		if g.PatientID == "pt-dep-3" || g.PatientID == "pt-nodep" {
			t.Errorf("unexpected phq9_overdue gap for %s", g.PatientID)
		}
	}
}
