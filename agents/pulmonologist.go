package agents

// File pulmonologist.go: rule-based pulmonology specialist agent —
// interprets pulmonary function tests (PFTs), performs COPD GOLD staging,
// and assesses asthma severity for clinical decision support.
//
// Every numeric threshold in this file is illustrative — educational
// demo, not a clinical guideline. The textbook-reasonable cutoffs
// (FEV1/FVC < 0.70 for obstruction, GOLD grade boundaries, SpO2 ≤ 88%
// for O2 indication, DLCO < 40% for significant impairment) elide real
// clinical context: GOLD COPD staging requires post-bronchodilator
// spirometry, DLCO interpretation requires haemoglobin correction and
// lung volume context, asthma severity requires longitudinal assessment
// and ICS response data, and O2 prescription requires formal LTOT
// criteria (PaO2 ≤ 55 mmHg or SaO2 ≤ 88% on two occasions 3 weeks
// apart at rest). This file is intentionally simple so it can be
// reasoned about; a clinical-grade engine would require formal
// validation per FDA SaMD guidance.

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/agent"
)

// PulmonologistAgent interprets pulmonary function tests and performs
// COPD GOLD staging and asthma severity assessment.
// It is a rule-based specialist that generates pulmonology_report messages
// for routing to the supervisor, care planner, or human reviewer.
//
// Input message Type:  pulmonology_consult
// Content JSON:        pulmoConsultRequest (patient_id, case_id, fev1_percent,
//
//	fvc_percent, fev1_fvc_ratio, dlco_percent, spo2,
//	smoking_pack_years, symptoms, cat_score, mmrc_dyspnea)
//
// Output message Type: pulmonology_report
// Content JSON:        pulmoReport (pattern, copd_gold_stage, asthma_severity,
//
//	exacerbation_risk, o2_indicated, pulm_rehab_indicated,
//	recommendations, urgent)
//
// Output routes to:    supervisor (always)
//
// Zero-value-usable: no fields, no construction required.
type PulmonologistAgent struct{}

// pulmoConsultRequest is the wire format for incoming pulmonology_consult
// messages. All numeric fields are optional (zero value = not provided).
// Fields map directly to the clinical data elements needed by the rule engine:
//
//   - FEV1Percent: FEV1 as % of predicted (post-bronchodilator for COPD GOLD)
//   - FVCPercent: FVC as % of predicted
//   - FEV1FVCRatio: FEV1/FVC ratio (post-bronchodilator; <0.70 = obstruction)
//   - DLCOPercent: DLCO as % of predicted (gas exchange capacity)
//   - SpO2: resting peripheral oxygen saturation in percent (0–100)
//   - SmokingPackYears: cumulative pack-year history
//   - Symptoms: free-text symptom description
//   - CATScore: COPD Assessment Test score (0–40; ≥10 = high symptom burden)
//   - MMRCDyspnea: Modified Medical Research Council dyspnoea scale (0–4)
type pulmoConsultRequest struct {
	PatientID         string  `json:"patient_id"`
	CaseID            string  `json:"case_id"`
	FEV1Percent       float64 `json:"fev1_percent,omitempty"`
	FVCPercent        float64 `json:"fvc_percent,omitempty"`
	FEV1FVCRatio      float64 `json:"fev1_fvc_ratio,omitempty"`
	DLCOPercent       float64 `json:"dlco_percent,omitempty"`
	SpO2              float64 `json:"spo2,omitempty"`
	SmokingPackYears  float64 `json:"smoking_pack_years,omitempty"`
	Symptoms          string  `json:"symptoms,omitempty"`
	CATScore          int     `json:"cat_score,omitempty"`
	MMRCDyspnea       int     `json:"mmrc_dyspnea,omitempty"`
}

