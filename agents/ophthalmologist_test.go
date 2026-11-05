package agents

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// ophthalConsultMsg builds a well-formed agent.Message for testing
// OphthalmologistAgent.HandleMessage end-to-end.
func ophthalConsultMsg(req ophthalConsultRequest) agent.Message {
	body, _ := json.Marshal(req)
	return agent.NewMessage("test_planner", "ophthalmologist", agent.RoleAgent,
		"ophthalmology_consult", string(body),
		map[string]any{"case_id": req.CaseID, "patient_id": req.PatientID})
}

// decodeOphthalReport is a test helper that parses the ophthalmology_report payload.
func decodeOphthalReport(t *testing.T, content string) ophthalReport {
	t.Helper()
	var r ophthalReport
	if err := json.Unmarshal([]byte(content), &r); err != nil {
		t.Fatalf("decodeOphthalReport: %v (content=%q)", err, content)
	}
	return r
}

// ptr helpers for optional fields.
func f64ptr(v float64) *float64 { return &v }
func intptr(v int) *int         { return &v }

// ---------------------------------------------------------------------------
// Agent identity
// ---------------------------------------------------------------------------

func TestOphthalmologistAgent_Identity(t *testing.T) {
	a := &OphthalmologistAgent{}
	if a.ID() != "ophthalmologist" {
		t.Errorf("ID: want ophthalmologist, got %q", a.ID())
	}
	if a.Name() != "Ophthalmology Specialist" {
		t.Errorf("Name: want Ophthalmology Specialist, got %q", a.Name())
	}
	wantCaps := map[string]bool{
		"retinopathy_screening":    true,
		"glaucoma_assessment":      true,
		"eye_exam_interpretation":  true,
	}
	for _, c := range a.Capabilities() {
		wantCaps[c] = false
	}
	for cap, unmatched := range wantCaps {
		if unmatched {
			t.Errorf("missing capability: %q", cap)
		}
	}
}

// ---------------------------------------------------------------------------
// HandleMessage integration tests
// ---------------------------------------------------------------------------

func TestOphthalmologist_HandleMessage_Routing(t *testing.T) {
	a := &OphthalmologistAgent{}
	req := ophthalConsultRequest{PatientID: "p1", CaseID: "c1", DiagnosisContext: "Type 2 DM"}
	msg := ophthalConsultMsg(req)
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 output message, got %d", len(out))
	}
	if out[0].Type != "ophthalmology_report" {
		t.Errorf("want Type=ophthalmology_report, got %q", out[0].Type)
	}
	if out[0].To != "test_planner" {
		t.Errorf("want To=test_planner (msg.From), got %q", out[0].To)
	}
	if out[0].From != "ophthalmologist" {
		t.Errorf("want From=ophthalmologist, got %q", out[0].From)
	}
}

func TestOphthalmologist_HandleMessage_MetadataPropagation(t *testing.T) {
	a := &OphthalmologistAgent{}
	req := ophthalConsultRequest{PatientID: "p42", CaseID: "case-99"}
	msg := ophthalConsultMsg(req)
	out, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if got := out[0].Metadata["patient_id"]; got != "p42" {
		t.Errorf("patient_id: want p42, got %v", got)
	}
	if got := out[0].Metadata["case_id"]; got != "case-99" {
		t.Errorf("case_id: want case-99, got %v", got)
	}
}

func TestOphthalmologist_HandleMessage_InvalidJSON(t *testing.T) {
	a := &OphthalmologistAgent{}
	msg := agent.NewMessage("src", "ophthalmologist", agent.RoleAgent,
		"ophthalmology_consult", "{not valid json}", nil)
	_, err := a.HandleMessage(context.Background(), msg, newFakeEnv())
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
}

// ---------------------------------------------------------------------------
// Diabetic retinopathy staging — ETDRS
// ---------------------------------------------------------------------------

func TestOphthalmologist_DR_NoDR(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:        "p1",
		DiagnosisContext: "Type 2 DM",
		FundusDescription: "Normal fundus. No diabetic changes identified.",
	}
	r := interpretOphthalmology(req)
	if r.DiabeticRetinopathyStage != "No DR" {
		t.Errorf("DR stage: want No DR, got %q", r.DiabeticRetinopathyStage)
	}
	if r.VisionThreatening {
		t.Error("vision_threatening should be false for No DR")
	}
}

