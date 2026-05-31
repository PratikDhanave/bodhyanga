package agents

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent"
)

// cardioConsultMsg builds an agent.Message for a cardiology_consult.
// Mirrors the helper pattern used by cdm_planner_test.go so the test
// style is consistent across the agents package.
func cardioConsultMsg(req cardioConsultRequest) agent.Message {
	body, _ := json.Marshal(req)
	return agent.NewMessage("test", "cardiologist", agent.RoleUser,
		"cardiology_consult", string(body),
		map[string]any{"patient_id": req.PatientID, "case_id": req.CaseID})
}

// TestCardiologistAgentInterface verifies the three identity methods.
func TestCardiologistAgentInterface(t *testing.T) {
	a := &CardiologistAgent{}
	if a.ID() != "cardiologist" {
		t.Errorf("ID = %q, want %q", a.ID(), "cardiologist")
	}
	if a.Name() != "Cardiology Specialist" {
		t.Errorf("Name = %q, want %q", a.Name(), "Cardiology Specialist")
	}
	caps := a.Capabilities()
	wantCaps := map[string]bool{"ecg_interpretation": false, "cardiac_risk_assessment": false}
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

// TestCardiologistHandleMessageRouting verifies HandleMessage emits a
// cardiology_report addressed to "supervisor" with non-empty content.
func TestCardiologistHandleMessageRouting(t *testing.T) {
	a := &CardiologistAgent{}
	req := cardioConsultRequest{
		PatientID:   "p1",
		CaseID:      "c1",
		ECGFindings: "Normal sinus rhythm",
		Troponin:    0.01,
		BNP:         50,
		EFPercent:   65,
	}
	out, err := a.HandleMessage(context.Background(), cardioConsultMsg(req), newFakeEnv())
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
	if msg.Type != "cardiology_report" {
		t.Errorf("Type = %q, want %q", msg.Type, "cardiology_report")
	}
	if msg.Content == "" {
		t.Error("Content must be non-empty")
	}
}

// TestCardiologistHandleMessageInvalidJSON verifies that malformed content
// returns an error so the orchestrator can emit a KindAgentError audit event.
func TestCardiologistHandleMessageInvalidJSON(t *testing.T) {
	a := &CardiologistAgent{}
	bad := agent.NewMessage("test", "cardiologist", agent.RoleUser,
		"cardiology_consult", "{not valid json", nil)
	_, err := a.HandleMessage(context.Background(), bad, newFakeEnv())
	if err == nil {
		t.Error("expected error for invalid JSON content, got nil")
	}
}

// TestCardiologistSTEMIDetection is the primary safety-critical test.
// The STEMI path sets stemi_equivalent=true and urgent=true and must
// recommend cath lab activation.
func TestCardiologistSTEMIDetection(t *testing.T) {
	cases := []struct {
		name        string
		ecgFindings string
		wantSTEMI   bool
		wantUrgent  bool
	}{
		{
			name:        "classic anterior STEMI",
			ecgFindings: "ST elevation V1-V4, reciprocal ST depression II III aVF",
			wantSTEMI:   true,
			wantUrgent:  true,
		},
		{
			name:        "inferior STEMI with reciprocal changes",
			ecgFindings: "ST elevation II III aVF, reciprocal ST depression V1 V2, possible posterior extension",
			wantSTEMI:   true,
			wantUrgent:  true,
		},
		{
			name:        "STEMI mixed case in ECG string",
			ecgFindings: "Marked ST Elevation in leads I and aVL consistent with lateral STEMI",
			wantSTEMI:   true,
			wantUrgent:  true,
		},
		{
			name:        "ST depression only — NOT STEMI",
			ecgFindings: "ST depression V4-V6 with T-wave inversion, no elevation",
			wantSTEMI:   false,
			wantUrgent:  false, // no other triggers
		},
		{
			name:        "normal sinus rhythm — NOT STEMI",
			ecgFindings: "Normal sinus rhythm, no ST changes",
			wantSTEMI:   false,
			wantUrgent:  false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := cardioConsultRequest{
				PatientID:   "p1",
				CaseID:      "c1",
				ECGFindings: tc.ecgFindings,
				Troponin:    0.01, // sub-threshold to isolate ECG rule
				BNP:         50,
				EFPercent:   60,
			}
			report := interpretCardiology(req)
			if report.STEMIEquivalent != tc.wantSTEMI {
				t.Errorf("stemi_equivalent = %v, want %v (ecg: %q)", report.STEMIEquivalent, tc.wantSTEMI, tc.ecgFindings)
			}
			if report.Urgent != tc.wantUrgent {
				t.Errorf("urgent = %v, want %v (ecg: %q)", report.Urgent, tc.wantUrgent, tc.ecgFindings)
			}
			if tc.wantSTEMI {
				cathlabFound := false
				for _, r := range report.Recommendations {
					if strings.Contains(strings.ToLower(r), "cath lab") {
						cathlabFound = true
					}
				}
				if !cathlabFound {
					t.Errorf("STEMI case missing cath lab recommendation: %v", report.Recommendations)
				}
			}
			if report.Interpretation == "" {
				t.Error("Interpretation must be non-empty")
			}
		})
	}
}

