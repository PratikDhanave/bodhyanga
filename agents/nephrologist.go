package agents

// File nephrologist.go: rule-based nephrology specialist agent —
// calculates eGFR, stages CKD, detects AKI, and classifies proteinuria
// for clinical decision support.
//
// Every numeric threshold in this file is illustrative — educational
// demo, not a clinical guideline. The CKD-EPI (2021) formula, KDIGO
// AKI staging, and KDIGO CKD staging are simplified representations:
// real CKD-EPI requires validated age/sex/creatinine inputs including
// correction for lab calibration; KDIGO AKI staging requires clinical
// context (oliguria, timing, and urine output criteria in addition to
// creatinine); and CKD staging requires both eGFR and albuminuria
// categories assessed at least twice ≥90 days apart. This file is
// intentionally simple so it can be reasoned about; a clinical-grade
// engine would be substantially larger and require formal validation
// per FDA SaMD guidance.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"github.com/PratikDhanave/bodhyanga/pkg/agent"
)

// NephrologistAgent evaluates renal function from laboratory data and
// generates a structured nephrology_report. It is a rule-based specialist
// that routes its output to the supervisor for downstream care planning
// or human review.
//
// Input message Type:  nephrology_consult
// Content JSON:        nephroConsultRequest
//
// Output message Type: nephrology_report
// Content JSON:        nephroReport
//
// Output routes to:    supervisor (always)
//
// Zero-value-usable: no fields, no construction required.
type NephrologistAgent struct{}

// nephroConsultRequest is the wire format for incoming nephrology_consult
// messages. Optional fields (BUN, urine_protein, potassium, bicarbonate,
// prior_creatinine) use pointer semantics so the rule engine can
// distinguish "not provided" from a zero value.
//
//   - CreatinineMgDl: serum creatinine in mg/dL (required)
//   - BUN: blood urea nitrogen in mg/dL (optional)
//   - Age: patient age in years (required)
//   - Sex: "male" or "female" (required for CKD-EPI, case-insensitive)
//   - WeightKg: body weight in kg (required for dosing calculations)
//   - UrineProtein: urine protein-to-creatinine ratio in g/g (optional)
//   - Potassium: serum potassium in mEq/L (optional)
//   - Bicarbonate: serum bicarbonate in mEq/L (optional)
//   - PriorCreatinine: baseline creatinine in mg/dL for AKI delta (optional)
type nephroConsultRequest struct {
	PatientID       string   `json:"patient_id"`
	CaseID          string   `json:"case_id"`
	CreatinineMgDl  float64  `json:"creatinine_mg_dl"`
	BUN             *float64 `json:"bun,omitempty"`
	Age             int      `json:"age"`
	Sex             string   `json:"sex"`
	WeightKg        float64  `json:"weight_kg"`
	UrineProtein    *float64 `json:"urine_protein,omitempty"`
	Potassium       *float64 `json:"potassium,omitempty"`
	Bicarbonate     *float64 `json:"bicarbonate,omitempty"`
	PriorCreatinine *float64 `json:"prior_creatinine,omitempty"`
}

// nephroReport is the wire format for outgoing nephrology_report messages.
// All fields are populated by interpretNephrology.
//
//   - EGFRMlMin: estimated GFR in mL/min/1.73 m² from CKD-EPI (2021)
//   - CKDStage: KDIGO G-stage label (G1–G5)
//   - AKIStage: KDIGO AKI stage 0–3 (0 = no AKI detected or no baseline)
//   - AKICause: probable mechanism when AKI is detected (illustrative)
//   - ProteinuriaCategory: KDIGO A1/A2/A3 label (empty if not provided)
//   - DialysisIndicated: true for CKD G5 (eGFR < 15) or AKI stage 3
//   - Recommendations: prioritised action list
//   - Urgent: true when immediate escalation is warranted
type nephroReport struct {
	EGFRMlMin            float64  `json:"egfr_ml_min"`
	CKDStage             string   `json:"ckd_stage"`
	AKIStage             int      `json:"aki_stage"`
	AKICause             string   `json:"aki_cause"`
	ProteinuriaCategory  string   `json:"proteinuria_category"`
	DialysisIndicated    bool     `json:"dialysis_indicated"`
	Recommendations      []string `json:"recommendations"`
	Urgent               bool     `json:"urgent"`
}

