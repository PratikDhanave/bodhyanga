package agents

// File ophthalmologist.go: rule-based ophthalmology specialist agent —
// interprets fundus findings, visual acuity, intraocular pressure, and
// diabetic markers to stage diabetic retinopathy, screen for diabetic
// macular edema, and flag glaucoma suspects.
//
// Every numeric threshold in this file is illustrative — educational demo,
// not a clinical guideline. ETDRS staging, IOP cutoffs, and cup-disc ratio
// thresholds reflect textbook-reasonable values; real clinical decisions
// require slit-lamp biomicroscopy, OCT, and a qualified ophthalmologist.
// This file is intentionally simple so it can be reasoned about; a
// clinical-grade engine would be 100× larger and require formal FDA SaMD
// validation.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/agent"
)

// OphthalmologistAgent performs rule-based diabetic retinopathy staging,
// diabetic macular edema (DME) screening, and glaucoma suspect assessment.
// It emits an ophthalmology_report message for routing to the supervisor,
// care planner, or human reviewer.
//
// Input message Type:  ophthalmology_consult
// Content JSON:        ophthalConsultRequest (see below)
//
// Output message Type: ophthalmology_report
// Content JSON:        ophthalReport (see below)
//
// Zero-value-usable: no fields, no construction required.
type OphthalmologistAgent struct{}

func (a *OphthalmologistAgent) ID() string   { return "ophthalmologist" }
func (a *OphthalmologistAgent) Name() string { return "Ophthalmology Specialist" }
func (a *OphthalmologistAgent) Capabilities() []string {
	return []string{"retinopathy_screening", "glaucoma_assessment", "eye_exam_interpretation"}
}

// ophthalConsultRequest is the wire format for incoming ophthalmology_consult
// messages. Optional fields carry a pointer so the rule engine can distinguish
// "not measured" from a zero value (e.g. IOP=0 vs not provided).
//
//   - PatientID: EHR patient identifier
//   - CaseID: orchestration case identifier (may be empty)
//   - DiagnosisContext: free-text clinical context (e.g. "Type 2 DM, 12y")
//   - VARightEye / VALeftEye: Snellen fraction string (e.g. "20/40")
//   - IOPRight / IOPLeft: intraocular pressure in mmHg
//   - FundusDescription: free-text fundus exam or OCT narrative
//   - CupDiscRatio: vertical cup-to-disc ratio (0.0–1.0)
//   - HbA1c: glycated haemoglobin in percent
//   - BPSystolic: systolic blood pressure in mmHg
//   - DurationDiabetesYears: years since diabetes diagnosis
type ophthalConsultRequest struct {
	PatientID             string   `json:"patient_id"`
	CaseID                string   `json:"case_id,omitempty"`
	DiagnosisContext      string   `json:"diagnosis_context"`
	VARightEye            string   `json:"va_right,omitempty"`
	VALeftEye             string   `json:"va_left,omitempty"`
	IOPRight              *float64 `json:"iop_right,omitempty"`
	IOPLeft               *float64 `json:"iop_left,omitempty"`
	FundusDescription     string   `json:"fundus_description,omitempty"`
	CupDiscRatio          *float64 `json:"cup_disc_ratio,omitempty"`
	HbA1c                 *float64 `json:"hba1c,omitempty"`
	BPSystolic            *int     `json:"bp_systolic,omitempty"`
	DurationDiabetesYears *int     `json:"duration_diabetes_years,omitempty"`
}

// ophthalReport is the wire format for outgoing ophthalmology_report messages.
//
//   - DiabeticRetinopathyStage: ETDRS stage label (No DR, Mild NPDR, …, PDR)
//   - DiabeticMacularEdema: true when DME indicators found in fundus text
//   - GlaucomaSuspect: true when IOP or cup-disc criteria are met
//   - VisionThreatening: true for PDR, DME, or legal blindness VA
//   - ReferralUrgency: "same-day" | "within-1-week" | "routine"
//   - Recommendations: prioritised action list
//   - NextExamMonths: recommended interval until next eye examination
type ophthalReport struct {
	DiabeticRetinopathyStage string   `json:"diabetic_retinopathy_stage"`
	DiabeticMacularEdema     bool     `json:"diabetic_macular_edema"`
	GlaucomaSuspect          bool     `json:"glaucoma_suspect"`
	VisionThreatening        bool     `json:"vision_threatening"`
	ReferralUrgency          string   `json:"referral_urgency"`
	Recommendations          []string `json:"recommendations"`
	NextExamMonths           int      `json:"next_exam_months"`
}

