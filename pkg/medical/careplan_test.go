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
