package agents

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent"
)

// intPtr is a test helper that returns a pointer to an int literal.
func intPtr(v int) *int { return &v }

// float64Ptr is a test helper that returns a pointer to a float64 literal.
func float64Ptr(v float64) *float64 { return &v }

// neuroConsultMsg builds an agent.Message for a neurology_consult.
// Mirrors the helper pattern used by cardiologist_test.go.
func neuroConsultMsg(req neuroConsultRequest) agent.Message {
	body, _ := json.Marshal(req)
	return agent.NewMessage("test", "neurologist", agent.RoleUser,
		"neurology_consult", string(body),
		map[string]any{"patient_id": req.PatientID, "case_id": req.CaseID})
}

// TestNeurologistAgentInterface verifies the three identity methods.
func TestNeurologistAgentInterface(t *testing.T) {
	a := &NeurologistAgent{}
	if a.ID() != "neurologist" {
		t.Errorf("ID = %q, want %q", a.ID(), "neurologist")
	}
	if a.Name() != "Neurology Specialist" {
		t.Errorf("Name = %q, want %q", a.Name(), "Neurology Specialist")
	}
	caps := a.Capabilities()
	wantCaps := map[string]bool{
		"stroke_assessment":      false,
		"seizure_classification": false,
		"neuro_scoring":          false,
	}
	for _, c := range caps {
		if _, ok := wantCaps[c]; ok {
			wantCaps[c] = true
		}
	}
	for cap, found := range wantCaps {
		if !found {
			t.Errorf("Capabilities() missing %q", cap)
		}
	}
}

// TestNeurologistHandleMessageRouting verifies HandleMessage emits a
// neurology_report addressed to "supervisor" with non-empty content.
func TestNeurologistHandleMessageRouting(t *testing.T) {
	a := &NeurologistAgent{}
	req := neuroConsultRequest{
		PatientID:    "p1",
		CaseID:       "c1",
		Presentation: "Headache, no focal deficits",
		OnsetHours:   2,
	}
	out, err := a.HandleMessage(context.Background(), neuroConsultMsg(req), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage err: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 output message, got %d", len(out))
	}
	msg := out[0]
	if msg.To != "supervisor" {
		t.Errorf("To = %q, want %q", msg.To, "supervisor")
	}
	if msg.Type != "neurology_report" {
		t.Errorf("Type = %q, want %q", msg.Type, "neurology_report")
	}
	if msg.Content == "" {
		t.Error("Content must be non-empty")
	}
}

// TestNeurologistHandleMessageInvalidJSON verifies that malformed content
// returns an error.
func TestNeurologistHandleMessageInvalidJSON(t *testing.T) {
	a := &NeurologistAgent{}
	bad := agent.NewMessage("test", "neurologist", agent.RoleUser,
		"neurology_consult", "{not valid json", nil)
	_, err := a.HandleMessage(context.Background(), bad, newFakeEnv())
	if err == nil {
		t.Error("expected error for invalid JSON content, got nil")
	}
}

// TestNeurologistStrokeDetectionFAST verifies that each FAST+ criterion
// independently triggers the stroke pathway (urgent=true, CT recommendation).
func TestNeurologistStrokeDetectionFAST(t *testing.T) {
	cases := []struct {
		name         string
		presentation string
	}{
		{"facial droop", "Patient presents with sudden facial droop and confusion"},
		{"arm weakness", "Acute onset arm weakness, right side, since 30 minutes ago"},
		{"speech difficulty", "Sudden speech difficulty, cannot form words"},
		{"sudden headache thunderclap", "Worst headache of life, sudden onset, thunderclap"},
		{"aphasia", "Global aphasia with right-sided weakness"},
		{"slurred speech", "Slurred speech and unsteady gait"},
		{"hemiparesis", "Left hemiparesis noted on exam, duration 1 hour"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := neuroConsultRequest{
				PatientID:    "p1",
				CaseID:       "c1",
				Presentation: tc.presentation,
				OnsetHours:   1.0,
			}
			report := interpretNeurology(req)
			if !report.Urgent {
				t.Errorf("presentation %q: urgent must be true for stroke criterion", tc.presentation)
			}
			ctFound := false
			for _, r := range report.Recommendations {
				if strings.Contains(strings.ToLower(r), "ct") {
					ctFound = true
				}
			}
			if !ctFound {
				t.Errorf("stroke case missing CT recommendation: %v", report.Recommendations)
			}
			if report.Diagnosis == "" {
				t.Error("Diagnosis must be non-empty")
			}
		})
	}
}

