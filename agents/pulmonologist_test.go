package agents

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
)

// pulmoConsultMsg builds an agent.Message for a pulmonology_consult.
// Mirrors the helper pattern used by cardiologist_test.go so the test
// style is consistent across the agents package.
func pulmoConsultMsg(req pulmoConsultRequest) agent.Message {
	body, _ := json.Marshal(req)
	return agent.NewMessage("test", "pulmonologist", agent.RoleUser,
		"pulmonology_consult", string(body),
		map[string]any{"patient_id": req.PatientID, "case_id": req.CaseID})
}

// TestPulmonologistAgentInterface verifies the three identity methods.
func TestPulmonologistAgentInterface(t *testing.T) {
	a := &PulmonologistAgent{}
	if a.ID() != "pulmonologist" {
		t.Errorf("ID = %q, want %q", a.ID(), "pulmonologist")
	}
	if a.Name() != "Pulmonology Specialist" {
		t.Errorf("Name = %q, want %q", a.Name(), "Pulmonology Specialist")
	}
	wantCaps := map[string]bool{
		"pft_interpretation": false,
		"copd_staging":       false,
		"asthma_assessment":  false,
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

// TestPulmonologistHandleMessageRouting verifies HandleMessage emits a
// pulmonology_report addressed to "supervisor" with non-empty content.
func TestPulmonologistHandleMessageRouting(t *testing.T) {
	a := &PulmonologistAgent{}
	req := pulmoConsultRequest{
		PatientID:    "p1",
		CaseID:       "c1",
		FEV1Percent:  72,
		FVCPercent:   88,
		FEV1FVCRatio: 0.74,
		SpO2:         96,
	}
	out, err := a.HandleMessage(context.Background(), pulmoConsultMsg(req), newFakeEnv())
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
	if msg.Type != "pulmonology_report" {
		t.Errorf("Type = %q, want %q", msg.Type, "pulmonology_report")
	}
	if msg.Content == "" {
		t.Error("Content must be non-empty")
	}
}

// TestPulmonologistHandleMessageInvalidJSON verifies that malformed content
// returns an error so the orchestrator can emit a KindAgentError audit event.
func TestPulmonologistHandleMessageInvalidJSON(t *testing.T) {
	a := &PulmonologistAgent{}
	bad := agent.NewMessage("test", "pulmonologist", agent.RoleUser,
		"pulmonology_consult", "{not valid json", nil)
	_, err := a.HandleMessage(context.Background(), bad, newFakeEnv())
	if err == nil {
		t.Error("expected error for invalid JSON content, got nil")
	}
}

// TestPulmonologistPFTPatternClassification verifies the spirometric
// pattern classification (obstruction, restriction, mixed, normal).
func TestPulmonologistPFTPatternClassification(t *testing.T) {
	cases := []struct {
		name         string
		fev1FVCRatio float64
		fvcPercent   float64
		fev1Percent  float64
		wantPattern  string
	}{
		{
			name:         "obstruction — FEV1/FVC < 0.70",
			fev1FVCRatio: 0.65,
			fvcPercent:   85,
			fev1Percent:  55,
			wantPattern:  "obstruction",
		},
		{
			name:         "restriction — FVC < 80% with normal ratio",
			fev1FVCRatio: 0.80,
			fvcPercent:   70,
			fev1Percent:  60,
			wantPattern:  "restriction",
		},
		{
			name:         "mixed — low ratio AND low FVC (obstruction dominates, FVC also low)",
			fev1FVCRatio: 0.65,
			fvcPercent:   70,
			fev1Percent:  45,
			wantPattern:  "mixed",
		},
		{
			name:         "normal — FEV1/FVC ≥ 0.70 and FVC ≥ 80%",
			fev1FVCRatio: 0.78,
			fvcPercent:   90,
			fev1Percent:  88,
			wantPattern:  "normal",
		},
		{
			name:         "obstruction boundary — exactly 0.70 is NOT obstructed",
			fev1FVCRatio: 0.70,
			fvcPercent:   88,
			fev1Percent:  75,
			wantPattern:  "normal",
		},
		{
			name:         "obstruction boundary — 0.69 IS obstructed",
			fev1FVCRatio: 0.69,
			fvcPercent:   88,
			fev1Percent:  75,
			wantPattern:  "obstruction",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := pulmoConsultRequest{
				PatientID:    "p1",
				FEV1FVCRatio: tc.fev1FVCRatio,
				FVCPercent:   tc.fvcPercent,
				FEV1Percent:  tc.fev1Percent,
			}
			report := interpretPulmonology(req)
			if report.Pattern != tc.wantPattern {
				t.Errorf("pattern = %q, want %q (ratio=%.2f fvc=%.0f fev1=%.0f)",
					report.Pattern, tc.wantPattern, tc.fev1FVCRatio, tc.fvcPercent, tc.fev1Percent)
			}
		})
	}
}

// TestPulmonologistCOPDGOLDGrading verifies the four GOLD spirometric grades.
func TestPulmonologistCOPDGOLDGrading(t *testing.T) {
	cases := []struct {
		name         string
		fev1Percent  float64
		fev1FVCRatio float64
		wantGrade    string // substring match in copd_gold_stage
	}{
		{
			name:         "GOLD 1 Mild — FEV1 ≥ 80%",
			fev1Percent:  82,
			fev1FVCRatio: 0.65,
			wantGrade:    "GOLD 1",
		},
		{
			name:         "GOLD 2 Moderate — FEV1 50-79%",
			fev1Percent:  65,
			fev1FVCRatio: 0.62,
			wantGrade:    "GOLD 2",
		},
		{
			name:         "GOLD 2 boundary — FEV1 exactly 50%",
			fev1Percent:  50,
			fev1FVCRatio: 0.60,
			wantGrade:    "GOLD 2",
		},
		{
			name:         "GOLD 3 Severe — FEV1 30-49%",
			fev1Percent:  40,
			fev1FVCRatio: 0.55,
			wantGrade:    "GOLD 3",
		},
		{
			name:         "GOLD 3 boundary — FEV1 exactly 30%",
			fev1Percent:  30,
			fev1FVCRatio: 0.55,
			wantGrade:    "GOLD 3",
		},
		{
			name:         "GOLD 4 Very Severe — FEV1 < 30%",
			fev1Percent:  22,
			fev1FVCRatio: 0.50,
			wantGrade:    "GOLD 4",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := pulmoConsultRequest{
				PatientID:    "p1",
				FEV1Percent:  tc.fev1Percent,
				FEV1FVCRatio: tc.fev1FVCRatio,
				FVCPercent:   85,
			}
			report := interpretPulmonology(req)
			if !strings.Contains(report.COPDGOLDStage, tc.wantGrade) {
				t.Errorf("copd_gold_stage = %q, want substring %q (fev1=%.0f ratio=%.2f)",
					report.COPDGOLDStage, tc.wantGrade, tc.fev1Percent, tc.fev1FVCRatio)
			}
		})
	}
}