func TestOphthalmologist_DR_MildNPDR(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:         "p2",
		DiagnosisContext:  "Type 2 DM 5 years",
		FundusDescription: "A few microaneurysms scattered in mid-periphery. No other changes.",
	}
	r := interpretOphthalmology(req)
	if r.DiabeticRetinopathyStage != "Mild NPDR" {
		t.Errorf("DR stage: want Mild NPDR, got %q", r.DiabeticRetinopathyStage)
	}
	if r.VisionThreatening {
		t.Error("vision_threatening should be false for Mild NPDR without DME")
	}
}

func TestOphthalmologist_DR_ModerateNPDR(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:         "p3",
		DiagnosisContext:  "Type 2 DM 8 years",
		FundusDescription: "Multiple microaneurysms with cotton-wool spots and hard exudates.",
	}
	r := interpretOphthalmology(req)
	if r.DiabeticRetinopathyStage != "Moderate NPDR" {
		t.Errorf("DR stage: want Moderate NPDR, got %q", r.DiabeticRetinopathyStage)
	}
}

func TestOphthalmologist_DR_SevereNPDR_FourQuadrantHaemorrhage(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:         "p4",
		DiagnosisContext:  "Type 2 DM 15 years",
		FundusDescription: "Hemorrhages in 4 quadrants with venous beading noted.",
	}
	r := interpretOphthalmology(req)
	if r.DiabeticRetinopathyStage != "Severe NPDR" {
		t.Errorf("DR stage: want Severe NPDR, got %q", r.DiabeticRetinopathyStage)
	}
}

func TestOphthalmologist_DR_SevereNPDR_VenousBeadingIRMA(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:         "p5",
		DiagnosisContext:  "Type 2 DM 12 years",
		FundusDescription: "Venous beading in superior arcade. IRMA present in temporal sector.",
	}
	r := interpretOphthalmology(req)
	if r.DiabeticRetinopathyStage != "Severe NPDR" {
		t.Errorf("DR stage: want Severe NPDR, got %q", r.DiabeticRetinopathyStage)
	}
}

func TestOphthalmologist_DR_PDR_Neovascularisation(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:         "p6",
		DiagnosisContext:  "Type 2 DM 20 years",
		FundusDescription: "Neovascularization of the disc (NVD) with pre-retinal haemorrhage.",
	}
	r := interpretOphthalmology(req)
	if r.DiabeticRetinopathyStage != "PDR" {
		t.Errorf("DR stage: want PDR, got %q", r.DiabeticRetinopathyStage)
	}
	if !r.VisionThreatening {
		t.Error("vision_threatening should be true for PDR")
	}
	if r.ReferralUrgency != "same-day" {
		t.Errorf("referral_urgency: want same-day, got %q", r.ReferralUrgency)
	}
}

func TestOphthalmologist_DR_PDR_NewVessels(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:         "p7",
		DiagnosisContext:  "Type 1 DM",
		FundusDescription: "New vessels elsewhere (NVE) on inferior arcade.",
	}
	r := interpretOphthalmology(req)
	if r.DiabeticRetinopathyStage != "PDR" {
		t.Errorf("DR stage: want PDR, got %q", r.DiabeticRetinopathyStage)
	}
}

// ---------------------------------------------------------------------------
// Diabetic macular edema
// ---------------------------------------------------------------------------

func TestOphthalmologist_DME_MacularEdema(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:         "p8",
		DiagnosisContext:  "Type 2 DM",
		FundusDescription: "Macular edema present with subretinal fluid.",
	}
	r := interpretOphthalmology(req)
	if !r.DiabeticMacularEdema {
		t.Error("diabetic_macular_edema should be true for macular edema")
	}
	if !r.VisionThreatening {
		t.Error("vision_threatening should be true for DME")
	}
	if r.ReferralUrgency != "within-1-week" {
		t.Errorf("referral_urgency: want within-1-week, got %q", r.ReferralUrgency)
	}
}

func TestOphthalmologist_DME_HardExudatesNearFovea(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:         "p9",
		DiagnosisContext:  "Type 2 DM",
		FundusDescription: "Hard exudates near fovea in a circinate pattern.",
	}
	r := interpretOphthalmology(req)
	if !r.DiabeticMacularEdema {
		t.Error("diabetic_macular_edema should be true for hard exudates near fovea")
	}
}