// pulmoReport is the wire format for outgoing pulmonology_report messages.
// All fields are populated by interpretPulmonology — field semantics follow
// GOLD 2023 / GINA 2023 textbook conventions (illustrative, not
// clinical-guideline-grade):
//
//   - Pattern: spirometric pattern ("normal", "obstruction", "restriction",
//     "mixed", or "insufficient data")
//   - COPDGOLDStage: GOLD grade + group string (e.g. "GOLD 2B — Moderate COPD,
//     Group B"), or empty if COPD criteria not met
//   - AsthmaSeverity: GINA step label ("intermittent", "mild persistent",
//     "moderate persistent", "severe persistent"), or empty if not applicable
//   - ExacerbationRisk: "low", "high", or empty
//   - O2Indicated: true when SpO2 ≤ 88% at rest (supplemental O2 indication)
//   - PulmRehabIndicated: true for moderate–severe COPD or significant
//     functional limitation
//   - Urgent: true when immediate escalation is warranted (SpO2 ≤ 88% or
//     DLCO < 40%)
//   - Recommendations: prioritised action list
type pulmoReport struct {
	Pattern             string   `json:"pattern"`
	COPDGOLDStage       string   `json:"copd_gold_stage"`
	AsthmaSeverity      string   `json:"asthma_severity"`
	ExacerbationRisk    string   `json:"exacerbation_risk"`
	O2Indicated         bool     `json:"o2_indicated"`
	PulmRehabIndicated  bool     `json:"pulm_rehab_indicated"`
	Recommendations     []string `json:"recommendations"`
	Urgent              bool     `json:"urgent"`
}

func (a *PulmonologistAgent) ID() string   { return "pulmonologist" }
func (a *PulmonologistAgent) Name() string { return "Pulmonology Specialist" }
func (a *PulmonologistAgent) Capabilities() []string {
	return []string{"pft_interpretation", "copd_staging", "asthma_assessment"}
}

// HandleMessage decodes a pulmonology_consult message, runs the rule
// engine, and emits a single pulmonology_report back to the supervisor.
//
// Error path: if the Content JSON cannot be decoded the agent returns the
// error so the orchestrator can emit a KindAgentError audit event.
// Partial failures inside interpretPulmonology (e.g. missing PFT values)
// produce a low-confidence report rather than an error, so the case
// keeps moving.
func (a *PulmonologistAgent) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	var req pulmoConsultRequest
	if err := json.Unmarshal([]byte(msg.Content), &req); err != nil {
		env.Logf("[pulmonologist] invalid pulmonology_consult: %v", err)
		return nil, err
	}

	report := interpretPulmonology(req)

	env.Logf("[pulmonologist] patient=%s pattern=%q copd_stage=%q asthma=%q urgent=%v o2=%v",
		req.PatientID, report.Pattern, report.COPDGOLDStage, report.AsthmaSeverity,
		report.Urgent, report.O2Indicated)
	env.Logf("[pulmonologist] exacerbation_risk=%q pulm_rehab=%v recs=%d",
		report.ExacerbationRisk, report.PulmRehabIndicated, len(report.Recommendations))

	content, _ := json.Marshal(report)
	meta := copyMeta(msg.Metadata)
	meta["patient_id"] = req.PatientID
	if req.CaseID != "" {
		meta["case_id"] = req.CaseID
	}

	out := agent.NewMessage(a.ID(), "supervisor", agent.RoleAgent,
		"pulmonology_report", string(content), meta)
	return []agent.Message{out}, nil
}