func (a *NephrologistAgent) ID() string   { return "nephrologist" }
func (a *NephrologistAgent) Name() string { return "Nephrology Specialist" }
func (a *NephrologistAgent) Capabilities() []string {
	return []string{"ckd_staging", "dialysis_adequacy", "aki_assessment"}
}

// HandleMessage decodes a nephrology_consult message, runs the rule
// engine, and emits a single nephrology_report back to the supervisor.
//
// Error path: if the Content JSON cannot be decoded the agent returns the
// error so the orchestrator can emit a KindAgentError audit event.
// Partial failures inside interpretNephrology (e.g. missing optional
// fields) produce a best-effort report rather than an error, so the case
// keeps moving.
func (a *NephrologistAgent) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	var req nephroConsultRequest
	if err := json.Unmarshal([]byte(msg.Content), &req); err != nil {
		env.Logf("[nephrologist] invalid nephrology_consult: %v", err)
		return nil, err
	}

	report := interpretNephrology(req)

	env.Logf("[nephrologist] patient=%s egfr=%.1f ckd_stage=%s aki_stage=%d urgent=%v",
		req.PatientID, report.EGFRMlMin, report.CKDStage, report.AKIStage, report.Urgent)
	if report.DialysisIndicated {
		env.Logf("[nephrologist] dialysis indicated for patient=%s", req.PatientID)
	}

	content, _ := json.Marshal(report)
	meta := copyMeta(msg.Metadata)
	meta["patient_id"] = req.PatientID
	if req.CaseID != "" {
		meta["case_id"] = req.CaseID
	}

	out := agent.NewMessage(a.ID(), "supervisor", agent.RoleAgent,
		"nephrology_report", string(content), meta)
	return []agent.Message{out}, nil
}