func TestOphthalmologist_DME_CSME(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:         "p10",
		DiagnosisContext:  "Type 2 DM",
		FundusDescription: "CSME identified on biomicroscopy.",
	}
	r := interpretOphthalmology(req)
	if !r.DiabeticMacularEdema {
		t.Error("diabetic_macular_edema should be true for CSME")
	}
}

func TestOphthalmologist_DME_NoDME(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:         "p11",
		DiagnosisContext:  "Type 2 DM",
		FundusDescription: "Microaneurysms only. No macular involvement.",
	}
	r := interpretOphthalmology(req)
	if r.DiabeticMacularEdema {
		t.Error("diabetic_macular_edema should be false when no DME keywords present")
	}
}

// ---------------------------------------------------------------------------
// Glaucoma assessment
// ---------------------------------------------------------------------------

func TestOphthalmologist_Glaucoma_NormalIOP(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID: "p12",
		IOPRight:  f64ptr(16),
		IOPLeft:   f64ptr(15),
	}
	r := interpretOphthalmology(req)
	if r.GlaucomaSuspect {
		t.Error("glaucoma_suspect should be false for normal IOP")
	}
}

func TestOphthalmologist_Glaucoma_ElevatedIOP_Suspect(t *testing.T) {
	// IOP = 24 mmHg: above the normal ceiling of 21 → glaucoma suspect,
	// but below the within-1-week trigger of 25. Referral is routine
	// (glaucoma evaluation recommended, not emergent escalation).
	req := ophthalConsultRequest{
		PatientID: "p13",
		IOPRight:  f64ptr(24),
		IOPLeft:   f64ptr(18),
	}
	r := interpretOphthalmology(req)
	if !r.GlaucomaSuspect {
		t.Error("glaucoma_suspect should be true for IOP > 21")
	}
	// 24 mmHg is elevated but does not cross the 25 mmHg within-1-week threshold.
	if r.ReferralUrgency != "routine" {
		t.Errorf("referral_urgency: want routine for IOP 22-25 (below within-1-week trigger), got %q", r.ReferralUrgency)
	}
}

func TestOphthalmologist_Glaucoma_HighIOP_Urgent(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID: "p14",
		IOPRight:  f64ptr(28),
		IOPLeft:   f64ptr(27),
	}
	r := interpretOphthalmology(req)
	if !r.GlaucomaSuspect {
		t.Error("glaucoma_suspect should be true for IOP > 25")
	}
	if r.ReferralUrgency != "within-1-week" {
		t.Errorf("referral_urgency: want within-1-week, got %q", r.ReferralUrgency)
	}
}

func TestOphthalmologist_Glaucoma_VeryHighIOP_SameDay(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID: "p15",
		IOPRight:  f64ptr(38),
		IOPLeft:   f64ptr(22),
	}
	r := interpretOphthalmology(req)
	if !r.GlaucomaSuspect {
		t.Error("glaucoma_suspect should be true for IOP > 35")
	}
	if r.ReferralUrgency != "same-day" {
		t.Errorf("referral_urgency: want same-day for IOP > 35, got %q", r.ReferralUrgency)
	}
}

func TestOphthalmologist_Glaucoma_CupDiscRatio_Suspect(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:    "p16",
		CupDiscRatio: f64ptr(0.75),
		IOPRight:     f64ptr(18),
		IOPLeft:      f64ptr(17),
	}
	r := interpretOphthalmology(req)
	if !r.GlaucomaSuspect {
		t.Error("glaucoma_suspect should be true for CDR > 0.6")
	}
}

func TestOphthalmologist_Glaucoma_NormalCDR_NotSuspect(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:    "p17",
		CupDiscRatio: f64ptr(0.4),
		IOPRight:     f64ptr(14),
		IOPLeft:      f64ptr(15),
	}
	r := interpretOphthalmology(req)
	if r.GlaucomaSuspect {
		t.Error("glaucoma_suspect should be false for CDR ≤ 0.6 and normal IOP")
	}
}

// ---------------------------------------------------------------------------
// Visual acuity
// ---------------------------------------------------------------------------

func TestOphthalmologist_VA_Normal(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:  "p18",
		VARightEye: "20/20",
		VALeftEye:  "20/20",
	}
	r := interpretOphthalmology(req)
	if r.VisionThreatening {
		t.Error("vision_threatening should be false for 20/20 VA")
	}
	if r.ReferralUrgency != "routine" {
		t.Errorf("referral_urgency: want routine for normal VA, got %q", r.ReferralUrgency)
	}
}