// TestNeurologistTPAEligibility verifies tPA eligibility based on onset_hours.
func TestNeurologistTPAEligibility(t *testing.T) {
	cases := []struct {
		name         string
		onsetHours   float64
		wantEligible bool
	}{
		{"onset 1h — within window", 1.0, true},
		{"onset 3h — within window", 3.0, true},
		{"onset 4.4h — just within", 4.4, true},
		{"onset 4.5h — at boundary not eligible", 4.5, false},
		{"onset 6h — outside window", 6.0, false},
		{"onset 0 unknown", 0, false}, // unknown onset: not auto-eligible
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := neuroConsultRequest{
				PatientID:    "p1",
				Presentation: "Acute facial droop and arm weakness",
				OnsetHours:   tc.onsetHours,
			}
			report := interpretNeurology(req)
			if report.TPAEligible != tc.wantEligible {
				t.Errorf("onset=%.1fh: tpa_eligible=%v, want %v", tc.onsetHours, report.TPAEligible, tc.wantEligible)
			}
		})
	}
}

// TestNeurologistBPContraindication verifies that BP >185 marks tPA
// as not eligible even if within the time window.
func TestNeurologistBPContraindication(t *testing.T) {
	req := neuroConsultRequest{
		PatientID:    "p1",
		Presentation: "Sudden arm weakness and speech difficulty",
		OnsetHours:   2.0,
		BPSystolic:   intPtr(200),
	}
	report := interpretNeurology(req)
	if report.TPAEligible {
		t.Error("BP >185 should mark tPA as not eligible")
	}
	bpFound := false
	for _, r := range report.Recommendations {
		if strings.Contains(strings.ToLower(r), "bp") || strings.Contains(strings.ToLower(r), "labetalol") || strings.Contains(strings.ToLower(r), "nicardipine") {
			bpFound = true
		}
	}
	if !bpFound {
		t.Errorf("BP contraindication case missing BP management recommendation: %v", report.Recommendations)
	}
}

// TestNeurologistLVODetection verifies LVO is suspected when NIHSS ≥ 6.
func TestNeurologistLVODetection(t *testing.T) {
	cases := []struct {
		name    string
		nihss   int
		wantLVO bool
	}{
		{"NIHSS 5 — below LVO threshold", 5, false},
		{"NIHSS 6 — at LVO threshold", 6, true},
		{"NIHSS 15 — high NIHSS", 15, true},
		{"NIHSS 0 — no deficit", 0, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := neuroConsultRequest{
				PatientID:    "p1",
				Presentation: "Sudden facial droop and arm weakness",
				OnsetHours:   2.0,
				NIHSSScore:   intPtr(tc.nihss),
			}
			report := interpretNeurology(req)
			if report.LVOSuspected != tc.wantLVO {
				t.Errorf("NIHSS=%d: lvo_suspected=%v, want %v", tc.nihss, report.LVOSuspected, tc.wantLVO)
			}
			if report.NIHSSScore != tc.nihss {
				t.Errorf("nihss_score=%d, want %d", report.NIHSSScore, tc.nihss)
			}
		})
	}
}

// TestNeurologistThrombectomyWindow verifies thrombectomy window logic:
// LVO suspected AND onset < 24h (or unknown).
func TestNeurologistThrombectomyWindow(t *testing.T) {
	cases := []struct {
		name             string
		nihss            int
		onsetHours       float64
		wantThrombectomy bool
	}{
		{"NIHSS 10, onset 6h — in window", 10, 6.0, true},
		{"NIHSS 10, onset 23h — in window", 10, 23.0, true},
		{"NIHSS 10, onset 0 unknown — in window", 10, 0, true},
		{"NIHSS 10, onset 25h — outside window", 10, 25.0, false},
		{"NIHSS 3, onset 6h — no LVO no thrombectomy", 3, 6.0, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := neuroConsultRequest{
				PatientID:    "p1",
				Presentation: "Sudden hemiparesis and aphasia",
				OnsetHours:   tc.onsetHours,
				NIHSSScore:   intPtr(tc.nihss),
			}
			report := interpretNeurology(req)
			if report.ThrombectomyWindow != tc.wantThrombectomy {
				t.Errorf("nihss=%d onset=%.1fh: thrombectomy_window=%v, want %v",
					tc.nihss, tc.onsetHours, report.ThrombectomyWindow, tc.wantThrombectomy)
			}
		})
	}
}