// interpretNephrology applies rule-based nephrology interpretation in
// order of clinical urgency. Rules are evaluated in priority order so
// the most critical condition drives the final Urgent flag and the
// recommendation list.
//
// Rule order (highest urgency first):
//
//  1. eGFR calculation via CKD-EPI (2021, race-free formula).
//     **Illustrative simplification:** the published CKD-EPI (2021) formula
//     requires calibrated creatinine assays traceable to IDMS reference
//     material. This implementation uses the published coefficients but
//     elides lab calibration correction. Not validated against the original
//     CKiD or CKD-EPI cohorts.
//
//  2. CKD staging (KDIGO 2012 G-stages G1–G5).
//     **Illustrative:** real CKD diagnosis requires eGFR < 60 or markers
//     of kidney damage persisting ≥3 months. A single eGFR measurement
//     cannot diagnose CKD. G5 triggers dialysis_indicated=true.
//
//  3. AKI detection (KDIGO 2012 creatinine criteria, stages 1–3).
//     Only evaluated when prior_creatinine is provided. Stage 3 triggers
//     dialysis_indicated=true and urgent=true.
//     **Illustrative:** KDIGO also uses urine output criteria (< 0.5 mL/kg/h
//     for ≥6 h) which are not captured in this data model.
//
//  4. Proteinuria classification (KDIGO A1/A2/A3).
//     Only evaluated when urine_protein (PCR g/g) is provided.
//
//  5. Hyperkalemia detection — potassium > 5.5 mEq/L urgent;
//     > 6.5 mEq/L emergent.
//     **Illustrative thresholds — not a clinical guideline.**
//
//  6. Metabolic acidosis — bicarbonate < 15 mEq/L urgent.
//     **Illustrative threshold — not a clinical guideline.**
//
// Returns a fully-populated nephroReport; Recommendations is never nil.
func interpretNephrology(req nephroConsultRequest) nephroReport {
	recs := []string{}
	report := nephroReport{}

	// 1. eGFR via CKD-EPI (2021, race-free).
	//
	// Formula (Inker et al., NEJM 2021):
	//   eGFR = 142 × min(scr/κ, 1)^α × max(scr/κ, 1)^(-1.200)
	//          × 0.9938^age × (1.012 if female)
	// where:
	//   κ = 0.7 (female) or 0.9 (male)
	//   α = -0.241 (female) or -0.302 (male)
	//
	// **Illustrative:** does not apply lab-calibration correction.
	// Not validated against the CKD-EPI 2021 development cohort.
	egfr := calcEGFR(req.CreatinineMgDl, req.Age, req.Sex)
	report.EGFRMlMin = math.Round(egfr*10) / 10

	// 2. CKD staging (KDIGO G1–G5).
	//
	// G1: ≥90   — normal or high eGFR
	// G2: 60–89 — mildly decreased
	// G3a: 45–59 — mild-to-moderately decreased
	// G3b: 30–44 — moderately to severely decreased
	// G4: 15–29 — severely decreased
	// G5: < 15  — kidney failure
	//
	// **Illustrative:** a single measurement is not diagnostic.
	ckdStage, stageDesc := ckdStageFromEGFR(egfr)
	report.CKDStage = ckdStage

	switch {
	case egfr < 15:
		// G5 — kidney failure
		report.DialysisIndicated = true
		report.Urgent = true
		recs = append(recs, "Urgent nephrology consult: eGFR <15 mL/min/1.73m² ("+stageDesc+") — dialysis evaluation required")
		recs = append(recs, "Assess for renal replacement therapy (haemodialysis, peritoneal dialysis, or transplant evaluation)")
		recs = append(recs, "Review and adjust all renally-cleared medications; avoid nephrotoxins")
		recs = append(recs, "Monitor fluid balance, electrolytes, and acid-base status closely")

	case egfr < 30:
		// G4 — severely decreased
		report.Urgent = true
		recs = append(recs, fmt.Sprintf("Nephrology referral required: eGFR %.1f mL/min/1.73m² (%s)", egfr, stageDesc))
		recs = append(recs, "Prepare for renal replacement therapy — initiate vascular access planning if haemodialysis anticipated")
		recs = append(recs, "Strict avoidance of nephrotoxins (NSAIDs, IV contrast without pre-hydration, aminoglycosides)")
		recs = append(recs, "Aggressive cardiovascular risk reduction (BP <130/80, statin, ACEi/ARB if tolerated)")

	case egfr < 45:
		// G3b — moderate-severe
		recs = append(recs, fmt.Sprintf("Nephrology referral recommended: eGFR %.1f mL/min/1.73m² (%s)", egfr, stageDesc))
		recs = append(recs, "Optimise blood pressure (target <130/80 mmHg); use ACEi/ARB if proteinuria present")
		recs = append(recs, "Avoid nephrotoxins; dose-adjust renally-cleared medications")
		recs = append(recs, "Monitor for complications: anaemia, hyperphosphataemia, secondary hyperparathyroidism")

	case egfr < 60:
		// G3a — mild-moderate
		recs = append(recs, fmt.Sprintf("Chronic kidney disease screening: eGFR %.1f mL/min/1.73m² (%s)", egfr, stageDesc))
		recs = append(recs, "Repeat eGFR in 3 months to confirm chronicity")
		recs = append(recs, "Blood pressure optimisation and metabolic management")

	case egfr < 90:
		// G2 — mildly decreased
		recs = append(recs, fmt.Sprintf("Mildly decreased eGFR %.1f mL/min/1.73m² (%s) — assess for markers of kidney damage", egfr, stageDesc))
		recs = append(recs, "Check urine albumin-to-creatinine ratio to confirm or exclude CKD")

	default:
		// G1 — normal/high
		recs = append(recs, fmt.Sprintf("eGFR %.1f mL/min/1.73m² (%s) — normal or high renal function", egfr, stageDesc))
	}

	// 3. AKI detection (KDIGO creatinine criteria).
	//
	// Requires prior_creatinine as baseline. KDIGO stages by ratio:
	//   Stage 1: ≥1.5× baseline within 7 days OR rise ≥0.3 mg/dL in 48h
	//   Stage 2: ≥2× baseline
	//   Stage 3: ≥3× baseline OR absolute creatinine ≥4.0 mg/dL
	//
	// **Illustrative:** urine output criteria are not modelled here.
	// A "48h" rise ≥0.3 is inferred from the prior_creatinine field
	// without explicit timestamp; the caller must ensure the prior value
	// represents a ≤48h baseline when using the acute rise criterion.
	if req.PriorCreatinine != nil && *req.PriorCreatinine > 0 {
		baseline := *req.PriorCreatinine
		ratio := req.CreatinineMgDl / baseline
		acuteRise := req.CreatinineMgDl - baseline

		switch {
		case ratio >= 3.0 || req.CreatinineMgDl >= 4.0:
			report.AKIStage = 3
			report.AKICause = "AKI stage 3 — probable severe acute tubular injury or obstructive/vascular cause"
			report.DialysisIndicated = true
			report.Urgent = true
			recs = append(recs, "AKI Stage 3: emergent nephrology consult — urgent renal replacement therapy evaluation")
			recs = append(recs, "Identify and reverse precipitant: volume depletion, sepsis, obstruction, nephrotoxin, or vascular event")
			recs = append(recs, "Strict hourly urine output monitoring; Foley catheter if not in situ")
			recs = append(recs, "Hold ACEi, ARBs, NSAIDs, and renally-cleared nephrotoxic agents")

		case ratio >= 2.0:
			report.AKIStage = 2
			report.AKICause = "AKI stage 2 — probable acute tubular injury; evaluate for pre-renal, intrinsic, or post-renal causes"
			report.Urgent = true
			recs = append(recs, "AKI Stage 2: urgent nephrology consult recommended")
			recs = append(recs, "Volume assessment: urine sodium, FENa, renal ultrasound to exclude obstruction")
			recs = append(recs, "Daily electrolytes, creatinine, and urine output monitoring")
			recs = append(recs, "Hold nephrotoxic agents; avoid IV contrast")

		case ratio >= 1.5 || acuteRise >= 0.3:
			report.AKIStage = 1
			report.AKICause = "AKI stage 1 — acute creatinine rise; evaluate for pre-renal causes (dehydration, hypoperfusion)"
			recs = append(recs, "AKI Stage 1: identify and treat precipitant — assess volume status")
			recs = append(recs, "Repeat creatinine and electrolytes in 24–48h")
			recs = append(recs, "Ensure adequate hydration; avoid nephrotoxins")

		default:
			report.AKIStage = 0 // no AKI by KDIGO creatinine criteria
		}
	}

	// 4. Proteinuria classification (KDIGO A1/A2/A3).
	//
	// Uses urine protein-to-creatinine ratio (PCR) in g/g:
	//   A1: < 0.03 g/g  — normal to mildly increased
	//   A2: 0.03–0.30   — moderately increased (microalbuminuria range)
	//   A3: > 0.30      — severely increased (macroalbuminuria)
	//
	// **Illustrative:** KDIGO 2012 uses albumin-to-creatinine ratio (ACR)
	// as the primary metric; PCR is used here as a simplified proxy.
	if req.UrineProtein != nil {
		pcr := *req.UrineProtein
		switch {
		case pcr > 0.30:
			report.ProteinuriaCategory = "A3"
			recs = append(recs, fmt.Sprintf("Severely increased proteinuria (PCR %.2f g/g, A3) — nephrology referral; consider ACEi/ARB therapy", pcr))
			recs = append(recs, "24-hour urine protein collection to quantify proteinuria; evaluate for glomerulonephritis")
		case pcr >= 0.03:
			report.ProteinuriaCategory = "A2"
			recs = append(recs, fmt.Sprintf("Moderately increased proteinuria (PCR %.2f g/g, A2) — initiate ACEi/ARB if not contraindicated", pcr))
			recs = append(recs, "Screen for diabetic nephropathy; optimise blood pressure and glycaemic control")
		default:
			report.ProteinuriaCategory = "A1"
			recs = append(recs, fmt.Sprintf("Normal to mildly increased proteinuria (PCR %.2f g/g, A1)", pcr))
		}
	}

	// 5. Hyperkalemia — potassium > 5.5 mEq/L.
	//
	// Thresholds (illustrative — not a clinical guideline):
	//   5.5–6.5 mEq/L: urgent — risk of cardiac arrhythmia
	//   > 6.5 mEq/L:  emergent — high risk of life-threatening arrhythmia
	if req.Potassium != nil {
		k := *req.Potassium
		switch {
		case k > 6.5:
			report.Urgent = true
			recs = append(recs, fmt.Sprintf("EMERGENT hyperkalaemia (K⁺ %.1f mEq/L >6.5): immediate cardiac monitoring, IV calcium gluconate, insulin/dextrose, sodium bicarbonate", k))
			recs = append(recs, "Emergent dialysis consideration if refractory to medical management")
			recs = append(recs, "Identify cause: haemolysis, cell lysis, rhabdomyolysis, medication effect (ACEi/ARB/MRA/SGLT2i), adrenal insufficiency")
		case k > 5.5:
			report.Urgent = true
			recs = append(recs, fmt.Sprintf("Urgent hyperkalaemia (K⁺ %.1f mEq/L >5.5): ECG for peaked T-waves; consider potassium binders (patiromer, sodium zirconium cyclosilicate)", k))
			recs = append(recs, "Dietary potassium restriction; review and adjust potassium-sparing medications")
		}
	}

	// 6. Metabolic acidosis — bicarbonate < 15 mEq/L.
	//
	// Bicarbonate < 15 mEq/L in the context of CKD or AKI suggests
	// uraemic acidosis or high-anion-gap metabolic acidosis requiring
	// urgent evaluation and likely sodium bicarbonate supplementation.
	// **Illustrative threshold — not a clinical guideline.**
	if req.Bicarbonate != nil {
		bicarb := *req.Bicarbonate
		if bicarb < 15 {
			report.Urgent = true
			recs = append(recs, fmt.Sprintf("Severe metabolic acidosis (HCO₃⁻ %.1f mEq/L <15): urgent nephrology consult; consider IV or oral sodium bicarbonate", bicarb))
			recs = append(recs, "Arterial blood gas and anion gap to characterise acidosis; rule out lactic acidosis, ketoacidosis, and toxic ingestions")
		} else if bicarb < 22 {
			recs = append(recs, fmt.Sprintf("Mild-moderate metabolic acidosis (HCO₃⁻ %.1f mEq/L) — evaluate for uraemic or renal tubular acidosis; oral sodium bicarbonate supplementation if CKD-related", bicarb))
		}
	}

	// Fallback recommendation when no actionable rule fired (normal
	// renal function with no AKI, no electrolyte disturbance).
	if len(recs) == 0 {
		recs = append(recs, "Renal function within acceptable parameters for available data — routine nephrology follow-up as clinically indicated")
	}

	report.Recommendations = dedupStrings(recs)
	return report
}