// interpretPulmonology applies rule-based pulmonology interpretation in
// order of clinical urgency. Rules are evaluated in priority order so
// the most critical condition drives the final Urgent flag and the
// recommendation list.
//
// Rule order (highest urgency first):
//
//  1. Supplemental O2 indication — SpO2 ≤ 88% at rest.
//     **Illustrative:** real LTOT requires two measurements 3 weeks
//     apart (PaO2 ≤ 55 mmHg or SaO2 ≤ 88%) per Medicare LTOT criteria.
//
//  2. DLCO < 40% — significant gas exchange impairment.
//     **Illustrative:** interpretation requires Hb correction, lung
//     volume, and clinical context. Isolated low DLCO may reflect
//     emphysema, ILD, pulmonary vascular disease, or anaemia.
//
//  3. PFT pattern classification:
//     - Obstruction: FEV1/FVC < 0.70
//     - Restriction: FVC < 80% predicted with normal or elevated FEV1/FVC
//     - Mixed: both obstruction and restriction present
//     **Illustrative thresholds.** Real interpretation uses LLN (lower
//     limit of normal, race/age/sex-adjusted) per ATS/ERS guidelines.
//
//  4. COPD GOLD spirometric grading (requires obstruction):
//     GOLD 1 (Mild): FEV1 ≥ 80% predicted
//     GOLD 2 (Moderate): 50–79%
//     GOLD 3 (Severe): 30–49%
//     GOLD 4 (Very Severe): < 30%
//     **Post-bronchodilator spirometry required.** Pre-BD values
//     overestimate reversibility and the GOLD grade.
//
//  5. GOLD symptom group (ABE framework, GOLD 2023):
//     Group A: low symptoms (CAT < 10, mMRC 0–1) + low exacerbation risk
//     Group B: high symptoms (CAT ≥ 10, mMRC ≥ 2) + low risk
//     Group E: high exacerbation risk (≥ 2/year or ≥ 1 hospitalisation)
//     **Illustrative.** Exacerbation history is inferred from symptoms
//     string here; real implementation requires structured EHR data.
//
//  6. Pulmonary rehabilitation indication:
//     Indicated for GOLD 2–4 or significant functional limitation.
//     **Illustrative.** Real PR referral considers comorbidities, patient
//     motivation, and programme availability.
//
//  7. Asthma severity assessment (GINA step classification):
//     Used when no persistent obstruction is present (or when obstruction
//     appears reversible based on symptoms).
//     Intermittent: symptoms ≤ 2 days/week (keyword "occasional" or
//     "rare" or "intermittent" or "2 days" in symptom string)
//     Mild persistent: symptoms > 2 days/week but not daily
//     Moderate persistent: daily symptoms
//     Severe persistent: continuous symptoms, limited activity
//     **Illustrative keyword matching only.** Real GINA severity requires
//     symptom frequency, nocturnal awakenings, rescue inhaler use, and
//     spirometric reversibility data.
//
// Returns a fully-populated pulmoReport; the Pattern field is always
// non-empty so downstream validators can rely on it.
func interpretPulmonology(req pulmoConsultRequest) pulmoReport {
	recs := []string{}
	report := pulmoReport{}
	syms := strings.ToLower(req.Symptoms)

	// ------------------------------------------------------------------ //
	// 1. Supplemental O2 indication — SpO2 ≤ 88% at rest.
	//
	// SpO2 ≤ 88% is the standard threshold for supplemental O2 prescription
	// in ambulatory COPD (CMS/Medicare LTOT criteria). Real LTOT requires
	// arterial blood gas confirmation (PaO2 ≤ 55 mmHg or 56–59 with cor
	// pulmonale / polycythaemia) on two occasions separated by 3 weeks
	// unless hospitalised. **Illustrative threshold — not a clinical guideline.**
	if req.SpO2 > 0 && req.SpO2 <= 88 {
		report.O2Indicated = true
		report.Urgent = true
		recs = append(recs, "Initiate supplemental O2 to maintain SpO2 ≥ 90%; titrate flow rate")
		recs = append(recs, "Obtain arterial blood gas to confirm O2 prescription and assess CO2 retention")
		recs = append(recs, "Urgent pulmonology evaluation for hypoxaemic respiratory failure workup")
	}

	// ------------------------------------------------------------------ //
	// 2. DLCO < 40% — significant gas exchange impairment → urgent.
	//
	// DLCO < 40% predicted indicates severe gas exchange impairment and
	// may reflect advanced emphysema, ILD, pulmonary hypertension, or
	// combined pulmonary fibrosis and emphysema (CPFE). Requires urgent
	// pulmonology referral. **Illustrative threshold only.**
	if req.DLCOPercent > 0 && req.DLCOPercent < 40 {
		report.Urgent = true
		recs = append(recs, "DLCO severely reduced (<40% predicted): urgent pulmonology referral for gas exchange assessment")
		recs = append(recs, "Consider CT chest and V/Q scan to evaluate for ILD, emphysema, or pulmonary vascular disease")
		recs = append(recs, "Haemoglobin-corrected DLCO and lung volume measurement (plethysmography) recommended")
	}

	// ------------------------------------------------------------------ //
	// 3. PFT pattern classification.
	//
	// Obstruction: FEV1/FVC < 0.70 (GOLD/ATS fixed ratio criterion).
	// Restriction: FVC < 80% predicted with normal or high FEV1/FVC, OR
	//   FVC < 80% in the presence of obstruction (possible mixed pattern;
	//   requires plethysmography to distinguish true restriction from
	//   air-trapping / pseudorestriction).
	// Mixed: FEV1/FVC < 0.70 AND FVC < 80% — clinically indicates mixed
	//   obstructive-restrictive physiology pending full lung volume data.
	// **Illustrative.** ATS/ERS 2022 guidelines recommend using z-scores
	// (LLN) rather than fixed ratios to reduce over-diagnosis in the
	// elderly and under-diagnosis in younger patients. Full lung volumes
	// (body plethysmography) are required to confirm true restriction when
	// obstruction is also present, as air-trapping raises RV and lowers
	// VC, mimicking restriction ("pseudorestriction").
	hasObstruction := req.FEV1FVCRatio > 0 && req.FEV1FVCRatio < 0.70
	hasFVCReduction := req.FVCPercent > 0 && req.FVCPercent < 80
	// Restriction without obstruction: low FVC with normal/elevated ratio.
	hasIsolatedRestriction := hasFVCReduction && (req.FEV1FVCRatio == 0 || req.FEV1FVCRatio >= 0.70)

	switch {
	case hasObstruction && hasFVCReduction:
		report.Pattern = "mixed"
		recs = append(recs, "Mixed obstructive-restrictive pattern: full lung volume measurement (body plethysmography) required to confirm restriction vs. air-trapping")
	case hasObstruction:
		report.Pattern = "obstruction"
	case hasIsolatedRestriction:
		report.Pattern = "restriction"
		recs = append(recs, "Restrictive pattern: refer for full lung volumes (plethysmography) and ILD/NMD workup")
	case req.FEV1FVCRatio > 0 || req.FVCPercent > 0 || req.FEV1Percent > 0:
		report.Pattern = "normal"
		recs = append(recs, "Spirometry within normal limits; correlate with clinical presentation")
	default:
		report.Pattern = "insufficient data"
		recs = append(recs, "Insufficient PFT data provided; spirometry recommended for complete assessment")
	}

	// ------------------------------------------------------------------ //
	// 4. COPD GOLD spirometric grading (requires obstruction).
	//
	// GOLD 2023 grades are based on post-bronchodilator FEV1 % predicted
	// in patients with FEV1/FVC < 0.70 (i.e. confirmed airflow obstruction).
	// **Post-bronchodilator requirement not verified here — illustrative.**
	if hasObstruction && req.FEV1Percent > 0 {
		goldGrade := copdGOLDGrade(req.FEV1Percent)
		goldGroup := copdGOLDGroup(req)

		// Obstruction severity narrative for recommendations.
		switch goldGrade {
		case "GOLD 1":
			// Mild obstruction — monitor and offer smoking cessation.
		case "GOLD 2":
			report.PulmRehabIndicated = true
			recs = append(recs, "Initiate LABA/LAMA dual bronchodilator therapy for GOLD 2 COPD")
		case "GOLD 3":
			report.PulmRehabIndicated = true
			recs = append(recs, "Escalate to LABA/LAMA/ICS triple therapy for GOLD 3 COPD; reassess response at 3 months")
		case "GOLD 4":
			report.PulmRehabIndicated = true
			report.Urgent = true
			recs = append(recs, "GOLD 4 very severe COPD: urgent pulmonology evaluation; assess for lung transplant/LVRS candidacy")
			recs = append(recs, "Initiate or continue triple therapy (LABA/LAMA/ICS); review home O2 and NIV need")
		}

		report.COPDGOLDStage = goldGrade + " — " + copdGradeName(goldGrade) + " COPD, Group " + goldGroup

		// Exacerbation risk from GOLD group.
		if goldGroup == "E" {
			report.ExacerbationRisk = "high"
			recs = append(recs, "High exacerbation risk (GOLD Group E): consider roflumilast or azithromycin prophylaxis if appropriate")
			recs = append(recs, "Ensure up-to-date influenza, pneumococcal, and COVID-19 vaccinations")
			recs = append(recs, "Provide written COPD action plan and rescue pack (prednisolone + antibiotic)")
		} else {
			report.ExacerbationRisk = "low"
		}

		// Smoking cessation for all COPD patients with pack-year history.
		if req.SmokingPackYears > 0 {
			recs = append(recs, "Smoking cessation: offer pharmacotherapy (varenicline preferred) + behavioural support")
		}

		// Pulmonary rehab recommendation text.
		if report.PulmRehabIndicated {
			recs = append(recs, "Refer to pulmonary rehabilitation programme (≥8 weeks supervised exercise + education)")
		}

		// SABA rescue inhaler for all symptomatic COPD.
		recs = append(recs, "Prescribe short-acting bronchodilator (SABA or SAMA) for rescue use")
	}

	// ------------------------------------------------------------------ //
	// 5. Asthma severity assessment (no persistent obstruction or when
	//    pattern is normal/insufficient and symptoms suggest asthma).
	//
	// Applied when COPD staging was not triggered (no confirmed obstruction
	// with FEV1% data) and symptoms are present.
	// **Illustrative keyword matching only — not GINA severity criteria.**
	if report.COPDGOLDStage == "" && req.Symptoms != "" {
		severity := asthmaSeverity(syms)
		if severity != "" {
			report.AsthmaSeverity = severity
			switch severity {
			case "intermittent":
				recs = append(recs, "Asthma intermittent: SABA rescue inhaler (salbutamol) as needed; avoid triggers")
			case "mild persistent":
				recs = append(recs, "Asthma mild persistent: initiate low-dose ICS (e.g. beclomethasone 100–200 mcg/day)")
				recs = append(recs, "Review inhaler technique and adherence at follow-up visit")
			case "moderate persistent":
				recs = append(recs, "Asthma moderate persistent: low-to-medium ICS/LABA combination therapy")
				recs = append(recs, "Assess for atopy, rhinitis, GERD, and obesity as contributing factors")
			case "severe persistent":
				report.Urgent = true
				recs = append(recs, "Asthma severe persistent: high-dose ICS/LABA; consider add-on tiotropium or biologics (anti-IL5, anti-IgE)")
				recs = append(recs, "Urgent specialist evaluation; assess for near-fatal asthma risk factors")
			}
			recs = append(recs, "Annual spirometry to monitor lung function trajectory")
		}
	}

	// ------------------------------------------------------------------ //
	// Fallback — no pathology detected or insufficient data.
	if report.Pattern == "normal" && report.COPDGOLDStage == "" && report.AsthmaSeverity == "" {
		recs = append(recs, "No obstructive or restrictive pattern identified; clinical correlation required")
		recs = append(recs, "Consider bronchoprovocation (methacholine) challenge if asthma is suspected with normal spirometry")
	}
	if report.Pattern == "insufficient data" && report.COPDGOLDStage == "" && report.AsthmaSeverity == "" {
		recs = append(recs, "Perform complete pulmonary function testing (spirometry, lung volumes, DLCO)")
	}

	report.Recommendations = dedupStrings(recs)
	return report
}