// TestPulmonologistCOPDGOLDGroups verifies the ABE group classification.
func TestPulmonologistCOPDGOLDGroups(t *testing.T) {
	cases := []struct {
		name        string
		catScore    int
		mmrc        int
		symptoms    string
		wantGroup   string // "A", "B", or "E"
	}{
		{
			name:      "Group A — low symptoms (CAT < 10, mMRC 0)",
			catScore:  8,
			mmrc:      1,
			symptoms:  "mild cough",
			wantGroup: "A",
		},
		{
			name:      "Group B — high symptoms (CAT ≥ 10)",
			catScore:  15,
			mmrc:      1,
			symptoms:  "significant breathlessness",
			wantGroup: "B",
		},
		{
			name:      "Group B — high symptoms (mMRC ≥ 2)",
			catScore:  9,
			mmrc:      2,
			symptoms:  "breathlessness on walking",
			wantGroup: "B",
		},
		{
			name:      "Group E — hospitalisation in symptoms",
			catScore:  18,
			mmrc:      3,
			symptoms:  "admitted to hospital last year for exacerbation",
			wantGroup: "E",
		},
		{
			name:      "Group E — exacerbation keyword",
			catScore:  12,
			mmrc:      2,
			symptoms:  "two exacerbations requiring antibiotics this year",
			wantGroup: "E",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := pulmoConsultRequest{
				PatientID:    "p1",
				FEV1Percent:  55,
				FEV1FVCRatio: 0.60,
				FVCPercent:   85,
				CATScore:     tc.catScore,
				MMRCDyspnea:  tc.mmrc,
				Symptoms:     tc.symptoms,
			}
			report := interpretPulmonology(req)
			if !strings.Contains(report.COPDGOLDStage, "Group "+tc.wantGroup) {
				t.Errorf("copd_gold_stage = %q, want Group %q", report.COPDGOLDStage, tc.wantGroup)
			}
		})
	}
}