func TestOphthalmologist_VA_DrivingLimit_Impaired(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:  "p19",
		VARightEye: "20/40",
		VALeftEye:  "20/60",
	}
	r := interpretOphthalmology(req)
	// 20/40 is the driving limit; better eye = 20/40 = 0.50 → not worse than limit.
	// The better eye (right, 20/40) is exactly at the limit, so impaired=false.
	// However the left eye (20/60) is worse; best eye is 20/40 = 0.50.
	// impaired condition is best < 0.50, so 0.50 is NOT impaired.
	if r.ReferralUrgency == "same-day" {
		t.Errorf("referral_urgency should not be same-day for 20/40 better eye, got %q", r.ReferralUrgency)
	}
}

func TestOphthalmologist_VA_WorseThanDrivingLimit(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:  "p20",
		VARightEye: "20/50",
		VALeftEye:  "20/60",
	}
	r := interpretOphthalmology(req)
	// best eye = 20/50 = 0.40 < 0.50 → impaired
	if r.ReferralUrgency != "within-1-week" {
		t.Errorf("referral_urgency: want within-1-week for VA < 20/40, got %q", r.ReferralUrgency)
	}
}

func TestOphthalmologist_VA_LegalBlindness(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:  "p21",
		VARightEye: "20/200",
		VALeftEye:  "20/400",
	}
	r := interpretOphthalmology(req)
	if !r.VisionThreatening {
		t.Error("vision_threatening should be true for legal blindness VA")
	}
	if r.ReferralUrgency != "same-day" {
		t.Errorf("referral_urgency: want same-day for legal blindness, got %q", r.ReferralUrgency)
	}
}

func TestOphthalmologist_VA_Unknown(t *testing.T) {
	// No VA provided — should not crash and should not flag impaired.
	req := ophthalConsultRequest{PatientID: "p22"}
	r := interpretOphthalmology(req)
	if r.VisionThreatening {
		t.Error("vision_threatening should be false when no VA provided")
	}
}

// ---------------------------------------------------------------------------
// Next exam interval
// ---------------------------------------------------------------------------

func TestOphthalmologist_NextExam_PDR(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:         "p23",
		FundusDescription: "Neovascularization of the disc present.",
	}
	r := interpretOphthalmology(req)
	if r.NextExamMonths != 3 {
		t.Errorf("next_exam_months: want 3 for PDR, got %d", r.NextExamMonths)
	}
}

func TestOphthalmologist_NextExam_DME(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:         "p24",
		FundusDescription: "Macular edema with hard exudates.",
	}
	r := interpretOphthalmology(req)
	if r.NextExamMonths != 3 {
		t.Errorf("next_exam_months: want 3 for DME, got %d", r.NextExamMonths)
	}
}

func TestOphthalmologist_NextExam_ModerateNPDR(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:         "p25",
		FundusDescription: "Microaneurysms with cotton-wool spots.",
	}
	r := interpretOphthalmology(req)
	if r.NextExamMonths != 6 {
		t.Errorf("next_exam_months: want 6 for Moderate NPDR, got %d", r.NextExamMonths)
	}
}

func TestOphthalmologist_NextExam_MildNPDR_LowRisk(t *testing.T) {
	hba1c := 6.5
	dur := 4
	req := ophthalConsultRequest{
		PatientID:             "p26",
		FundusDescription:     "Occasional microaneurysm.",
		HbA1c:                 &hba1c,
		DurationDiabetesYears: &dur,
	}
	r := interpretOphthalmology(req)
	if r.NextExamMonths != 12 {
		t.Errorf("next_exam_months: want 12 for Mild NPDR low risk, got %d", r.NextExamMonths)
	}
}

func TestOphthalmologist_NextExam_MildNPDR_HighRisk(t *testing.T) {
	hba1c := 9.2
	dur := 15
	req := ophthalConsultRequest{
		PatientID:             "p27",
		FundusDescription:     "Occasional microaneurysm.",
		HbA1c:                 &hba1c,
		DurationDiabetesYears: &dur,
	}
	r := interpretOphthalmology(req)
	if r.NextExamMonths != 6 {
		t.Errorf("next_exam_months: want 6 for Mild NPDR high risk (HbA1c ≥8 + duration >10y), got %d", r.NextExamMonths)
	}
}

