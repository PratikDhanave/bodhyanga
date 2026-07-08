package agents

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent"
)

// nephroConsultMsg builds an agent.Message for a nephrology_consult.
// Mirrors the helper pattern used by cardiologist_test.go.
func nephroConsultMsg(req nephroConsultRequest) agent.Message {
	body, _ := json.Marshal(req)
	return agent.NewMessage("test", "nephrologist", agent.RoleUser,
		"nephrology_consult", string(body),
		map[string]any{"patient_id": req.PatientID, "case_id": req.CaseID})
}

// ptr returns a pointer to a float64 — convenience helper for optional fields.
func ptr(f float64) *float64 { return &f }

// TestNephrologistAgentInterface verifies the three identity methods.
func TestNephrologistAgentInterface(t *testing.T) {
	a := &NephrologistAgent{}
	if a.ID() != "nephrologist" {
		t.Errorf("ID = %q, want %q", a.ID(), "nephrologist")
	}
	if a.Name() != "Nephrology Specialist" {
		t.Errorf("Name = %q, want %q", a.Name(), "Nephrology Specialist")
	}
	wantCaps := map[string]bool{
		"ckd_staging":       false,
		"dialysis_adequacy": false,
		"aki_assessment":    false,
	}
	for _, c := range a.Capabilities() {
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

// TestNephrologistHandleMessageRouting verifies HandleMessage emits a
// nephrology_report addressed to "supervisor" with non-empty content.
func TestNephrologistHandleMessageRouting(t *testing.T) {
	a := &NephrologistAgent{}
	req := nephroConsultRequest{
		PatientID:      "p1",
		CaseID:         "c1",
		CreatinineMgDl: 1.0,
		Age:            45,
		Sex:            "male",
		WeightKg:       80,
	}
	out, err := a.HandleMessage(context.Background(), nephroConsultMsg(req), newFakeEnv())
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
	if msg.Type != "nephrology_report" {
		t.Errorf("Type = %q, want %q", msg.Type, "nephrology_report")
	}
	if msg.Content == "" {
		t.Error("Content must be non-empty")
	}
}

// TestNephrologistHandleMessageInvalidJSON verifies that malformed content
// returns an error so the orchestrator can emit a KindAgentError audit event.
func TestNephrologistHandleMessageInvalidJSON(t *testing.T) {
	a := &NephrologistAgent{}
	bad := agent.NewMessage("test", "nephrologist", agent.RoleUser,
		"nephrology_consult", "{not valid json", nil)
	_, err := a.HandleMessage(context.Background(), bad, newFakeEnv())
	if err == nil {
		t.Error("expected error for invalid JSON content, got nil")
	}
}

// TestNephrologistMetadataPropagation verifies that patient_id and
// case_id are copied into the outbound message metadata.
func TestNephrologistMetadataPropagation(t *testing.T) {
	a := &NephrologistAgent{}
	req := nephroConsultRequest{
		PatientID:      "patient-neph-xyz",
		CaseID:         "case-neph-abc",
		CreatinineMgDl: 1.0,
		Age:            50,
		Sex:            "female",
		WeightKg:       65,
	}
	out, err := a.HandleMessage(context.Background(), nephroConsultMsg(req), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage err: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 output, got %d", len(out))
	}
	if pid, _ := out[0].Metadata["patient_id"].(string); pid != "patient-neph-xyz" {
		t.Errorf("metadata patient_id = %q, want %q", pid, "patient-neph-xyz")
	}
	if cid, _ := out[0].Metadata["case_id"].(string); cid != "case-neph-abc" {
		t.Errorf("metadata case_id = %q, want %q", cid, "case-neph-abc")
	}
}

// TestNephrologistCalcEGFR verifies the CKD-EPI (2021) eGFR formula
// produces values in a physiologically plausible range for representative
// inputs. Reference values derived from the published CKD-EPI 2021 equation.
func TestNephrologistCalcEGFR(t *testing.T) {
	cases := []struct {
		name    string
		scr     float64
		age     int
		sex     string
		minEGFR float64
		maxEGFR float64
	}{
		// Young female, normal creatinine — expect high normal eGFR
		{"female 30y scr 0.8", 0.8, 30, "female", 95, 130},
		// Middle-aged male, normal creatinine
		{"male 50y scr 1.0", 1.0, 50, "male", 80, 105},
		// Older male, moderately elevated creatinine — expect G3a/G3b range
		{"male 65y scr 2.0", 2.0, 65, "male", 28, 42},
		// Severely elevated creatinine — expect G4/G5 range
		{"female 70y scr 4.0", 4.0, 70, "female", 8, 18},
		// Male, scr at kappa boundary (0.9) — check formula continuity
		{"male 40y scr 0.9", 0.9, 40, "male", 92, 115},
		// Female, scr at kappa boundary (0.7) — check formula continuity
		{"female 40y scr 0.7", 0.7, 40, "female", 100, 130},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			egfr := calcEGFR(tc.scr, tc.age, tc.sex)
			if egfr < tc.minEGFR || egfr > tc.maxEGFR {
				t.Errorf("calcEGFR(scr=%.1f, age=%d, sex=%s) = %.1f, want [%.0f, %.0f]",
					tc.scr, tc.age, tc.sex, egfr, tc.minEGFR, tc.maxEGFR)
			}
		})
	}
}