// HandleMessage decodes an ophthalmology_consult message, runs the rule
// engine, and emits a single ophthalmology_report back to the requesting
// agent (msg.From).
//
// Error path: if the Content JSON cannot be decoded the error is returned
// to the orchestrator for audit. Partial data (missing optional fields)
// produces a conservative report rather than an error.
func (a *OphthalmologistAgent) HandleMessage(_ context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	var req ophthalConsultRequest
	if err := json.Unmarshal([]byte(msg.Content), &req); err != nil {
		env.Logf("[ophthalmologist] invalid ophthalmology_consult: %v", err)
		return nil, err
	}

	report := interpretOphthalmology(req)

	env.Logf("[ophthalmologist] patient=%s dr_stage=%q dme=%v glaucoma_suspect=%v vision_threatening=%v urgency=%q",
		req.PatientID,
		report.DiabeticRetinopathyStage,
		report.DiabeticMacularEdema,
		report.GlaucomaSuspect,
		report.VisionThreatening,
		report.ReferralUrgency,
	)

	content, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}

	meta := copyMeta(msg.Metadata)
	meta["patient_id"] = req.PatientID
	if req.CaseID != "" {
		meta["case_id"] = req.CaseID
	}

	out := agent.NewMessage(a.ID(), msg.From, agent.RoleAgent,
		"ophthalmology_report", string(content), meta)
	return []agent.Message{out}, nil
}

// interpretOphthalmology orchestrates all rule groups and assembles the
// final ophthalReport. Rules are evaluated in the following order:
//
//  1. Visual acuity — flag legal blindness / driving-limit VA.
//  2. DME screening — keyword match in fundus description.
//  3. Diabetic retinopathy staging — ETDRS-inspired rules.
//  4. Glaucoma suspect assessment — IOP and cup-disc thresholds.
//  5. Referral urgency — highest-priority finding drives the level.
//  6. Next exam interval — based on DR stage and risk factors.
func interpretOphthalmology(req ophthalConsultRequest) ophthalReport {
	fundus := strings.ToLower(req.FundusDescription)

	// --- 1. Visual acuity ------------------------------------------------
	vaImpaired, vaLegalBlind := assessVisualAcuity(req.VARightEye, req.VALeftEye)

	// --- 2. Diabetic macular edema ----------------------------------------
	dme := detectDME(fundus)

	// --- 3. Diabetic retinopathy staging (ETDRS) --------------------------
	drStage := stageDiabeticRetinopathy(fundus)

	// --- 4. Glaucoma suspect ----------------------------------------------
	glaucomaSuspect, iopUrgent, iopSameDay := assessGlaucoma(req.IOPRight, req.IOPLeft, req.CupDiscRatio)

	// --- 5. Vision threatening --------------------------------------------
	visionThreatening := drStage == "PDR" || dme || vaLegalBlind

	// --- 6. Referral urgency ---------------------------------------------
	urgency := computeOphthUrgency(drStage, dme, vaImpaired, vaLegalBlind, iopUrgent, iopSameDay)

	// --- 7. Recommendations -----------------------------------------------
	recs := buildOphthRecommendations(req, drStage, dme, glaucomaSuspect, iopUrgent, iopSameDay, vaImpaired, vaLegalBlind)

	// --- 8. Next exam interval -------------------------------------------
	nextExam := computeNextExamInterval(drStage, dme, req.HbA1c, req.DurationDiabetesYears)

	return ophthalReport{
		DiabeticRetinopathyStage: drStage,
		DiabeticMacularEdema:     dme,
		GlaucomaSuspect:          glaucomaSuspect,
		VisionThreatening:        visionThreatening,
		ReferralUrgency:          urgency,
		Recommendations:          recs,
		NextExamMonths:           nextExam,
	}
}

// ---------------------------------------------------------------------------
// Visual acuity helpers
// ---------------------------------------------------------------------------

// vaSnellenTable maps standard Snellen fractions (as printed strings) to their
// decimal equivalent for comparison. Values outside this table are treated as
// unknown (not impaired).
//
// Key thresholds:
//   - 20/40  — driving limit recognised by most US states
//   - 20/200 — legal blindness threshold (SSDI / ADA definition)
var vaSnellenTable = map[string]float64{
	"20/20":  1.00,
	"20/25":  0.80,
	"20/30":  0.67,
	"20/40":  0.50, // driving limit
	"20/50":  0.40,
	"20/60":  0.33,
	"20/80":  0.25,
	"20/100": 0.20,
	"20/200": 0.10, // legal blindness
	"20/400": 0.05,
	"cf":     0.02,  // counting fingers
	"hm":     0.01,  // hand motion
	"lp":     0.005, // light perception
	"nlp":    0.0,   // no light perception
}