// TestNeurologistStrokeTypeClassification verifies stroke type detection.
func TestNeurologistStrokeTypeClassification(t *testing.T) {
	cases := []struct {
		name         string
		presentation string
		wantType     string
	}{
		{
			name:         "cardioembolic — atrial fibrillation",
			presentation: "Sudden arm weakness; history of atrial fibrillation, not anticoagulated",
			wantType:     "cardioembolic",
		},
		{
			name:         "cardioembolic — afib abbreviation",
			presentation: "afib with rapid ventricular response; acute aphasia",
			wantType:     "cardioembolic",
		},
		{
			name:         "large vessel — carotid stenosis",
			presentation: "TIA last week; carotid stenosis 80% on duplex",
			wantType:     "large vessel atherosclerosis",
		},
		{
			name:         "small vessel — pure motor",
			presentation: "Pure motor hemiparesis, no cortical signs",
			wantType:     "small vessel",
		},
		{
			name:         "small vessel — ataxic hemiparesis",
			presentation: "Ataxic hemiparesis with dysarthria",
			wantType:     "small vessel",
		},
		{
			name:         "undetermined — no specific markers",
			presentation: "Arm weakness and speech difficulty, no known cardiac history",
			wantType:     "",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			result := classifyStrokeType(strings.ToLower(tc.presentation))
			if result != tc.wantType {
				t.Errorf("classifyStrokeType = %q, want %q (presentation: %q)",
					result, tc.wantType, tc.presentation)
			}
		})
	}
}

// TestNeurologistSeizureClassification verifies ILAE-inspired seizure typing.
func TestNeurologistSeizureClassification(t *testing.T) {
	cases := []struct {
		name         string
		presentation string
		wantType     string
	}{
		{
			name:         "generalized tonic-clonic",
			presentation: "Witnessed tonic-clonic seizure lasting 2 minutes, post-ictal confusion",
			wantType:     "generalized tonic-clonic",
		},
		{
			name:         "grand mal",
			presentation: "Grand mal seizure in the ED, no prior history",
			wantType:     "generalized tonic-clonic",
		},
		{
			name:         "absence — staring spell",
			presentation: "Episodes of staring spell, 10-15 seconds, no post-ictal phase",
			wantType:     "absence seizure",
		},
		{
			name:         "absence — absence keyword",
			presentation: "Absence seizures since childhood, currently uncontrolled",
			wantType:     "absence seizure",
		},
		{
			name:         "focal onset",
			presentation: "Focal seizure with right hand jerking, preserved awareness",
			wantType:     "focal onset",
		},
		{
			name:         "complex partial (legacy term → focal onset)",
			presentation: "Complex partial seizure with automatisms",
			wantType:     "focal onset",
		},
		{
			name:         "no seizure language",
			presentation: "Headache and dizziness",
			wantType:     "",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			result := classifySeizure(strings.ToLower(tc.presentation))
			if result != tc.wantType {
				t.Errorf("classifySeizure = %q, want %q (presentation: %q)",
					result, tc.wantType, tc.presentation)
			}
		})
	}
}

// TestNeurologistProvokedSeizure verifies provoked vs unprovoked distinction.
func TestNeurologistProvokedSeizure(t *testing.T) {
	cases := []struct {
		name         string
		presentation string
		wantProvoked bool
	}{
		{
			name:         "hypoglycaemia provoked",
			presentation: "First seizure in setting of hypoglycemia, glucose 1.8 mmol/L",
			wantProvoked: true,
		},
		{
			name:         "alcohol withdrawal provoked",
			presentation: "Tonic-clonic seizure; alcohol withdrawal, last drink 48 hours ago",
			wantProvoked: true,
		},
		{
			name:         "febrile seizure",
			presentation: "Febrile seizure in 2-year-old, temperature 39.5C",
			wantProvoked: true,
		},
		{
			name:         "unprovoked first seizure",
			presentation: "First seizure tonic-clonic, no provoking factor identified, normal glucose",
			wantProvoked: false,
		},
		{
			name:         "unprovoked epilepsy",
			presentation: "Known epilepsy, breakthrough absence seizure",
			wantProvoked: false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			result := isProvokedSeizure(strings.ToLower(tc.presentation))
			if result != tc.wantProvoked {
				t.Errorf("isProvokedSeizure = %v, want %v (presentation: %q)",
					result, tc.wantProvoked, tc.presentation)
			}
		})
	}
}