// TestCardiologistNSTEMITroponin verifies that an elevated troponin
// (>0.04 ng/mL) flags the case as urgent and recommends serial biomarkers.
func TestCardiologistNSTEMITroponin(t *testing.T) {
	cases := []struct {
		name       string
		troponin   float64
		wantUrgent bool
	}{
		{"sub-threshold troponin 0.01", 0.01, false},
		{"at threshold 0.04", 0.04, false},   // strictly greater than, not >=
		{"above threshold 0.05", 0.05, true},
		{"markedly elevated 2.0", 2.0, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := cardioConsultRequest{
				PatientID:   "p1",
				ECGFindings: "Normal sinus rhythm", // no ECG triggers
				Troponin:    tc.troponin,
				BNP:         50,
				EFPercent:   60,
			}
			report := interpretCardiology(req)
			if report.Urgent != tc.wantUrgent {
				t.Errorf("troponin=%.3f: urgent=%v, want %v", tc.troponin, report.Urgent, tc.wantUrgent)
			}
			if tc.wantUrgent {
				cathFound := false
				for _, r := range report.Recommendations {
					if strings.Contains(strings.ToLower(r), "cath lab") || strings.Contains(strings.ToLower(r), "serial troponin") {
						cathFound = true
					}
				}
				if !cathFound {
					t.Errorf("elevated troponin case missing ACS recommendation: %v", report.Recommendations)
				}
			}
		})
	}
}