// TestNephrologistCalcEGFRInvalidInput verifies edge cases return 0.
func TestNephrologistCalcEGFRInvalidInput(t *testing.T) {
	if v := calcEGFR(0, 50, "male"); v != 0.0 {
		t.Errorf("calcEGFR(0, 50, male) = %.2f, want 0.0", v)
	}
	if v := calcEGFR(1.0, 0, "male"); v != 0.0 {
		t.Errorf("calcEGFR(1.0, 0, male) = %.2f, want 0.0", v)
	}
	if v := calcEGFR(-1.0, 50, "female"); v != 0.0 {
		t.Errorf("calcEGFR(-1.0, 50, female) = %.2f, want 0.0", v)
	}
}

// TestNephrologistCKDStaging verifies KDIGO G-stage boundaries.
func TestNephrologistCKDStaging(t *testing.T) {
	cases := []struct {
		name      string
		egfr      float64
		wantStage string
	}{
		{"G1 egfr 100", 100.0, "G1"},
		{"G1 egfr 90 boundary", 90.0, "G1"},
		{"G2 egfr 89", 89.0, "G2"},
		{"G2 egfr 60 boundary", 60.0, "G2"},
		{"G3a egfr 59", 59.0, "G3a"},
		{"G3a egfr 45 boundary", 45.0, "G3a"},
		{"G3b egfr 44", 44.0, "G3b"},
		{"G3b egfr 30 boundary", 30.0, "G3b"},
		{"G4 egfr 29", 29.0, "G4"},
		{"G4 egfr 15 boundary", 15.0, "G4"},
		{"G5 egfr 14", 14.0, "G5"},
		{"G5 egfr 5", 5.0, "G5"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			stage, _ := ckdStageFromEGFR(tc.egfr)
			if stage != tc.wantStage {
				t.Errorf("ckdStageFromEGFR(%.1f) = %q, want %q", tc.egfr, stage, tc.wantStage)
			}
		})
	}
}

// TestNephrologistDialysisIndicatedG5 verifies that CKD G5 (eGFR < 15)
// sets dialysis_indicated=true and urgent=true.
func TestNephrologistDialysisIndicatedG5(t *testing.T) {
	// Creatinine ~8 mg/dL in a 60-year-old male yields eGFR well below 15.
	req := nephroConsultRequest{
		PatientID:      "p1",
		CreatinineMgDl: 8.0,
		Age:            60,
		Sex:            "male",
		WeightKg:       75,
	}
	report := interpretNephrology(req)
	if report.CKDStage != "G5" {
		t.Errorf("CKDStage = %q, want G5 (eGFR=%.1f)", report.CKDStage, report.EGFRMlMin)
	}
	if !report.DialysisIndicated {
		t.Error("dialysis_indicated must be true for G5")
	}
	if !report.Urgent {
		t.Error("urgent must be true for G5")
	}
	// Must recommend dialysis evaluation
	dialysisFound := false
	for _, r := range report.Recommendations {
		if strings.Contains(strings.ToLower(r), "dialysis") {
			dialysisFound = true
		}
	}
	if !dialysisFound {
		t.Errorf("G5 case missing dialysis recommendation: %v", report.Recommendations)
	}
}