// copdGOLDGrade returns the GOLD spirometric grade string for a given
// FEV1 % predicted in a patient with confirmed airflow obstruction
// (FEV1/FVC < 0.70 post-bronchodilator):
//
//	GOLD 1 (Mild):       FEV1 ≥ 80%
//	GOLD 2 (Moderate):   50% ≤ FEV1 < 80%
//	GOLD 3 (Severe):     30% ≤ FEV1 < 50%
//	GOLD 4 (Very Severe): FEV1 < 30%
//
// **Illustrative thresholds — GOLD 2023, not validated here.**
func copdGOLDGrade(fev1Percent float64) string {
	switch {
	case fev1Percent >= 80:
		return "GOLD 1"
	case fev1Percent >= 50:
		return "GOLD 2"
	case fev1Percent >= 30:
		return "GOLD 3"
	default:
		return "GOLD 4"
	}
}

// copdGradeName returns the textual severity label for a GOLD grade string.
func copdGradeName(grade string) string {
	switch grade {
	case "GOLD 1":
		return "Mild"
	case "GOLD 2":
		return "Moderate"
	case "GOLD 3":
		return "Severe"
	case "GOLD 4":
		return "Very Severe"
	default:
		return "Unknown"
	}
}

// copdGOLDGroup returns the GOLD 2023 ABE symptom/risk group:
//
//	Group E: high exacerbation risk (≥2/year or ≥1 hospitalisation),
//	         inferred from symptom string keywords or high CAT/mMRC.
//	Group B: high symptom burden (CAT ≥10 or mMRC ≥2) + low risk.
//	Group A: low symptoms (CAT < 10 and mMRC 0–1) + low risk.
//
// **Exacerbation history is inferred from free-text symptoms here.
// Real implementation requires structured EHR exacerbation event data.**
func copdGOLDGroup(req pulmoConsultRequest) string {
	syms := strings.ToLower(req.Symptoms)

	// High exacerbation risk: explicit keywords or ≥2 exacerbations implied.
	if strings.Contains(syms, "hospitaliz") || strings.Contains(syms, "hospitalised") ||
		strings.Contains(syms, "hospitaliz") || strings.Contains(syms, "admitted") ||
		strings.Contains(syms, "exacerbation") {
		return "E"
	}

	// High symptom burden: CAT ≥10 or mMRC ≥2.
	if req.CATScore >= 10 || req.MMRCDyspnea >= 2 {
		return "B"
	}

	return "A"
}