func TestOphthalmologist_NextExam_NoDR_Annual(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:         "p28",
		FundusDescription: "Normal fundus.",
		HbA1c:             f64ptr(6.8),
	}
	r := interpretOphthalmology(req)
	if r.NextExamMonths != 12 {
		t.Errorf("next_exam_months: want 12 for No DR low risk, got %d", r.NextExamMonths)
	}
}

func TestOphthalmologist_NextExam_NoDR_HighRisk_SixMonths(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:             "p29",
		FundusDescription:     "Normal fundus.",
		HbA1c:                 f64ptr(8.5),
		DurationDiabetesYears: intptr(12),
	}
	r := interpretOphthalmology(req)
	if r.NextExamMonths != 6 {
		t.Errorf("next_exam_months: want 6 for No DR high risk, got %d", r.NextExamMonths)
	}
}

// ---------------------------------------------------------------------------
// Referral urgency edge cases
// ---------------------------------------------------------------------------

func TestOphthalmologist_Urgency_Routine_NoDR_Normal(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:         "p30",
		DiagnosisContext:  "Annual diabetic eye exam",
		FundusDescription: "No diabetic changes.",
		VARightEye:        "20/20",
		VALeftEye:         "20/20",
		IOPRight:          f64ptr(16),
		IOPLeft:           f64ptr(15),
		CupDiscRatio:      f64ptr(0.4),
	}
	r := interpretOphthalmology(req)
	if r.ReferralUrgency != "routine" {
		t.Errorf("referral_urgency: want routine, got %q", r.ReferralUrgency)
	}
	if len(r.Recommendations) == 0 {
		t.Error("recommendations should not be empty even for routine exam")
	}
}

// PDR overrides everything else — same-day even if IOP and VA are normal.
func TestOphthalmologist_Urgency_PDR_OverridesNormalIOPAndVA(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:         "p31",
		FundusDescription: "NVD present with vitreous haemorrhage.",
		VARightEye:        "20/20",
		IOPRight:          f64ptr(16),
	}
	r := interpretOphthalmology(req)
	if r.ReferralUrgency != "same-day" {
		t.Errorf("PDR must be same-day regardless of VA/IOP, got %q", r.ReferralUrgency)
	}
}

// ---------------------------------------------------------------------------
// HbA1c counselling
// ---------------------------------------------------------------------------

func TestOphthalmologist_HighHbA1c_RecommendationPresent(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:         "p32",
		DiagnosisContext:  "Type 2 DM",
		FundusDescription: "No diabetic changes.",
		HbA1c:             f64ptr(9.0),
	}
	r := interpretOphthalmology(req)
	found := false
	for _, rec := range r.Recommendations {
		if containsSubstr(rec, "HbA1c") || containsSubstr(rec, "diabetes management") || containsSubstr(rec, "Endocrinology") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected HbA1c counselling recommendation, got: %v", r.Recommendations)
	}
}

func TestOphthalmologist_NormalHbA1c_NoCounsellingRec(t *testing.T) {
	req := ophthalConsultRequest{
		PatientID:         "p33",
		DiagnosisContext:  "Type 2 DM well-controlled",
		FundusDescription: "No diabetic changes.",
		HbA1c:             f64ptr(6.5),
	}
	r := interpretOphthalmology(req)
	for _, rec := range r.Recommendations {
		if containsSubstr(rec, "Endocrinology co-management") {
			t.Errorf("unexpected Endocrinology recommendation for normal HbA1c: %q", rec)
		}
	}
}

// ---------------------------------------------------------------------------
// Recommendations non-empty invariant
// ---------------------------------------------------------------------------

func TestOphthalmologist_Recommendations_AlwaysNonEmpty(t *testing.T) {
	cases := []ophthalConsultRequest{
		{PatientID: "a", FundusDescription: ""},
		{PatientID: "b", FundusDescription: "Normal fundus.", VARightEye: "20/20", VALeftEye: "20/20"},
		{PatientID: "c", FundusDescription: "Microaneurysms only."},
		{PatientID: "d", FundusDescription: "Neovascularization of the disc."},
		{PatientID: "e", IOPRight: f64ptr(40)},
	}
	for _, req := range cases {
		r := interpretOphthalmology(req)
		if len(r.Recommendations) == 0 {
			t.Errorf("patient=%q: recommendations must never be empty", req.PatientID)
		}
	}
}

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

func containsSubstr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || containsSubstrLoop(s, sub))
}

func containsSubstrLoop(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