// TestPulmonologistO2Indication verifies the supplemental O2 rule fires
// correctly at the SpO2 ≤ 88% threshold.
func TestPulmonologistO2Indication(t *testing.T) {
	cases := []struct {
		name       string
		spo2       float64
		wantO2     bool
		wantUrgent bool
	}{
		{"SpO2 92% — no O2", 92, false, false},
		{"SpO2 89% — no O2 (above threshold)", 89, false, false},
		{"SpO2 88% — O2 indicated", 88, true, true},
		{"SpO2 85% — O2 indicated", 85, true, true},
		{"SpO2 70% — O2 indicated, very low", 70, true, true},
		{"SpO2 0 — not provided, no O2", 0, false, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := pulmoConsultRequest{
				PatientID: "p1",
				SpO2:      tc.spo2,
			}
			report := interpretPulmonology(req)
			if report.O2Indicated != tc.wantO2 {
				t.Errorf("o2_indicated = %v, want %v (spo2=%.0f)", report.O2Indicated, tc.wantO2, tc.spo2)
			}
			if report.Urgent != tc.wantUrgent {
				t.Errorf("urgent = %v, want %v (spo2=%.0f)", report.Urgent, tc.wantUrgent, tc.spo2)
			}
		})
	}
}

// TestPulmonologistDLCOImpairment verifies that DLCO < 40% triggers urgent
// escalation and appropriate recommendations.
func TestPulmonologistDLCOImpairment(t *testing.T) {
	cases := []struct {
		name        string
		dlco        float64
		wantUrgent  bool
	}{
		{"DLCO 60% — no urgency", 60, false},
		{"DLCO 40% — threshold, no urgency (strict <)", 40, false},
		{"DLCO 39% — just below threshold, urgent", 39, true},
		{"DLCO 20% — severe impairment, urgent", 20, true},
		{"DLCO 0 — not provided, no urgency", 0, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := pulmoConsultRequest{
				PatientID:   "p1",
				DLCOPercent: tc.dlco,
			}
			report := interpretPulmonology(req)
			if report.Urgent != tc.wantUrgent {
				t.Errorf("urgent = %v, want %v (dlco=%.0f)", report.Urgent, tc.wantUrgent, tc.dlco)
			}
			if tc.wantUrgent {
				found := false
				for _, r := range report.Recommendations {
					if strings.Contains(strings.ToLower(r), "dlco") ||
						strings.Contains(strings.ToLower(r), "gas exchange") {
						found = true
					}
				}
				if !found {
					t.Errorf("DLCO impairment case missing gas exchange recommendation: %v", report.Recommendations)
				}
			}
		})
	}
}

