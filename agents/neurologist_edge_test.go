package agents

import (
	"testing"
)

func TestBuildSeizureDiagnosis(t *testing.T) {
	cases := []struct {
		seizureType string
		wantNonEmpty bool
	}{
		{"generalized tonic-clonic", true},
		{"absence seizure", true},
		{"focal onset", true},
		{"undetermined onset", true},
		{"", true}, // default branch
	}
	for _, tc := range cases {
		got := buildSeizureDiagnosis(tc.seizureType)
		if tc.wantNonEmpty && got == "" {
			t.Errorf("buildSeizureDiagnosis(%q) returned empty", tc.seizureType)
		}
	}
}

func TestEstimateNIHSSAllPathways(t *testing.T) {
	// Test with provided NIHSS score.
	score := 15
	req := neuroConsultRequest{NIHSSScore: &score}
	got := estimateNIHSS(req)
	if got != 15 {
		t.Errorf("provided NIHSS=%d, want 15", got)
	}

	// Test hemiplegia (motor=4).
	req = neuroConsultRequest{Presentation: "Complete paralysis hemiplegia on right side"}
	got = estimateNIHSS(req)
	if got < 4 {
		t.Errorf("hemiplegia NIHSS=%d, want >= 4", got)
	}

	// Test severe hemiparesis (motor=3).
	req = neuroConsultRequest{Presentation: "severe arm weakness"}
	got = estimateNIHSS(req)
	if got < 3 {
		t.Errorf("severe hemiparesis NIHSS=%d, want >= 3", got)
	}

	// Test mild arm weakness (motor=1).
	req = neuroConsultRequest{Presentation: "mild arm weakness"}
	got = estimateNIHSS(req)
	if got != 1 {
		t.Errorf("mild arm weakness NIHSS=%d, want 1", got)
	}

	// Test gaze deviation.
	req = neuroConsultRequest{Presentation: "gaze deviation to the left"}
	got = estimateNIHSS(req)
	if got < 2 {
		t.Errorf("gaze deviation NIHSS=%d, want >= 2", got)
	}

	// Test hemianopia.
	req = neuroConsultRequest{Presentation: "hemianopia right visual field"}
	got = estimateNIHSS(req)
	if got < 2 {
		t.Errorf("hemianopia NIHSS=%d, want >= 2", got)
	}

	// Test vision loss (visual=1, not hemianopia).
	req = neuroConsultRequest{Presentation: "sudden vision loss"}
	got = estimateNIHSS(req)
	if got < 1 {
		t.Errorf("vision loss NIHSS=%d, want >= 1", got)
	}

	// Test ataxia.
	req = neuroConsultRequest{Presentation: "ataxia bilateral"}
	got = estimateNIHSS(req)
	if got < 1 {
		t.Errorf("ataxia NIHSS=%d, want >= 1", got)
	}

	// Test sensory loss.
	req = neuroConsultRequest{Presentation: "hemisensory loss right side"}
	got = estimateNIHSS(req)
	if got < 1 {
		t.Errorf("sensory NIHSS=%d, want >= 1", got)
	}

	// Test consciousness levels.
	req = neuroConsultRequest{Presentation: "unconscious patient"}
	got = estimateNIHSS(req)
	if got < 3 {
		t.Errorf("unconscious NIHSS=%d, want >= 3", got)
	}

	req = neuroConsultRequest{Presentation: "stupor obtunded"}
	got = estimateNIHSS(req)
	if got < 2 {
		t.Errorf("stupor NIHSS=%d, want >= 2", got)
	}

	req = neuroConsultRequest{Presentation: "drowsy but arousable"}
	got = estimateNIHSS(req)
	if got >= 2 {
		t.Errorf("drowsy NIHSS=%d, want < 2", got)
	}

	// Test numbness (sensory).
	req = neuroConsultRequest{Presentation: "numbness on left side"}
	got = estimateNIHSS(req)
	if got < 1 {
		t.Errorf("numbness NIHSS=%d, want >= 1", got)
	}
}

func TestClassifySeizureAllTypes(t *testing.T) {
	cases := []struct {
		pres string
		want string
	}{
		{"tonic-clonic seizure", "generalized tonic-clonic"},
		{"tonic clonic episode", "generalized tonic-clonic"},
		{"grand mal seizure", "generalized tonic-clonic"},
		{"generalized seizure", "generalized tonic-clonic"},
		{"generalised seizure", "generalized tonic-clonic"},
		{"absence spell", "absence seizure"},
		{"staring spell", "absence seizure"},
		{"staring episode", "absence seizure"},
		{"petit mal seizure", "absence seizure"},
		{"focal seizure", "focal onset"},
		{"partial seizure", "focal onset"},
		{"focal onset seizure", "focal onset"},
		{"focal with impaired awareness", "focal onset"},
		{"complex partial seizure", "focal onset"},
		{"simple partial seizure", "focal onset"},
		{"seizure of unknown type", "undetermined onset"},
		{"convulsion episode", "undetermined onset"},
		{"no issues here", ""},
		{"headache only", ""},
	}
	for _, tc := range cases {
		got := classifySeizure(tc.pres)
		if got != tc.want {
			t.Errorf("classifySeizure(%q)=%q, want %q", tc.pres, got, tc.want)
		}
	}
}

