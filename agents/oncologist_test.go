package agents

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"github.com/PratikDhanave/bodh/pkg/agent"
)

// newOncologyMsg builds an oncology_consult agent.Message from an OncologyConsult.
func newOncologyMsg(c OncologyConsult) agent.Message {
	body, _ := json.Marshal(c)
	return agent.NewMessage(
		"user",
		"oncologist",
		agent.RoleUser,
		"oncology_consult",
		string(body),
		map[string]any{
			"case_id":    c.CaseID,
			"patient_id": c.PatientID,
		},
	)
}

// decodeRec is a test helper that decodes an OncologyRecommendation from
// the first output message's Content.
func decodeRec(t *testing.T, msgs []agent.Message) OncologyRecommendation {
	t.Helper()
	if len(msgs) != 1 {
		t.Fatalf("want 1 output message, got %d", len(msgs))
	}
	var rec OncologyRecommendation
	if err := json.Unmarshal([]byte(msgs[0].Content), &rec); err != nil {
		t.Fatalf("decode OncologyRecommendation: %v", err)
	}
	return rec
}

// TestOncologistIDAndCapabilities verifies static metadata.
func TestOncologistIDAndCapabilities(t *testing.T) {
	a := &OncologistAgent{}
	if a.ID() != "oncologist" {
		t.Errorf("ID: want oncologist, got %q", a.ID())
	}
	if a.Name() != "Oncology Specialist" {
		t.Errorf("Name: want %q, got %q", "Oncology Specialist", a.Name())
	}
	caps := a.Capabilities()
	wantCaps := map[string]bool{"chemotherapy_protocol": true, "oncology_assessment": true}
	for _, c := range caps {
		delete(wantCaps, c)
	}
	for missing := range wantCaps {
		t.Errorf("Capabilities: missing %q", missing)
	}
}

// TestOncologistInvalidJSON ensures a malformed payload returns an error.
func TestOncologistInvalidJSON(t *testing.T) {
	a := &OncologistAgent{}
	msg := agent.NewMessage("user", "oncologist", agent.RoleUser, "oncology_consult",
		"not json at all", nil)
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Fatal("expected error on invalid JSON, got nil")
	}
	if len(out) != 0 {
		t.Errorf("expected no output messages on error, got %d", len(out))
	}
}