// TestPulmonologistAsthmaSeverityClassification verifies the keyword-based
// GINA severity classification from the symptoms string.
func TestPulmonologistAsthmaSeverityClassification(t *testing.T) {
	cases := []struct {
		name         string
		symptoms     string
		wantSeverity string
	}{
		{
			name:         "intermittent — occasional wheeze",
			symptoms:     "occasional wheeze when exercising",
			wantSeverity: "intermittent",
		},
		{
			name:         "intermittent — rare symptoms",
			symptoms:     "rare episodes of dyspnoea",
			wantSeverity: "intermittent",
		},
		{
			name:         "moderate persistent — daily shortness of breath",
			symptoms:     "daily shortness of breath requiring rescue inhaler",
			wantSeverity: "moderate persistent",
		},
		{
			name:         "severe persistent — continuous wheeze with limited activity",
			symptoms:     "continuous wheeze and severely limited activity",
			wantSeverity: "severe persistent",
		},
		{
			name:         "mild persistent — frequent wheeze most days",
			symptoms:     "frequent wheeze on most days of the week",
			wantSeverity: "mild persistent",
		},
		{
			name:         "no asthma keywords — empty severity",
			symptoms:     "chest pain on exertion",
			wantSeverity: "",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// Use normal spirometry so COPD staging doesn't suppress asthma path.
			req := pulmoConsultRequest{
				PatientID:    "p1",
				FEV1FVCRatio: 0.78,
				FVCPercent:   90,
				FEV1Percent:  88,
				Symptoms:     tc.symptoms,
			}
			report := interpretPulmonology(req)
			if report.AsthmaSeverity != tc.wantSeverity {
				t.Errorf("asthma_severity = %q, want %q (symptoms: %q)",
					report.AsthmaSeverity, tc.wantSeverity, tc.symptoms)
			}
		})
	}
}

// TestPulmonologistPulmRehabIndicated verifies that pulmonary rehabilitation
// is recommended for GOLD 2–4 but not GOLD 1.
func TestPulmonologistPulmRehabIndicated(t *testing.T) {
	cases := []struct {
		name        string
		fev1Percent float64
		wantRehab   bool
	}{
		{"GOLD 1 (FEV1 82%) — no rehab", 82, false},
		{"GOLD 2 (FEV1 65%) — rehab indicated", 65, true},
		{"GOLD 3 (FEV1 40%) — rehab indicated", 40, true},
		{"GOLD 4 (FEV1 22%) — rehab indicated", 22, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := pulmoConsultRequest{
				PatientID:    "p1",
				FEV1Percent:  tc.fev1Percent,
				FEV1FVCRatio: 0.62,
				FVCPercent:   88,
			}
			report := interpretPulmonology(req)
			if report.PulmRehabIndicated != tc.wantRehab {
				t.Errorf("pulm_rehab_indicated = %v, want %v (fev1=%.0f gold=%q)",
					report.PulmRehabIndicated, tc.wantRehab, tc.fev1Percent, report.COPDGOLDStage)
			}
		})
	}
}

// TestPulmonologistExacerbationRisk verifies high vs. low exacerbation risk.
func TestPulmonologistExacerbationRisk(t *testing.T) {
	cases := []struct {
		name     string
		symptoms string
		cat      int
		mmrc     int
		wantRisk string
	}{
		{
			name:     "low risk — no exacerbation history",
			symptoms: "mild cough",
			cat:      5,
			mmrc:     1,
			wantRisk: "low",
		},
		{
			name:     "high risk — exacerbation keyword",
			symptoms: "frequent exacerbation requiring steroids",
			cat:      12,
			mmrc:     2,
			wantRisk: "high",
		},
		{
			name:     "high risk — hospitalised keyword",
			symptoms: "hospitalised twice last year",
			cat:      20,
			mmrc:     3,
			wantRisk: "high",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			req := pulmoConsultRequest{
				PatientID:    "p1",
				FEV1Percent:  55,
				FEV1FVCRatio: 0.62,
				FVCPercent:   88,
				CATScore:     tc.cat,
				MMRCDyspnea:  tc.mmrc,
				Symptoms:     tc.symptoms,
			}
			report := interpretPulmonology(req)
			if report.ExacerbationRisk != tc.wantRisk {
				t.Errorf("exacerbation_risk = %q, want %q", report.ExacerbationRisk, tc.wantRisk)
			}
		})
	}
}

