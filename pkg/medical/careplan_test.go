package medical

import (
	"testing"
	"time"
)

func TestBuildCarePlanPerCondition(t *testing.T) {
	now := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		condition string
		wantGoal  string
		wantMed   string
	}{
		{"htn", "systolic_bp", "Lisinopril"},
		{"dm", "hba1c", "Metformin"},
		{"chf", "weight_lb", "Furosemide"},
		{"ckd", "egfr", "Losartan"},
		{"copd", "spo2", "Tiotropium"},
		{"asthma", "act_score", "Fluticasone"},
	}
	for _, c := range cases {
		plan := BuildCarePlan("case-1", "pt-1", c.condition, now)
		if len(plan.Goals) == 0 || plan.Goals[0].Metric != c.wantGoal {
			t.Errorf("%s: want goal metric %q, got %+v", c.condition, c.wantGoal, plan.Goals)
		}
		if len(plan.Medications) == 0 || plan.Medications[0].Name != c.wantMed {
			t.Errorf("%s: want first med %q, got %+v", c.condition, c.wantMed, plan.Medications)
		}
		if plan.NextReview.Before(now) {
			t.Errorf("%s: next review must be in the future", c.condition)
		}
	}
}

func TestBuildCarePlanAsthma(t *testing.T) {
	now := time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC)
	plan := BuildCarePlan("case-asthma", "pt-asthma", "asthma", now)

	if plan.Condition != "asthma" {
		t.Fatalf("condition: want asthma, got %q", plan.Condition)
	}
	// Goals: ACT, peak flow zone, exacerbations.
	wantGoals := map[string]bool{"act_score": false, "peak_flow_zone": false, "exacerbations": false}
	for _, g := range plan.Goals {
		if _, ok := wantGoals[g.Metric]; ok {
			wantGoals[g.Metric] = true
		}
	}
	for m, found := range wantGoals {
		if !found {
			t.Errorf("missing asthma goal %q", m)
		}
	}
	// Medications: ICS controller + SABA rescue.
	wantClasses := map[string]bool{"inhaled_corticosteroid": false, "bronchodilator_short_acting": false}
	for _, m := range plan.Medications {
		if _, ok := wantClasses[m.Class]; ok {
			wantClasses[m.Class] = true
		}
	}
	for c, found := range wantClasses {
		if !found {
			t.Errorf("missing asthma med class %q", c)
		}
	}
	// Monitoring: ACT, peak flow, exacerbation count.
	wantMonitors := map[string]bool{"act_score": false, "peak_flow": false, "exacerbation_count": false}
	for _, m := range plan.Monitoring {
		if _, ok := wantMonitors[m.Metric]; ok {
			wantMonitors[m.Metric] = true
		}
	}
	for m, found := range wantMonitors {
		if !found {
			t.Errorf("missing asthma monitoring %q", m)
		}
	}
	// Education: 4 topics.
	if len(plan.Education) != 4 {
		t.Errorf("education topics: want 4, got %d (%v)", len(plan.Education), plan.Education)
	}
	// NextReview: ~90 days out.
	wantReview := now.AddDate(0, 3, 0)
	if !plan.NextReview.Equal(wantReview) {
		t.Errorf("next review: want %s, got %s", wantReview, plan.NextReview)
	}
}

func TestCarePlanRoundTrip(t *testing.T) {
	plan := BuildCarePlan("case-9", "pt-9", "dm", time.Now())
	got, err := DecodeCarePlan(EncodeCarePlan(plan))
	if err != nil {
		t.Fatal(err)
	}
	if got.Condition != "dm" || got.PatientID != "pt-9" {
		t.Fatalf("round trip mismatch: %+v", got)
	}
}