// asthmaSeverity classifies asthma severity from a free-text symptom
// string using keyword matching. Returns one of:
//
//	"intermittent"        — symptoms ≤ 2 days/week or keywords: occasional,
//	                        rare, intermittent, "2 days"
//	"mild persistent"     — symptoms > 2 days/week but not daily
//	"moderate persistent" — daily symptoms
//	"severe persistent"   — continuous symptoms or limited activity
//
// Returns empty string if no asthma-indicative keywords are found.
//
// **Illustrative keyword matching only. GINA severity classification
// requires structured symptom frequency data, nocturnal awakenings,
// activity limitation, and spirometric reversibility — not free text.**
func asthmaSeverity(symsLower string) string {
	// Detect asthma-like context first.
	asthmaKeywords := []string{"wheez", "asthma", "bronchospasm", "tight", "shortness of breath", "dyspnoea", "dyspnea"}
	hasAsthmaContext := false
	for _, kw := range asthmaKeywords {
		if strings.Contains(symsLower, kw) {
			hasAsthmaContext = true
			break
		}
	}
	if !hasAsthmaContext {
		return ""
	}

	// Severity classification — most severe match wins.
	if strings.Contains(symsLower, "continuous") || strings.Contains(symsLower, "limited activity") ||
		strings.Contains(symsLower, "severely limited") || strings.Contains(symsLower, "severe persistent") {
		return "severe persistent"
	}
	if strings.Contains(symsLower, "daily") || strings.Contains(symsLower, "every day") ||
		strings.Contains(symsLower, "moderate persistent") {
		return "moderate persistent"
	}
	if strings.Contains(symsLower, "mild persistent") || strings.Contains(symsLower, "most days") ||
		strings.Contains(symsLower, "several") || strings.Contains(symsLower, "frequent") {
		return "mild persistent"
	}
	// Default asthma context without severity keywords → intermittent.
	if strings.Contains(symsLower, "occasional") || strings.Contains(symsLower, "rare") ||
		strings.Contains(symsLower, "intermittent") || strings.Contains(symsLower, "2 days") ||
		hasAsthmaContext {
		return "intermittent"
	}
	return ""
}