// TestNeurologistNIHSSEstimation verifies that the keyword-based NIHSS
// estimator returns reasonable scores (not zero for deficit presentations,
// respects the provided value when given).
func TestNeurologistNIHSSEstimation(t *testing.T) {
	// Provided value should be used as-is.
	nihssVal := 12
	req := neuroConsultRequest{
		PatientID:    "p1",
		Presentation: "some presentation",
		NIHSSScore:   &nihssVal,
	}
	score := estimateNIHSS(req)
	if score != 12 {
		t.Errorf("estimateNIHSS with provided value: got %d, want 12", score)
	}

	// No NIHSS provided, severe deficits → score should be > 0.
	req2 := neuroConsultRequest{
		PatientID:    "p2",
		Presentation: "Global aphasia with right hemiparesis and gaze deviation",
	}
	score2 := estimateNIHSS(req2)
	if score2 <= 0 {
		t.Errorf("estimateNIHSS for severe deficits: got %d, want >0", score2)
	}

	// Mild headache only → estimated score should be 0.
	req3 := neuroConsultRequest{
		PatientID:    "p3",
		Presentation: "Mild headache, no neurological deficits",
	}
	score3 := estimateNIHSS(req3)
	if score3 != 0 {
		t.Errorf("estimateNIHSS for headache only: got %d, want 0", score3)
	}

	// Score must never exceed 42.
	req4 := neuroConsultRequest{
		PatientID: "p4",
		Presentation: "Coma, complete hemiplegia, global aphasia, hemianopia, hemisensory loss, " +
			"gaze deviation, facial palsy, ataxia",
	}
	score4 := estimateNIHSS(req4)
	if score4 > 42 {
		t.Errorf("estimateNIHSS score %d exceeds NIHSS max 42", score4)
	}
}

// TestNeurologistNormalPresentation verifies the no-pathology fallback
// returns a non-urgent report with non-empty diagnosis and at least one
// routine recommendation.
func TestNeurologistNormalPresentation(t *testing.T) {
	req := neuroConsultRequest{
		PatientID:    "p1",
		CaseID:       "c1",
		Presentation: "Mild tension headache, no focal deficits, no prior neurological events",
		OnsetHours:   12,
	}
	report := interpretNeurology(req)
	if report.Urgent {
		t.Error("normal presentation: urgent must be false")
	}
	if report.LVOSuspected {
		t.Error("normal presentation: lvo_suspected must be false")
	}
	if report.TPAEligible {
		t.Error("normal presentation: tpa_eligible must be false")
	}
	if report.Diagnosis == "" {
		t.Error("Diagnosis must be non-empty even for normal presentation")
	}
	if len(report.Recommendations) == 0 {
		t.Error("at least one routine recommendation expected for normal presentation")
	}
}

// TestNeurologistRecommendationsDeduplicated verifies no duplicate
// recommendation strings when multiple rules fire.
func TestNeurologistRecommendationsDeduplicated(t *testing.T) {
	req := neuroConsultRequest{
		PatientID:    "p1",
		Presentation: "Sudden facial droop, arm weakness, atrial fibrillation known, tonic-clonic seizure",
		OnsetHours:   1.5,
		NIHSSScore:   intPtr(10),
	}
	report := interpretNeurology(req)
	seen := make(map[string]int)
	for _, r := range report.Recommendations {
		seen[r]++
		if seen[r] > 1 {
			t.Errorf("duplicate recommendation: %q", r)
		}
	}
}

// TestNeurologistMetadataPropagation verifies patient_id and case_id are
// propagated into the outbound message metadata.
func TestNeurologistMetadataPropagation(t *testing.T) {
	a := &NeurologistAgent{}
	req := neuroConsultRequest{
		PatientID:    "patient-neuro-1",
		CaseID:       "case-neuro-2",
		Presentation: "Headache and dizziness",
		OnsetHours:   4,
	}
	out, err := a.HandleMessage(context.Background(), neuroConsultMsg(req), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage err: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 output, got %d", len(out))
	}
	if pid, _ := out[0].Metadata["patient_id"].(string); pid != "patient-neuro-1" {
		t.Errorf("metadata patient_id = %q, want %q", pid, "patient-neuro-1")
	}
	if cid, _ := out[0].Metadata["case_id"].(string); cid != "case-neuro-2" {
		t.Errorf("metadata case_id = %q, want %q", cid, "case-neuro-2")
	}
}

// TestNeurologistOutputDeserializes verifies the output content JSON
// deserializes back into a neuroReport with the expected shape.
func TestNeurologistOutputDeserializes(t *testing.T) {
	a := &NeurologistAgent{}
	nihss := 8
	req := neuroConsultRequest{
		PatientID:    "p1",
		Presentation: "Sudden arm weakness and aphasia",
		OnsetHours:   2.0,
		NIHSSScore:   &nihss,
	}
	out, err := a.HandleMessage(context.Background(), neuroConsultMsg(req), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage err: %v", err)
	}
	var report neuroReport
	if err := json.Unmarshal([]byte(out[0].Content), &report); err != nil {
		t.Fatalf("output content not valid JSON neuroReport: %v", err)
	}
	if report.NIHSSScore != 8 {
		t.Errorf("nihss_score = %d, want 8", report.NIHSSScore)
	}
	if !report.Urgent {
		t.Error("stroke presentation should be urgent")
	}
	if !report.LVOSuspected {
		t.Error("NIHSS 8 should suspect LVO")
	}
	if !report.TPAEligible {
		t.Error("onset 2h should be tPA eligible")
	}
}