// calcEGFR computes eGFR in mL/min/1.73 m² using the CKD-EPI (2021)
// race-free equation (Inker et al., NEJM 2021):
//
//	eGFR = 142 × min(scr/κ, 1)^α × max(scr/κ, 1)^(-1.200) × 0.9938^age
//	       × (1.012 if female)
//
// Parameters:
//   - scrMgDl: serum creatinine in mg/dL (must be > 0)
//   - age: patient age in years
//   - sex: "female" or "male" (case-insensitive; defaults to male coefficients)
//
// **Illustrative:** does not apply IDMS calibration correction.
// Returns 0.0 for invalid inputs (scrMgDl ≤ 0 or age ≤ 0).
func calcEGFR(scrMgDl float64, age int, sex string) float64 {
	if scrMgDl <= 0 || age <= 0 {
		return 0.0
	}

	var kappa, alpha, sexFactor float64
	isFemale := len(sex) >= 1 && (sex[0] == 'f' || sex[0] == 'F')

	if isFemale {
		kappa = 0.7
		alpha = -0.241
		sexFactor = 1.012
	} else {
		kappa = 0.9
		alpha = -0.302
		sexFactor = 1.0
	}

	ratio := scrMgDl / kappa
	minPart := math.Pow(math.Min(ratio, 1.0), alpha)
	maxPart := math.Pow(math.Max(ratio, 1.0), -1.200)
	ageFactor := math.Pow(0.9938, float64(age))

	return 142.0 * minPart * maxPart * ageFactor * sexFactor
}

// ckdStageFromEGFR returns the KDIGO CKD G-stage label and a short
// descriptive string for a given eGFR value (mL/min/1.73 m²).
//
// Stages follow KDIGO 2012 Clinical Practice Guideline for CKD:
//   G1  ≥ 90:  normal or high
//   G2  60–89: mildly decreased
//   G3a 45–59: mild-to-moderately decreased
//   G3b 30–44: moderately to severely decreased
//   G4  15–29: severely decreased
//   G5  < 15:  kidney failure
//
// **Illustrative:** a single eGFR value is insufficient to diagnose CKD
// per KDIGO; persistent abnormality ≥3 months is required.
func ckdStageFromEGFR(egfr float64) (stage, desc string) {
	switch {
	case egfr >= 90:
		return "G1", "normal or high"
	case egfr >= 60:
		return "G2", "mildly decreased"
	case egfr >= 45:
		return "G3a", "mild-to-moderately decreased"
	case egfr >= 30:
		return "G3b", "moderately to severely decreased"
	case egfr >= 15:
		return "G4", "severely decreased"
	default:
		return "G5", "kidney failure"
	}
}