// TestNephrologistAKIStaging verifies AKI detection across all three
// KDIGO creatinine stages.
func TestNephrologistAKIStaging(t *testing.T) {
	cases := []struct {
		name         string
		scr          float64
		priorScr     float64
		wantAKIStage int
		wantUrgent   bool
		wantDialysis bool
	}{
		// Stage 0 — no AKI
		{"no AKI ratio 1.2", 1.2, 1.0, 0, false, false},
		// Stage 1 — ratio ≥1.5
		{"AKI1 ratio 1.5", 1.5, 1.0, 1, false, false},
		// Stage 1 — acute rise ≥0.3 mg/dL
		{"AKI1 rise 0.3", 1.3, 1.0, 1, false, false},
		// Stage 2 — ratio ≥2.0
		{"AKI2 ratio 2.0", 2.0, 1.0, 2, true, false},
		// Stage 3 — ratio ≥3.0
		{"AKI3 ratio 3.0", 3.0, 1.0, 3, true, true},
		// Stage 3 — absolute ≥4.0 mg/dL (even if ratio < 3)
		{"AKI3 absolute 4.0", 4.0, 2.0, 3, true, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := nephroConsultRequest{
				PatientID:       "p1",
				CreatinineMgDl:  tc.scr,
				PriorCreatinine: ptr(tc.priorScr),
				Age:             55,
				Sex:             "male",
				WeightKg:        80,
			}
			report := interpretNephrology(req)
			if report.AKIStage != tc.wantAKIStage {
				t.Errorf("AKIStage = %d, want %d (scr=%.1f, prior=%.1f)",
					report.AKIStage, tc.wantAKIStage, tc.scr, tc.priorScr)
			}
			if report.Urgent != tc.wantUrgent {
				t.Errorf("urgent = %v, want %v (AKI stage %d)", report.Urgent, tc.wantUrgent, tc.wantAKIStage)
			}
			if report.DialysisIndicated != tc.wantDialysis {
				t.Errorf("dialysis_indicated = %v, want %v (AKI stage %d)",
					report.DialysisIndicated, tc.wantDialysis, tc.wantAKIStage)
			}
		})
	}
}

// TestNephrologistAKIStage1AcuteRise verifies the 0.3 mg/dL acute rise
// criterion triggers AKI Stage 1 even when ratio is < 1.5.
func TestNephrologistAKIStage1AcuteRise(t *testing.T) {
	// scr 1.3, prior 1.0 → ratio 1.3 (< 1.5) but rise = 0.3 → AKI1
	req := nephroConsultRequest{
		PatientID:       "p1",
		CreatinineMgDl:  1.3,
		PriorCreatinine: ptr(1.0),
		Age:             60,
		Sex:             "female",
		WeightKg:        60,
	}
	report := interpretNephrology(req)
	if report.AKIStage != 1 {
		t.Errorf("AKIStage = %d, want 1 (acute rise 0.3 mg/dL criterion)", report.AKIStage)
	}
}

// TestNephrologistNoAKIWithoutBaseline verifies that AKI stage remains 0
// when no prior_creatinine is provided.
func TestNephrologistNoAKIWithoutBaseline(t *testing.T) {
	req := nephroConsultRequest{
		PatientID:      "p1",
		CreatinineMgDl: 3.5, // would be AKI3 if baseline known
		Age:            55,
		Sex:            "male",
		WeightKg:       80,
		// PriorCreatinine intentionally nil
	}
	report := interpretNephrology(req)
	if report.AKIStage != 0 {
		t.Errorf("AKIStage = %d, want 0 when no prior creatinine provided", report.AKIStage)
	}
}