func TestInterpretNeurologySeizureOnly(t *testing.T) {
	req := neuroConsultRequest{
		PatientID:    "p1",
		Presentation: "Patient had a tonic-clonic seizure with provoked hypoglycemia",
	}
	report := interpretNeurology(req)
	if report.SeizureType != "generalized tonic-clonic" {
		t.Errorf("seizure_type=%s", report.SeizureType)
	}
	if !report.Urgent {
		t.Error("seizure should be urgent")
	}
	if report.Diagnosis == "" {
		t.Error("diagnosis should not be empty")
	}
}

func TestInterpretNeurologyNoFindings(t *testing.T) {
	req := neuroConsultRequest{
		PatientID:    "p1",
		Presentation: "routine headache follow-up",
	}
	report := interpretNeurology(req)
	if report.Urgent {
		t.Error("no findings should not be urgent")
	}
	if report.Diagnosis == "" {
		t.Error("fallback diagnosis should not be empty")
	}
}

func TestInterpretNeurologyStrokeWithBPContra(t *testing.T) {
	bp := 190
	req := neuroConsultRequest{
		PatientID:    "p1",
		Presentation: "facial droop and arm weakness with aphasia",
		OnsetHours:   2.0,
		BPSystolic:   &bp,
	}
	report := interpretNeurology(req)
	if report.TPAEligible {
		t.Error("BP > 185 should make tPA ineligible")
	}
}

func TestInterpretNeurologyStrokeOnsetUnknown(t *testing.T) {
	req := neuroConsultRequest{
		PatientID:    "p1",
		Presentation: "facial droop, arm weakness",
		OnsetHours:   0,
	}
	report := interpretNeurology(req)
	if report.TPAEligible {
		t.Error("unknown onset should not be tPA eligible")
	}
}

func TestInterpretNeurologyStrokeTypes(t *testing.T) {
	// Cardioembolic.
	req := neuroConsultRequest{
		PatientID:    "p1",
		Presentation: "stroke with atrial fibrillation, arm weakness",
	}
	report := interpretNeurology(req)
	if report.StrokeType != "cardioembolic" {
		t.Errorf("stroke_type=%s, want cardioembolic", report.StrokeType)
	}

	// Large vessel.
	req = neuroConsultRequest{
		PatientID:    "p1",
		Presentation: "stroke with carotid stenosis, arm weakness",
	}
	report = interpretNeurology(req)
	if report.StrokeType != "large vessel atherosclerosis" {
		t.Errorf("stroke_type=%s, want large vessel", report.StrokeType)
	}

	// Small vessel.
	req = neuroConsultRequest{
		PatientID:    "p1",
		Presentation: "stroke pure motor deficit lacunar syndrome, arm weakness",
	}
	report = interpretNeurology(req)
	if report.StrokeType != "small vessel" {
		t.Errorf("stroke_type=%s, want small vessel", report.StrokeType)
	}
}

func TestInterpretNeurologyHyperdenseSign(t *testing.T) {
	req := neuroConsultRequest{
		PatientID:    "p1",
		Presentation: "stroke facial droop arm weakness",
		Imaging:      "CT shows hyperdense MCA sign",
	}
	report := interpretNeurology(req)
	found := false
	for _, rec := range report.Recommendations {
		if containsSubstr(rec, "Hyperdense") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected hyperdense recommendation")
	}
}

func TestInterpretNeurologyStrokePlusSe(t *testing.T) {
	req := neuroConsultRequest{
		PatientID:    "p1",
		Presentation: "stroke arm weakness with concurrent seizure activity",
	}
	report := interpretNeurology(req)
	if report.SeizureType == "" {
		t.Error("should classify seizure")
	}
	// Diagnosis should mention both.
	if !containsSubstr(report.Diagnosis, "seizure") {
		t.Error("diagnosis should mention concurrent seizure")
	}
}

func TestInterpretNeurologyBPContraWithoutTPAEligible(t *testing.T) {
	// BP > 185 without being in tPA window - should still get BP recommendation.
	bp := 200
	req := neuroConsultRequest{
		PatientID:    "p1",
		Presentation: "facial droop, arm weakness",
		OnsetHours:   6.0, // outside tPA window
		BPSystolic:   &bp,
	}
	report := interpretNeurology(req)
	if report.TPAEligible {
		t.Error("outside tPA window should not be eligible")
	}
	found := false
	for _, rec := range report.Recommendations {
		if containsSubstr(rec, "BP >185") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected BP recommendation")
	}
}