// TestOncologistNSCLC verifies Carboplatin/Paclitaxel is selected for
// NSCLC stage III with ECOG 0 and normal renal function.
func TestOncologistNSCLC(t *testing.T) {
	a := &OncologistAgent{}
	consult := OncologyConsult{
		PatientID:     "PT-001",
		CaseID:        "CASE-001",
		CancerType:    "NSCLC",
		Stage:         "III",
		ECOGScore:     0,
		WeightKg:      70,
		HeightCm:      170,
		RenalFunction: "normal",
	}
	out, err := a.HandleMessage(context.Background(), newOncologyMsg(consult), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	rec := decodeRec(t, out)

	if rec.Protocol != "Carboplatin/Paclitaxel" {
		t.Errorf("Protocol: want Carboplatin/Paclitaxel, got %q", rec.Protocol)
	}
	if rec.Urgent {
		t.Errorf("Urgent: expected false for ECOG 0 / normal renal")
	}
	if len(rec.DoseReductions) != 0 {
		t.Errorf("DoseReductions: expected none, got %v", rec.DoseReductions)
	}
	if rec.BSAM2 <= 0 {
		t.Errorf("BSAM2: expected positive value, got %v", rec.BSAM2)
	}
	// Mosteller: sqrt(170*70/3600) ≈ 1.82
	expectedBSA := math.Round(math.Sqrt(170*70.0/3600)*100) / 100
	if math.Abs(rec.BSAM2-expectedBSA) > 0.01 {
		t.Errorf("BSAM2: want %.2f, got %.2f", expectedBSA, rec.BSAM2)
	}
	// Must have at least Carboplatin and Paclitaxel
	drugNames := map[string]bool{}
	for _, d := range rec.Drugs {
		drugNames[d.Name] = true
	}
	if !drugNames["Carboplatin"] {
		t.Errorf("Drugs: missing Carboplatin")
	}
	if !drugNames["Paclitaxel"] {
		t.Errorf("Drugs: missing Paclitaxel")
	}
}

// TestOncologistNSCLCHighECOGNotSelected verifies that NSCLC with ECOG ≥ 2
// does not receive the Carboplatin/Paclitaxel protocol (falls through to
// supportive care since no other rule matches).
func TestOncologistNSCLCHighECOGNotSelected(t *testing.T) {
	a := &OncologistAgent{}
	consult := OncologyConsult{
		PatientID:     "PT-002",
		CaseID:        "CASE-002",
		CancerType:    "NSCLC",
		Stage:         "IV",
		ECOGScore:     2,
		WeightKg:      65,
		HeightCm:      165,
		RenalFunction: "normal",
	}
	out, err := a.HandleMessage(context.Background(), newOncologyMsg(consult), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	rec := decodeRec(t, out)
	// ECOG 2 falls outside the ≤1 guard, should be supportive care
	if rec.Protocol == "Carboplatin/Paclitaxel" {
		t.Errorf("ECOG 2 NSCLC should not receive Carboplatin/Paclitaxel, got %q", rec.Protocol)
	}
	// Supportive care → Urgent=true
	if !rec.Urgent {
		t.Errorf("expected Urgent=true for unsupported case, got false")
	}
}

// TestOncologistColonFOLFOX verifies FOLFOX is selected for stage III colon.
func TestOncologistColonFOLFOX(t *testing.T) {
	a := &OncologistAgent{}
	consult := OncologyConsult{
		PatientID:     "PT-003",
		CaseID:        "CASE-003",
		CancerType:    "Colon cancer",
		Stage:         "III",
		ECOGScore:     1,
		WeightKg:      80,
		HeightCm:      175,
		RenalFunction: "normal",
	}
	out, err := a.HandleMessage(context.Background(), newOncologyMsg(consult), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	rec := decodeRec(t, out)
	if rec.Protocol != "FOLFOX" {
		t.Errorf("Protocol: want FOLFOX, got %q", rec.Protocol)
	}
	if rec.Urgent {
		t.Errorf("Urgent: expected false for ECOG 1 / normal renal")
	}
	// FOLFOX must contain Oxaliplatin and 5-Fluorouracil
	drugNames := map[string]bool{}
	for _, d := range rec.Drugs {
		drugNames[d.Name] = true
	}
	if !drugNames["Oxaliplatin"] {
		t.Errorf("Drugs: missing Oxaliplatin")
	}
	if !drugNames["5-Fluorouracil"] {
		t.Errorf("Drugs: missing 5-Fluorouracil")
	}
}

// TestOncologistLymphomaDLBCL verifies R-CHOP is selected for DLBCL.
func TestOncologistLymphomaDLBCL(t *testing.T) {
	a := &OncologistAgent{}
	consult := OncologyConsult{
		PatientID:     "PT-004",
		CaseID:        "CASE-004",
		CancerType:    "DLBCL",
		Stage:         "II",
		ECOGScore:     0,
		WeightKg:      72,
		HeightCm:      172,
		RenalFunction: "normal",
	}
	out, err := a.HandleMessage(context.Background(), newOncologyMsg(consult), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	rec := decodeRec(t, out)
	if rec.Protocol != "R-CHOP" {
		t.Errorf("Protocol: want R-CHOP, got %q", rec.Protocol)
	}
	drugNames := map[string]bool{}
	for _, d := range rec.Drugs {
		drugNames[d.Name] = true
	}
	if !drugNames["Rituximab"] {
		t.Errorf("Drugs: missing Rituximab")
	}
	if !drugNames["Doxorubicin"] {
		t.Errorf("Drugs: missing Doxorubicin")
	}
}

// TestOncologistDoseReductionECOG3 verifies that ECOG ≥ 3 triggers a 25%
// dose reduction on all drugs and sets Urgent=true.
func TestOncologistDoseReductionECOG3(t *testing.T) {
	a := &OncologistAgent{}
	// Use FOLFOX (colon stage III) so we have drugs with non-zero DoseMgM2.
	consult := OncologyConsult{
		PatientID:     "PT-005",
		CaseID:        "CASE-005",
		CancerType:    "colorectal",
		Stage:         "III",
		ECOGScore:     3,
		WeightKg:      60,
		HeightCm:      160,
		RenalFunction: "normal",
	}
	// Compute expected doses before reduction.
	// FOLFOX Oxaliplatin 85 → 85*0.75 = 63.75
	out, err := a.HandleMessage(context.Background(), newOncologyMsg(consult), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	rec := decodeRec(t, out)

	if !rec.Urgent {
		t.Errorf("Urgent: expected true for ECOG 3, got false")
	}

	reductionFound := false
	for _, dr := range rec.DoseReductions {
		if len(dr) > 0 {
			reductionFound = true
		}
	}
	if !reductionFound {
		t.Errorf("DoseReductions: expected at least one entry for ECOG 3")
	}

	// Verify Oxaliplatin dose was reduced 25%.
	for _, d := range rec.Drugs {
		if d.Name == "Oxaliplatin" {
			expected := math.Round(85*0.75*100) / 100
			if math.Abs(d.DoseMgM2-expected) > 0.01 {
				t.Errorf("Oxaliplatin dose: want %.2f (75%% of 85), got %.2f", expected, d.DoseMgM2)
			}
		}
	}
}

// TestOncologistDoseReductionRenalModerate verifies that moderate renal
// impairment reduces carboplatin/cisplatin doses by 25%.
func TestOncologistDoseReductionRenalModerate(t *testing.T) {
	a := &OncologistAgent{}
	// NSCLC with moderate renal — Carboplatin should be flagged.
	// Note: Carboplatin's DoseMgM2 is 0 (AUC-based), so the reduction
	// adds a note but the numeric value stays 0. Verify via DoseReductions text.
	consult := OncologyConsult{
		PatientID:     "PT-006",
		CaseID:        "CASE-006",
		CancerType:    "NSCLC",
		Stage:         "IV",
		ECOGScore:     0,
		WeightKg:      68,
		HeightCm:      168,
		RenalFunction: "moderate",
	}
	out, err := a.HandleMessage(context.Background(), newOncologyMsg(consult), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	rec := decodeRec(t, out)

	reductionMentionsCarboplatin := false
	for _, dr := range rec.DoseReductions {
		if containsCI(dr, "carboplatin") || containsCI(dr, "renal") {
			reductionMentionsCarboplatin = true
		}
	}
	if !reductionMentionsCarboplatin {
		t.Errorf("DoseReductions: expected mention of Carboplatin renal adjustment, got %v", rec.DoseReductions)
	}
}

// TestOncologistDoseReductionRenalSevere verifies that severe renal
// impairment with nephrotoxic agents sets Urgent=true.
func TestOncologistDoseReductionRenalSevere(t *testing.T) {
	a := &OncologistAgent{}
	consult := OncologyConsult{
		PatientID:     "PT-007",
		CaseID:        "CASE-007",
		CancerType:    "NSCLC",
		Stage:         "III",
		ECOGScore:     1,
		WeightKg:      75,
		HeightCm:      175,
		RenalFunction: "severe",
	}
	out, err := a.HandleMessage(context.Background(), newOncologyMsg(consult), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	rec := decodeRec(t, out)
	if !rec.Urgent {
		t.Errorf("Urgent: expected true for severe renal + nephrotoxic agent, got false")
	}
}

// TestOncologistUnknownCancerType verifies the supportive care fallback
// fires for unrecognised cancer types and Urgent=true.
func TestOncologistUnknownCancerType(t *testing.T) {
	a := &OncologistAgent{}
	consult := OncologyConsult{
		PatientID:     "PT-008",
		CaseID:        "CASE-008",
		CancerType:    "thymoma",
		Stage:         "II",
		ECOGScore:     1,
		WeightKg:      65,
		HeightCm:      165,
		RenalFunction: "normal",
	}
	out, err := a.HandleMessage(context.Background(), newOncologyMsg(consult), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	rec := decodeRec(t, out)
	if !rec.Urgent {
		t.Errorf("Urgent: expected true for unknown cancer type")
	}
	if len(rec.Drugs) != 0 {
		t.Errorf("Drugs: expected empty for supportive care only, got %v", rec.Drugs)
	}
}

// TestOncologistOutputMessageType verifies the output message type and
// metadata propagation.
func TestOncologistOutputMessageType(t *testing.T) {
	a := &OncologistAgent{}
	consult := OncologyConsult{
		PatientID:     "PT-009",
		CaseID:        "CASE-009",
		CancerType:    "DLBCL",
		Stage:         "III",
		ECOGScore:     1,
		WeightKg:      70,
		HeightCm:      170,
		RenalFunction: "normal",
	}
	out, err := a.HandleMessage(context.Background(), newOncologyMsg(consult), newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 message, got %d", len(out))
	}
	if out[0].Type != "oncology_recommendation" {
		t.Errorf("Type: want oncology_recommendation, got %q", out[0].Type)
	}
	if out[0].From != "oncologist" {
		t.Errorf("From: want oncologist, got %q", out[0].From)
	}
	if out[0].Metadata["patient_id"] != "PT-009" {
		t.Errorf("Metadata[patient_id]: want PT-009, got %v", out[0].Metadata["patient_id"])
	}
	if out[0].Metadata["case_id"] != "CASE-009" {
		t.Errorf("Metadata[case_id]: want CASE-009, got %v", out[0].Metadata["case_id"])
	}
}

// TestMosteller verifies the BSA formula against known values.
// Expected values are computed from the exact Mosteller formula
// sqrt(height_cm * weight_kg / 3600) without pre-rounding so the
// test tolerates floating-point representation differences.
func TestMosteller(t *testing.T) {
	tests := []struct {
		name     string
		heightCm float64
		weightKg float64
		wantBSA  float64
	}{
		{"typical adult", 170, 70, math.Sqrt(170 * 70.0 / 3600)},
		{"small patient", 155, 50, math.Sqrt(155 * 50.0 / 3600)},
		{"large patient", 185, 100, math.Sqrt(185 * 100.0 / 3600)},
		{"zero height", 0, 70, 0},
		{"zero weight", 170, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mosteller(tc.heightCm, tc.weightKg)
			if math.Abs(got-tc.wantBSA) > 0.0001 {
				t.Errorf("mosteller(%.0f, %.0f) = %.6f, want %.6f", tc.heightCm, tc.weightKg, got, tc.wantBSA)
			}
		})
	}
}

// TestOncologistSupportiveCareContents verifies that recommendations always
// include supportive care and monitoring entries.
func TestOncologistSupportiveCareContents(t *testing.T) {
	cases := []OncologyConsult{
		{PatientID: "P1", CaseID: "C1", CancerType: "NSCLC", Stage: "III", ECOGScore: 0, WeightKg: 70, HeightCm: 170, RenalFunction: "normal"},
		{PatientID: "P2", CaseID: "C2", CancerType: "Breast cancer", Stage: "II", ECOGScore: 1, WeightKg: 65, HeightCm: 165, RenalFunction: "normal"},
		{PatientID: "P3", CaseID: "C3", CancerType: "Colon cancer", Stage: "III", ECOGScore: 0, WeightKg: 80, HeightCm: 175, RenalFunction: "normal"},
		{PatientID: "P4", CaseID: "C4", CancerType: "lymphoma", Stage: "I", ECOGScore: 0, WeightKg: 72, HeightCm: 172, RenalFunction: "normal"},
	}
	a := &OncologistAgent{}
	for _, c := range cases {
		out, err := a.HandleMessage(context.Background(), newOncologyMsg(c), newFakeEnv())
		if err != nil {
			t.Fatalf("case %s: HandleMessage: %v", c.CaseID, err)
		}
		rec := decodeRec(t, out)
		if len(rec.SupportiveCare) == 0 {
			t.Errorf("case %s (%s): expected non-empty SupportiveCare", c.CaseID, c.CancerType)
		}
		if len(rec.Monitoring) == 0 {
			t.Errorf("case %s (%s): expected non-empty Monitoring", c.CaseID, c.CancerType)
		}
	}
}

// containsCI is a case-insensitive string contains helper for tests.
func containsCI(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub ||
		len(s) > 0 && len(sub) > 0 &&
			containsLower(s, sub))
}

func containsLower(s, sub string) bool {
	sl := make([]byte, len(s))
	subl := make([]byte, len(sub))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		sl[i] = c
	}
	for i := range sub {
		c := sub[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		subl[i] = c
	}
	return string(sl) != "" && string(subl) != "" && len(sl) >= len(subl) &&
		indexBytes(sl, subl) >= 0
}

func indexBytes(s, sub []byte) int {
	if len(sub) == 0 {
		return 0
	}
	if len(s) < len(sub) {
		return -1
	}
	for i := 0; i <= len(s)-len(sub); i++ {
		match := true
		for j := range sub {
			if s[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