// TestNephrologistProteinuriaClassification verifies KDIGO A-category
// assignment across A1/A2/A3 boundaries.
func TestNephrologistProteinuriaClassification(t *testing.T) {
	cases := []struct {
		name    string
		pcr     float64
		wantCat string
	}{
		{"A1 pcr 0.01", 0.01, "A1"},
		{"A1 pcr 0.029", 0.029, "A1"},
		{"A2 pcr 0.03 boundary", 0.03, "A2"},
		{"A2 pcr 0.15", 0.15, "A2"},
		{"A2 pcr 0.30 boundary", 0.30, "A2"},
		{"A3 pcr 0.31", 0.31, "A3"},
		{"A3 pcr 3.0", 3.0, "A3"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := nephroConsultRequest{
				PatientID:      "p1",
				CreatinineMgDl: 1.0,
				Age:            50,
				Sex:            "male",
				WeightKg:       80,
				UrineProtein:   ptr(tc.pcr),
			}
			report := interpretNephrology(req)
			if report.ProteinuriaCategory != tc.wantCat {
				t.Errorf("ProteinuriaCategory = %q, want %q (pcr=%.3f)",
					report.ProteinuriaCategory, tc.wantCat, tc.pcr)
			}
		})
	}
}

// TestNephrologistNoProteinuriaWhenFieldAbsent verifies that
// proteinuria_category is empty when urine_protein is not provided.
func TestNephrologistNoProteinuriaWhenFieldAbsent(t *testing.T) {
	req := nephroConsultRequest{
		PatientID:      "p1",
		CreatinineMgDl: 1.0,
		Age:            50,
		Sex:            "male",
		WeightKg:       80,
		// UrineProtein intentionally nil
	}
	report := interpretNephrology(req)
	if report.ProteinuriaCategory != "" {
		t.Errorf("ProteinuriaCategory = %q, want empty when urine_protein not provided",
			report.ProteinuriaCategory)
	}
}

// TestNephrologistHyperkalemia verifies potassium thresholds produce
// correct urgency levels and recommendations.
func TestNephrologistHyperkalemia(t *testing.T) {
	cases := []struct {
		name       string
		potassium  float64
		wantUrgent bool
		wantWord   string // substring to find in recommendations
	}{
		{"normal K 4.0", 4.0, false, ""},
		{"borderline K 5.4", 5.4, false, ""},
		{"urgent K 5.6", 5.6, true, "hyperkalaemia"},
		{"emergent K 6.6", 6.6, true, "EMERGENT"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := nephroConsultRequest{
				PatientID:      "p1",
				CreatinineMgDl: 1.0,
				Age:            55,
				Sex:            "male",
				WeightKg:       80,
				Potassium:      ptr(tc.potassium),
			}
			report := interpretNephrology(req)
			if report.Urgent != tc.wantUrgent {
				t.Errorf("potassium=%.1f: urgent=%v, want %v", tc.potassium, report.Urgent, tc.wantUrgent)
			}
			if tc.wantWord != "" {
				found := false
				for _, r := range report.Recommendations {
					if strings.Contains(r, tc.wantWord) {
						found = true
					}
				}
				if !found {
					t.Errorf("potassium=%.1f: expected %q in recommendations: %v",
						tc.potassium, tc.wantWord, report.Recommendations)
				}
			}
		})
	}
}