// TestCardiologistCHFClassification verifies HFrEF and decompensated CHF
// classification from EF and BNP values.
func TestCardiologistCHFClassification(t *testing.T) {
	cases := []struct {
		name      string
		ef        float64
		bnp       float64
		wantClass string // substring match
		wantUrgent bool
	}{
		{
			name:      "normal EF normal BNP — no CHF",
			ef:        65,
			bnp:       50,
			wantClass: "", // empty
			wantUrgent: false,
		},
		{
			name:      "HFrEF EF 30% normal BNP",
			ef:        30,
			bnp:       120,
			wantClass: "HFrEF",
			wantUrgent: false, // EF alone does not set urgent
		},
		{
			name:      "HFrEF EF 35% decompensated BNP 850",
			ef:        35,
			bnp:       850,
			wantClass: "decompensated",
			wantUrgent: true, // BNP >400 sets urgent
		},
		{
			name:      "HFpEF / decompensated BNP 500 normal EF",
			ef:        55,
			bnp:       500,
			wantClass: "decompensated",
			wantUrgent: true,
		},
		{
			name:      "borderline EF 39.9% just below threshold",
			ef:        39.9,
			bnp:       100,
			wantClass: "HFrEF",
			wantUrgent: false,
		},
		{
			name:      "EF 0 (not provided) BNP normal — no CHF",
			ef:        0, // sentinel: EFPercent == 0 means not provided
			bnp:       50,
			wantClass: "",
			wantUrgent: false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := cardioConsultRequest{
				PatientID:   "p1",
				ECGFindings: "Normal sinus rhythm",
				Troponin:    0.01,
				BNP:         tc.bnp,
				EFPercent:   tc.ef,
			}
			report := interpretCardiology(req)

			if tc.wantClass == "" {
				if report.HeartFailureClass != "" {
					t.Errorf("heart_failure_class = %q, want empty", report.HeartFailureClass)
				}
			} else {
				if !strings.Contains(report.HeartFailureClass, tc.wantClass) {
					t.Errorf("heart_failure_class = %q, want substring %q", report.HeartFailureClass, tc.wantClass)
				}
			}

			if report.Urgent != tc.wantUrgent {
				t.Errorf("urgent = %v, want %v", report.Urgent, tc.wantUrgent)
			}
		})
	}
}

// TestCardiologistAFib verifies atrial fibrillation detection and the
// CHA₂DS₂-VASc / rate-control recommendation.
func TestCardiologistAFib(t *testing.T) {
	cases := []struct {
		name string
		ecg  string
	}{
		{"explicit atrial fibrillation", "Atrial fibrillation with rapid ventricular response, rate 130 bpm"},
		{"lowercase afib abbreviation", "afib with RVR"},
		{"trailing space af pattern", "af with controlled ventricular rate"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := cardioConsultRequest{
				PatientID:   "p1",
				ECGFindings: tc.ecg,
				Troponin:    0.01,
				BNP:         50,
				EFPercent:   60,
			}
			report := interpretCardiology(req)
			anticoagFound := false
			for _, r := range report.Recommendations {
				if strings.Contains(strings.ToLower(r), "anticoagulation") ||
					strings.Contains(strings.ToLower(r), "cha") {
					anticoagFound = true
				}
			}
			if !anticoagFound {
				t.Errorf("AFib case missing anticoagulation recommendation: %v", report.Recommendations)
			}
		})
	}
}

// TestCardiologistCompleteHeartBlock verifies the urgent pacing path.
func TestCardiologistCompleteHeartBlock(t *testing.T) {
	cases := []struct {
		name string
		ecg  string
	}{
		{"complete heart block phrase", "Complete heart block with junctional escape rhythm at 38 bpm"},
		{"third degree phrase", "Third degree AV block with broad complex escape"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := cardioConsultRequest{
				PatientID:   "p1",
				ECGFindings: tc.ecg,
				Troponin:    0.01,
				BNP:         50,
				EFPercent:   60,
			}
			report := interpretCardiology(req)
			if !report.Urgent {
				t.Error("complete heart block must set urgent=true")
			}
			pacingFound := false
			for _, r := range report.Recommendations {
				if strings.Contains(strings.ToLower(r), "pac") {
					pacingFound = true
				}
			}
			if !pacingFound {
				t.Errorf("heart block case missing pacing recommendation: %v", report.Recommendations)
			}
		})
	}
}