// assessVisualAcuity returns (impaired, legalBlind) flags for the better eye.
// "Impaired" means VA worse than 20/40 in the better eye.
// "LegalBlind" means VA ≤ 20/200 in the better eye.
//
// If either eye string is absent the better-eye logic uses only the provided
// value; if both are absent (empty) impaired=false, legalBlind=false.
func assessVisualAcuity(vaRight, vaLeft string) (impaired, legalBlind bool) {
	rNorm := strings.ToLower(strings.TrimSpace(vaRight))
	lNorm := strings.ToLower(strings.TrimSpace(vaLeft))

	decode := func(s string) (float64, bool) {
		if s == "" {
			return 0, false
		}
		v, ok := vaSnellenTable[s]
		return v, ok
	}

	rVal, rOK := decode(rNorm)
	lVal, lOK := decode(lNorm)

	if !rOK && !lOK {
		return false, false
	}

	// Best eye = highest decimal acuity.
	best := -1.0
	if rOK {
		best = rVal
	}
	if lOK && lVal > best {
		best = lVal
	}

	// 20/40 → decimal 0.50; worse means < 0.50.
	impaired = best < 0.50
	// 20/200 → decimal 0.10; legal blindness means ≤ 0.10.
	legalBlind = best <= 0.10
	return
}

// ---------------------------------------------------------------------------
// DME detection
// ---------------------------------------------------------------------------