// TestNephrologistMetabolicAcidosis verifies bicarbonate thresholds
// produce correct urgency and sodium bicarbonate recommendations.
func TestNephrologistMetabolicAcidosis(t *testing.T) {
	cases := []struct {
		name        string
		bicarbonate float64
		wantUrgent  bool
		wantBicarb  bool // recommendation mentions sodium bicarbonate
	}{
		{"normal bicarb 24", 24.0, false, false},
		{"mild acidosis 20", 20.0, false, true},
		{"severe acidosis 14", 14.0, true, true},
		{"severe acidosis 10", 10.0, true, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := nephroConsultRequest{
				PatientID:      "p1",
				CreatinineMgDl: 1.0,
				Age:            55,
				Sex:            "male",
				WeightKg:       80,
				Bicarbonate:    ptr(tc.bicarbonate),
			}
			report := interpretNephrology(req)
			if report.Urgent != tc.wantUrgent {
				t.Errorf("bicarb=%.1f: urgent=%v, want %v", tc.bicarbonate, report.Urgent, tc.wantUrgent)
			}
			if tc.wantBicarb {
				found := false
				for _, r := range report.Recommendations {
					if strings.Contains(strings.ToLower(r), "bicarbonate") {
						found = true
					}
				}
				if !found {
					t.Errorf("bicarb=%.1f: expected bicarbonate recommendation: %v",
						tc.bicarbonate, report.Recommendations)
				}
			}
		})
	}
}

// TestNephrologistNormalFindings verifies the no-pathology fallback path
// returns a non-urgent report with a non-empty recommendation list.
func TestNephrologistNormalFindings(t *testing.T) {
	req := nephroConsultRequest{
		PatientID:      "p1",
		CaseID:         "c1",
		CreatinineMgDl: 0.9,
		Age:            35,
		Sex:            "female",
		WeightKg:       65,
	}
	report := interpretNephrology(req)
	if report.Urgent {
		t.Error("normal findings: urgent must be false")
	}
	if report.DialysisIndicated {
		t.Error("normal findings: dialysis_indicated must be false")
	}
	if report.AKIStage != 0 {
		t.Errorf("normal findings: aki_stage must be 0, got %d", report.AKIStage)
	}
	if len(report.Recommendations) == 0 {
		t.Error("at least one recommendation expected even for normal findings")
	}
	if report.EGFRMlMin <= 0 {
		t.Errorf("egfr_ml_min must be > 0, got %.1f", report.EGFRMlMin)
	}
}

// TestNephrologistRecommendationsDeduplicated verifies that when multiple
// rules fire they do not produce duplicate recommendation strings.
func TestNephrologistRecommendationsDeduplicated(t *testing.T) {
	// G4 CKD + AKI stage 2 + hyperkalemia — many rules fire.
	req := nephroConsultRequest{
		PatientID:       "p1",
		CreatinineMgDl:  5.0,
		PriorCreatinine: ptr(2.0),
		Age:             70,
		Sex:             "male",
		WeightKg:        80,
		Potassium:       ptr(5.8),
		Bicarbonate:     ptr(13.0),
	}
	report := interpretNephrology(req)
	seen := make(map[string]int)
	for _, r := range report.Recommendations {
		seen[r]++
		if seen[r] > 1 {
			t.Errorf("duplicate recommendation: %q", r)
		}
	}
}

// TestNephrologistEGFRInReport verifies that the egfr_ml_min field in the
// full HandleMessage output parses as a valid JSON float.
func TestNephrologistEGFRInReport(t *testing.T) {
	a := &NephrologistAgent{}
	req := nephroConsultRequest{
		PatientID:      "p1",
		CreatinineMgDl: 1.2,
		Age:            55,
		Sex:            "male",
		WeightKg:       85,
	}
	out, err := a.HandleMessage(context.Background(), nephroConsultMsg(req), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage err: %v", err)
	}
	var rep nephroReport
	if err := json.Unmarshal([]byte(out[0].Content), &rep); err != nil {
		t.Fatalf("cannot unmarshal nephrology_report: %v", err)
	}
	if rep.EGFRMlMin <= 0 {
		t.Errorf("egfr_ml_min = %.1f, want > 0", rep.EGFRMlMin)
	}
	if rep.CKDStage == "" {
		t.Error("ckd_stage must be non-empty")
	}
}