// TestCardiologistRiskScore verifies the simplified TIMI-like score
// increases with each additional risk factor and stays ≤7.
func TestCardiologistRiskScore(t *testing.T) {
	// No risk factors at all → score 0.
	base := cardioConsultRequest{
		PatientID:   "p1",
		ECGFindings: "Normal sinus rhythm",
		Troponin:    0.01,
		BNP:         50,
		EFPercent:   60,
	}
	r0 := interpretCardiology(base)
	if r0.RiskScore != 0 {
		t.Errorf("base case score = %.0f, want 0", r0.RiskScore)
	}

	// Add troponin >0.04 → score 1.
	r1 := interpretCardiology(cardioConsultRequest{
		ECGFindings: "Normal sinus rhythm",
		Troponin:    0.1,
		BNP:         50,
		EFPercent:   60,
	})
	if r1.RiskScore < 1 {
		t.Errorf("troponin elevation should add ≥1 to score, got %.0f", r1.RiskScore)
	}

	// All triggers → score capped at 7.
	rMax := interpretCardiology(cardioConsultRequest{
		ECGFindings: "ST elevation II III, atrial fibrillation, complete heart block",
		Troponin:    2.0, // >0.04 AND >1.0 → 2 points
		BNP:         500,
		EFPercent:   30,
	})
	if rMax.RiskScore > 7 {
		t.Errorf("risk score should be capped at 7, got %.0f", rMax.RiskScore)
	}
	if rMax.RiskScore < 5 {
		t.Errorf("all-triggers case should score ≥5, got %.0f", rMax.RiskScore)
	}
}

// TestCardiologistNormalFindings verifies the no-pathology fallback path
// returns a non-urgent report with a non-empty interpretation and at
// least one routine recommendation.
func TestCardiologistNormalFindings(t *testing.T) {
	req := cardioConsultRequest{
		PatientID:   "p1",
		CaseID:      "c1",
		ECGFindings: "Normal sinus rhythm at 72 bpm. No ST-T changes. Normal axis.",
		Troponin:    0.01,
		BNP:         45,
		EFPercent:   65,
	}
	report := interpretCardiology(req)
	if report.STEMIEquivalent {
		t.Error("normal case: stemi_equivalent must be false")
	}
	if report.Urgent {
		t.Error("normal case: urgent must be false")
	}
	if report.HeartFailureClass != "" {
		t.Errorf("normal case: heart_failure_class must be empty, got %q", report.HeartFailureClass)
	}
	if report.Interpretation == "" {
		t.Error("interpretation must be non-empty even for normal findings")
	}
	if len(report.Recommendations) == 0 {
		t.Error("at least one routine recommendation expected for normal findings")
	}
}

// TestCardiologistRecommendationsDeduplicated verifies that when multiple
// rules fire they do not produce duplicate recommendation strings.
func TestCardiologistRecommendationsDeduplicated(t *testing.T) {
	// STEMI + elevated troponin is a common combination; ensure the
	// recommendation list has no exact duplicates.
	req := cardioConsultRequest{
		PatientID:   "p1",
		ECGFindings: "ST elevation V1-V4 with reciprocal changes, sinus tachycardia",
		Troponin:    5.0,
		BNP:         200,
		EFPercent:   40,
	}
	report := interpretCardiology(req)
	seen := make(map[string]int)
	for _, r := range report.Recommendations {
		seen[r]++
		if seen[r] > 1 {
			t.Errorf("duplicate recommendation: %q", r)
		}
	}
}

// TestCardiologistMetadataPropagation verifies that patient_id and
// case_id are copied into the outbound message metadata.
func TestCardiologistMetadataPropagation(t *testing.T) {
	a := &CardiologistAgent{}
	req := cardioConsultRequest{
		PatientID:   "patient-xyz",
		CaseID:      "case-abc",
		ECGFindings: "Normal sinus rhythm",
		Troponin:    0.01,
		BNP:         50,
		EFPercent:   65,
	}
	out, err := a.HandleMessage(context.Background(), cardioConsultMsg(req), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage err: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 output, got %d", len(out))
	}
	if pid, _ := out[0].Metadata["patient_id"].(string); pid != "patient-xyz" {
		t.Errorf("metadata patient_id = %q, want %q", pid, "patient-xyz")
	}
	if cid, _ := out[0].Metadata["case_id"].(string); cid != "case-abc" {
		t.Errorf("metadata case_id = %q, want %q", cid, "case-abc")
	}
}
