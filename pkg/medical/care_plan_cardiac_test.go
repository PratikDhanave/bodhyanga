package medical

import (
	"testing"
	"time"
)

var cardiacNow = time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)

// ---------------------------------------------------------------------------
// BuildPostMICarePlan
// ---------------------------------------------------------------------------

func TestBuildPostMICarePlan_Condition(t *testing.T) {
	plan := BuildPostMICarePlan("case-mi-1", "pt-mi-1", cardiacNow)
	if plan.Condition != "post_mi" {
		t.Fatalf("condition: want post_mi, got %q", plan.Condition)
	}
}

func TestBuildPostMICarePlan_IDs(t *testing.T) {
	plan := BuildPostMICarePlan("case-mi-2", "pt-mi-2", cardiacNow)
	if plan.CaseID != "case-mi-2" {
		t.Errorf("case id: want case-mi-2, got %q", plan.CaseID)
	}
	if plan.PatientID != "pt-mi-2" {
		t.Errorf("patient id: want pt-mi-2, got %q", plan.PatientID)
	}
}

func TestBuildPostMICarePlan_Goals(t *testing.T) {
	plan := BuildPostMICarePlan("case-mi-3", "pt-mi-3", cardiacNow)
	wantGoals := map[string]bool{
		"ldl":                    false,
		"systolic_bp":            false,
		"smoking_status":         false,
		"cardiac_rehab_enrolled": false,
	}
	for _, g := range plan.Goals {
		if _, ok := wantGoals[g.Metric]; ok {
			wantGoals[g.Metric] = true
		}
	}
	for metric, found := range wantGoals {
		if !found {
			t.Errorf("missing post-MI goal metric %q", metric)
		}
	}
}

func TestBuildPostMICarePlan_Medications(t *testing.T) {
	plan := BuildPostMICarePlan("case-mi-4", "pt-mi-4", cardiacNow)
	wantClasses := map[string]bool{
		"antiplatelet":          false,
		"high_intensity_statin": false,
		"beta_blocker":          false,
		"ace_inhibitor":         false,
		"p2y12_antiplatelet":    false,
	}
	for _, m := range plan.Medications {
		if _, ok := wantClasses[m.Class]; ok {
			wantClasses[m.Class] = true
		}
	}
	for class, found := range wantClasses {
		if !found {
			t.Errorf("missing post-MI medication class %q", class)
		}
	}
}

func TestBuildPostMICarePlan_NextReview(t *testing.T) {
	plan := BuildPostMICarePlan("case-mi-5", "pt-mi-5", cardiacNow)
	want := cardiacNow.AddDate(0, 0, 14)
	if !plan.NextReview.Equal(want) {
		t.Errorf("next review: want %s (2-week), got %s", want, plan.NextReview)
	}
}

func TestBuildPostMICarePlan_Monitoring(t *testing.T) {
	plan := BuildPostMICarePlan("case-mi-6", "pt-mi-6", cardiacNow)
	if len(plan.Monitoring) == 0 {
		t.Fatal("monitoring entries must not be empty")
	}
}

func TestBuildPostMICarePlan_Education(t *testing.T) {
	plan := BuildPostMICarePlan("case-mi-7", "pt-mi-7", cardiacNow)
	if len(plan.Education) == 0 {
		t.Fatal("education entries must not be empty")
	}
}

func TestBuildPostMICarePlan_CreatedAt(t *testing.T) {
	plan := BuildPostMICarePlan("case-mi-8", "pt-mi-8", cardiacNow)
	if !plan.CreatedAt.Equal(cardiacNow.UTC()) {
		t.Errorf("created_at: want %s, got %s", cardiacNow.UTC(), plan.CreatedAt)
	}
}

// ---------------------------------------------------------------------------
// BuildPostStrokeCarePlan — ischemic
// ---------------------------------------------------------------------------

func TestBuildPostStrokeIschemicCarePlan_Condition(t *testing.T) {
	plan := BuildPostStrokeCarePlan("case-stroke-1", "pt-stroke-1", "ischemic", cardiacNow)
	if plan.Condition != "post_stroke_ischemic" {
		t.Fatalf("condition: want post_stroke_ischemic, got %q", plan.Condition)
	}
}

func TestBuildPostStrokeIschemicCarePlan_Medications(t *testing.T) {
	plan := BuildPostStrokeCarePlan("case-stroke-2", "pt-stroke-2", "ischemic", cardiacNow)
	wantClasses := map[string]bool{
		"antiplatelet":          false,
		"high_intensity_statin": false,
		"ace_inhibitor":         false,
		"anticoagulant_noac":    false,
	}
	for _, m := range plan.Medications {
		if _, ok := wantClasses[m.Class]; ok {
			wantClasses[m.Class] = true
		}
	}
	for class, found := range wantClasses {
		if !found {
			t.Errorf("ischemic stroke: missing medication class %q", class)
		}
	}
}