// TestPulmonologistMetadataPropagation verifies that patient_id and
// case_id are copied into the outbound message metadata.
func TestPulmonologistMetadataPropagation(t *testing.T) {
	a := &PulmonologistAgent{}
	req := pulmoConsultRequest{
		PatientID:    "patient-xyz",
		CaseID:       "case-abc",
		FEV1FVCRatio: 0.78,
		FVCPercent:   90,
		FEV1Percent:  88,
		SpO2:         96,
	}
	out, err := a.HandleMessage(context.Background(), pulmoConsultMsg(req), newFakeEnv())
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

// TestPulmonologistRecommendationsDeduplicated verifies that when multiple
// rules fire they do not produce duplicate recommendation strings.
func TestPulmonologistRecommendationsDeduplicated(t *testing.T) {
	// Severe COPD + low SpO2 + low DLCO — multiple rules fire.
	req := pulmoConsultRequest{
		PatientID:        "p1",
		FEV1Percent:      28,
		FEV1FVCRatio:     0.55,
		FVCPercent:       72,
		DLCOPercent:      35,
		SpO2:             86,
		SmokingPackYears: 45,
		CATScore:         22,
		MMRCDyspnea:      3,
		Symptoms:         "severe breathlessness, hospitalised last month for exacerbation",
	}
	report := interpretPulmonology(req)
	seen := make(map[string]int)
	for _, r := range report.Recommendations {
		seen[r]++
		if seen[r] > 1 {
			t.Errorf("duplicate recommendation: %q", r)
		}
	}
}

// TestPulmonologistInsufficientData verifies the no-data fallback path
// returns a non-urgent report with an "insufficient data" pattern and
// at least one recommendation for further testing.
func TestPulmonologistInsufficientData(t *testing.T) {
	req := pulmoConsultRequest{
		PatientID: "p1",
		CaseID:    "c1",
	}
	report := interpretPulmonology(req)
	if report.Pattern != "insufficient data" {
		t.Errorf("pattern = %q, want %q", report.Pattern, "insufficient data")
	}
	if report.Urgent {
		t.Error("insufficient data case must not be urgent")
	}
	if len(report.Recommendations) == 0 {
		t.Error("at least one recommendation expected for insufficient data")
	}
	found := false
	for _, r := range report.Recommendations {
		if strings.Contains(strings.ToLower(r), "spirometry") ||
			strings.Contains(strings.ToLower(r), "pulmonary function") {
			found = true
		}
	}
	if !found {
		t.Errorf("insufficient data case should recommend spirometry/PFT: %v", report.Recommendations)
	}
}

// TestPulmonologistGOLD4Urgent verifies that GOLD 4 (very severe) COPD
// sets urgent=true.
func TestPulmonologistGOLD4Urgent(t *testing.T) {
	req := pulmoConsultRequest{
		PatientID:    "p1",
		FEV1Percent:  22,
		FEV1FVCRatio: 0.50,
		FVCPercent:   85,
	}
	report := interpretPulmonology(req)
	if !report.Urgent {
		t.Error("GOLD 4 must set urgent=true")
	}
	if !strings.Contains(report.COPDGOLDStage, "GOLD 4") {
		t.Errorf("copd_gold_stage = %q, want GOLD 4", report.COPDGOLDStage)
	}
}

// TestPulmonologistSmokingCessationRecommended verifies that patients with
// COPD and smoking history receive a smoking cessation recommendation.
func TestPulmonologistSmokingCessationRecommended(t *testing.T) {
	req := pulmoConsultRequest{
		PatientID:        "p1",
		FEV1Percent:      65,
		FEV1FVCRatio:     0.62,
		FVCPercent:       88,
		SmokingPackYears: 30,
	}
	report := interpretPulmonology(req)
	found := false
	for _, r := range report.Recommendations {
		if strings.Contains(strings.ToLower(r), "smoking cessation") ||
			strings.Contains(strings.ToLower(r), "varenicline") {
			found = true
		}
	}
	if !found {
		t.Errorf("COPD + smoking history missing smoking cessation recommendation: %v", report.Recommendations)
	}
}