// detectDME returns true when the lower-cased fundus description contains
// keywords that suggest diabetic macular edema.
//
// Recognised indicators (ETDRS / CSME criteria, illustrative):
//   - "macular edema"
//   - "hard exudates near fovea"  (CSME: within 500 µm of foveal centre)
//   - "retinal thickening"        (OCT-based criterion)
//   - "clinically significant macular edema" / "csme"
func detectDME(fundusLower string) bool {
	dmeKeywords := []string{
		"macular edema",
		"hard exudates near fovea",
		"retinal thickening",
		"clinically significant macular edema",
		"csme",
	}
	for _, kw := range dmeKeywords {
		if strings.Contains(fundusLower, kw) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Diabetic retinopathy staging (ETDRS-inspired)
// ---------------------------------------------------------------------------

// stageDiabeticRetinopathy assigns an ETDRS-derived stage label from the
// lower-cased fundus description text.
//
// ETDRS staging hierarchy (evaluated highest-first so the most severe
// matching stage wins):
//
//	PDR:              neovascularisation of any kind present.
//	Severe NPDR:      4-2-1 rule — haemorrhages in 4 quadrants, venous
//	                  beading in 2+, IRMA in 1+.
//	Moderate NPDR:    microaneurysms + any of cotton-wool spots, hard
//	                  exudates, haemorrhage (not meeting severe criteria).
//	Mild NPDR:        microaneurysms only.
//	No DR:            no retinopathy indicators found.
//
// **Illustrative keyword matching — not a validated clinical algorithm.**
func stageDiabeticRetinopathy(fundusLower string) string {
	// PDR: neovascularisation is the defining feature.
	if strings.Contains(fundusLower, "neovascularization") ||
		strings.Contains(fundusLower, "neovascularisation") ||
		strings.Contains(fundusLower, "new vessels") ||
		strings.Contains(fundusLower, "nvd") || // new vessels on disc
		strings.Contains(fundusLower, "nve") { // new vessels elsewhere
		return "PDR"
	}

	// Severe NPDR: 4-2-1 rule components.
	hasFourQuadHem := strings.Contains(fundusLower, "hemorrhages in 4 quadrants") ||
		strings.Contains(fundusLower, "haemorrhages in 4 quadrants") ||
		strings.Contains(fundusLower, "4-quadrant hemorrhage") ||
		strings.Contains(fundusLower, "4-quadrant haemorrhage")
	hasVenousBeading := strings.Contains(fundusLower, "venous beading")
	hasIRMA := strings.Contains(fundusLower, "irma") ||
		strings.Contains(fundusLower, "intraretinal microvascular")

	if hasFourQuadHem || (hasVenousBeading && hasIRMA) || (hasFourQuadHem && hasVenousBeading) {
		return "Severe NPDR"
	}

	// Moderate NPDR: microaneurysms + additional features.
	hasMicroaneurysms := strings.Contains(fundusLower, "microaneurysm")
	hasAdditional := strings.Contains(fundusLower, "cotton-wool") ||
		strings.Contains(fundusLower, "cotton wool") ||
		strings.Contains(fundusLower, "hard exudate") ||
		strings.Contains(fundusLower, "flame hemorrhage") ||
		strings.Contains(fundusLower, "dot hemorrhage") ||
		strings.Contains(fundusLower, "blot hemorrhage") ||
		strings.Contains(fundusLower, "dot haemorrhage") ||
		strings.Contains(fundusLower, "blot haemorrhage") ||
		hasVenousBeading || hasIRMA

	if hasMicroaneurysms && hasAdditional {
		return "Moderate NPDR"
	}

	// Mild NPDR: microaneurysms only.
	if hasMicroaneurysms {
		return "Mild NPDR"
	}

	return "No DR"
}

// ---------------------------------------------------------------------------
// Glaucoma assessment
// ---------------------------------------------------------------------------

// maxIOP returns the higher of two optional IOP values, or 0 if both nil.
func maxIOP(right, left *float64) float64 {
	var max float64
	if right != nil {
		max = *right
	}
	if left != nil && *left > max {
		max = *left
	}
	return max
}

// assessGlaucoma returns:
//
//   - suspect:  true when IOP > 21 mmHg or CDR > 0.6 (illustrative thresholds)
//   - urgent:   true when IOP > 25 mmHg (within-1-week referral trigger)
//   - sameDay:  true when IOP > 35 mmHg (same-day emergency referral)
//
// IOP normal range: 10–21 mmHg (Goldmann applanation tonometry).
// Cup-disc ratio > 0.6 is a recognised glaucoma suspect criterion;
// asymmetry > 0.2 between eyes also raises suspicion (not implemented here).
//
// **Illustrative thresholds only — not a validated clinical algorithm.**
func assessGlaucoma(iopRight, iopLeft *float64, cdr *float64) (suspect, urgent, sameDay bool) {
	iop := maxIOP(iopRight, iopLeft)

	if iop > 21 {
		suspect = true
	}
	if iop > 25 {
		urgent = true
	}
	if iop > 35 {
		sameDay = true
	}

	if cdr != nil && *cdr > 0.6 {
		suspect = true
	}

	return
}

// ---------------------------------------------------------------------------
// Referral urgency
// ---------------------------------------------------------------------------

// computeOphthUrgency returns the highest-priority referral urgency string
// based on the assessed findings.
//
// Hierarchy (highest first):
//
//	"same-day":      PDR, legal blindness, IOP > 35 mmHg.
//	"within-1-week": DME, new vision change (driving-limit impairment), IOP > 25 mmHg.
//	"routine":       stable findings, screening, Mild/Moderate NPDR without DME.
func computeOphthUrgency(drStage string, dme, vaImpaired, vaLegalBlind bool, iopUrgent, iopSameDay bool) string {
	if drStage == "PDR" || vaLegalBlind || iopSameDay {
		return "same-day"
	}
	if dme || vaImpaired || iopUrgent {
		return "within-1-week"
	}
	return "routine"
}

// ---------------------------------------------------------------------------
// Recommendations
// ---------------------------------------------------------------------------

// buildOphthRecommendations assembles a prioritised recommendation list.
// Entries are deduplicated (first-occurrence order preserved).
func buildOphthRecommendations(
	req ophthalConsultRequest,
	drStage string,
	dme, glaucomaSuspect, iopUrgent, iopSameDay bool,
	vaImpaired, vaLegalBlind bool,
) []string {
	var recs []string

	// PDR — highest-urgency DR finding.
	if drStage == "PDR" {
		recs = append(recs,
			"Same-day retina specialist referral for PDR evaluation",
			"Consider pan-retinal photocoagulation (PRP) or anti-VEGF therapy",
			"Urgent vitreoretinal surgery evaluation if vitreous haemorrhage or tractional retinal detachment suspected",
		)
	}

	// DME.
	if dme {
		recs = append(recs,
			"Urgent retina referral for diabetic macular edema (DME) evaluation",
			"Consider intravitreal anti-VEGF injection (ranibizumab, aflibercept, or bevacizumab)",
			"Optimise glycaemic control: target HbA1c < 7% per ADA guidelines",
			"Optimise blood pressure: target < 130/80 mmHg",
		)
	}

	// Severe NPDR — high risk of progression.
	if drStage == "Severe NPDR" {
		recs = append(recs,
			"Retina specialist referral within 1–2 weeks for Severe NPDR",
			"Discuss risk of progression to PDR; consider early PRP",
			"Intensify glycaemic and blood pressure control",
		)
	}

	// Moderate NPDR.
	if drStage == "Moderate NPDR" {
		recs = append(recs,
			"Ophthalmology follow-up in 3–6 months for Moderate NPDR surveillance",
			"Reinforce glycaemic control and blood pressure management",
		)
	}

	// Mild NPDR.
	if drStage == "Mild NPDR" {
		recs = append(recs,
			"Annual dilated fundus examination for Mild NPDR",
			"Patient education on DR progression risk factors",
		)
	}

	// Glaucoma / elevated IOP.
	if iopSameDay {
		recs = append(recs,
			"Same-day ophthalmology referral for IOP > 35 mmHg (acute angle-closure or uncontrolled open-angle glaucoma)",
			"Emergent IOP-lowering therapy (topical timolol, IV acetazolamide, pilocarpine)",
		)
	} else if iopUrgent {
		recs = append(recs,
			"Ophthalmology referral within 1 week for IOP > 25 mmHg",
			"Consider initiating topical prostaglandin analogue or beta-blocker",
		)
	} else if glaucomaSuspect {
		recs = append(recs,
			"Comprehensive glaucoma evaluation: visual fields, OCT nerve fibre layer, gonioscopy",
			"Repeat IOP measurement in 4–6 weeks",
		)
	}

	// Legal blindness / severe VA impairment.
	if vaLegalBlind {
		recs = append(recs,
			"Urgent ophthalmology evaluation for visual acuity ≤ 20/200 (legal blindness threshold)",
			"Low vision rehabilitation referral",
			"SSDI / disability assessment documentation if appropriate",
		)
	} else if vaImpaired {
		recs = append(recs,
			"Ophthalmology evaluation for visual acuity worse than driving limit (20/40)",
			"Advise patient on driving restrictions pending ophthalmology assessment",
		)
	}

	// HbA1c-based metabolic counselling.
	if req.HbA1c != nil && *req.HbA1c >= 8.0 {
		recs = append(recs,
			fmt.Sprintf("HbA1c %.1f%% — intensify diabetes management; target < 7%% per ADA guidelines (illustrative)", *req.HbA1c),
			"Endocrinology co-management recommended for poorly controlled diabetes",
		)
	}

	// Fallback — no actionable findings.
	if len(recs) == 0 {
		recs = append(recs,
			"No vision-threatening findings identified at this examination",
			"Continue routine diabetic eye screening per ADA guidelines",
		)
	}

	return dedupStrings(recs)
}

// ---------------------------------------------------------------------------
// Next exam interval
// ---------------------------------------------------------------------------

// computeNextExamInterval returns the recommended months until the next
// dilated fundus examination.
//
// Interval logic (based on ADA Standards of Medical Care in Diabetes,
// illustrative — not a clinical guideline):
//
//	PDR or DME:                              3 months (intensive follow-up)
//	Severe NPDR:                             3–4 months
//	Moderate NPDR:                           6 months
//	High-risk profile (HbA1c ≥ 8% AND duration > 10 years): 6 months
//	Mild NPDR:                               12 months
//	No DR, HbA1c < 7%, short duration:       12 months
//	Default (No DR, otherwise):              12 months
func computeNextExamInterval(drStage string, dme bool, hba1c *float64, durationYears *int) int {
	switch {
	case drStage == "PDR" || dme:
		return 3
	case drStage == "Severe NPDR":
		return 3
	case drStage == "Moderate NPDR":
		return 6
	case drStage == "Mild NPDR":
		// High-risk: HbA1c ≥ 8 AND duration > 10 years → shorten to 6 months.
		if hba1c != nil && *hba1c >= 8.0 && durationYears != nil && *durationYears > 10 {
			return 6
		}
		return 12
	default:
		// No DR — interval based on risk factors.
		// Low risk: HbA1c < 7 (good control) → annual is sufficient.
		// High risk combination → 6 months.
		if hba1c != nil && *hba1c >= 8.0 && durationYears != nil && *durationYears > 10 {
			return 6
		}
		return 12
	}
}