func TestBuildPostStrokeIschemicCarePlan_Goals(t *testing.T) {
	plan := BuildPostStrokeCarePlan("case-stroke-3", "pt-stroke-3", "ischemic", cardiacNow)
	wantGoals := map[string]bool{
		"systolic_bp":            false,
		"smoking_status":         false,
		"rehab_therapy_enrolled": false,
	}
	for _, g := range plan.Goals {
		if _, ok := wantGoals[g.Metric]; ok {
			wantGoals[g.Metric] = true
		}
	}
	for metric, found := range wantGoals {
		if !found {
			t.Errorf("ischemic stroke: missing goal metric %q", metric)
		}
	}
}

func TestBuildPostStrokeIschemicCarePlan_NextReview(t *testing.T) {
	plan := BuildPostStrokeCarePlan("case-stroke-4", "pt-stroke-4", "ischemic", cardiacNow)
	want := cardiacNow.AddDate(0, 0, 14)
	if !plan.NextReview.Equal(want) {
		t.Errorf("next review: want %s (2-week), got %s", want, plan.NextReview)
	}
}

// ---------------------------------------------------------------------------
// BuildPostStrokeCarePlan — hemorrhagic
// ---------------------------------------------------------------------------

func TestBuildPostStrokeHemorrhagicCarePlan_Condition(t *testing.T) {
	plan := BuildPostStrokeCarePlan("case-hem-1", "pt-hem-1", "hemorrhagic", cardiacNow)
	if plan.Condition != "post_stroke_hemorrhagic" {
		t.Fatalf("condition: want post_stroke_hemorrhagic, got %q", plan.Condition)
	}
}

func TestBuildPostStrokeHemorrhagicCarePlan_NoAnticoagulants(t *testing.T) {
	plan := BuildPostStrokeCarePlan("case-hem-2", "pt-hem-2", "hemorrhagic", cardiacNow)
	for _, m := range plan.Medications {
		if m.Class == "anticoagulant_noac" || m.Class == "anticoagulant" {
			t.Errorf("hemorrhagic stroke must NOT include anticoagulant class; found %q (%s)", m.Name, m.Class)
		}
	}
}

func TestBuildPostStrokeHemorrhagicCarePlan_HasAntihypertensive(t *testing.T) {
	plan := BuildPostStrokeCarePlan("case-hem-3", "pt-hem-3", "hemorrhagic", cardiacNow)
	found := false
	for _, m := range plan.Medications {
		if m.Class == "calcium_channel_blocker" {
			found = true
			break
		}
	}
	if !found {
		t.Error("hemorrhagic stroke: expected calcium_channel_blocker (amlodipine) in medications")
	}
}

func TestBuildPostStrokeHemorrhagicCarePlan_NextReview(t *testing.T) {
	plan := BuildPostStrokeCarePlan("case-hem-4", "pt-hem-4", "hemorrhagic", cardiacNow)
	want := cardiacNow.AddDate(0, 0, 14)
	if !plan.NextReview.Equal(want) {
		t.Errorf("next review: want %s (2-week), got %s", want, plan.NextReview)
	}
}

// ---------------------------------------------------------------------------
// Default (unknown strokeType) falls through to ischemic path
// ---------------------------------------------------------------------------

func TestBuildPostStrokeCarePlan_UnknownTypeDefaultsToIschemic(t *testing.T) {
	plan := BuildPostStrokeCarePlan("case-unk-1", "pt-unk-1", "unknown_type", cardiacNow)
	if plan.Condition != "post_stroke_ischemic" {
		t.Errorf("unknown strokeType should default to post_stroke_ischemic, got %q", plan.Condition)
	}
}

// ---------------------------------------------------------------------------
// JSON round-trip
// ---------------------------------------------------------------------------

func TestPostMICarePlanRoundTrip(t *testing.T) {
	plan := BuildPostMICarePlan("case-rt-mi", "pt-rt-mi", cardiacNow)
	got, err := DecodeCarePlan(EncodeCarePlan(plan))
	if err != nil {
		t.Fatal(err)
	}
	if got.Condition != "post_mi" || got.PatientID != "pt-rt-mi" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if len(got.Medications) != len(plan.Medications) {
		t.Errorf("medications count after round-trip: want %d, got %d", len(plan.Medications), len(got.Medications))
	}
}

func TestPostStrokeIschemicCarePlanRoundTrip(t *testing.T) {
	plan := BuildPostStrokeCarePlan("case-rt-stroke", "pt-rt-stroke", "ischemic", cardiacNow)
	got, err := DecodeCarePlan(EncodeCarePlan(plan))
	if err != nil {
		t.Fatal(err)
	}
	if got.Condition != "post_stroke_ischemic" || got.PatientID != "pt-rt-stroke" {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}
