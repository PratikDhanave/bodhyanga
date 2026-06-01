package agents

// File cardiologist.go: rule-based cardiology specialist agent —
// interprets ECG findings and cardiac biomarkers for clinical decision
// support.
//
// Every numeric threshold in this file is illustrative — educational
// demo, not a clinical guideline. The textbook-reasonable cutoffs
// (troponin > 0.04, BNP > 400, EF < 40, TIMI components) elide real
// clinical context: AHA/ACC STEMI criteria require validated 12-lead
// ECG interpretation, Fourth Universal Definition of MI requires
// rise-and-fall patterns, NYHA/ACC heart failure staging requires
// comprehensive clinical evaluation, and TIMI risk score uses
// validated registry populations. This file is intentionally simple so
// it can be reasoned about; a clinical-grade engine would be 100× larger
// and require formal validation per FDA SaMD guidance.

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/agent"
)

// CardiologistAgent interprets ECG findings and cardiac biomarkers.
// It is a rule-based specialist that generates cardiology_report messages
// for routing to the supervisor, care planner, or human reviewer.
//
// Input message Type:  cardiology_consult
// Content JSON:        cardioConsultRequest (patient_id, case_id, ecg_findings,
//
//	troponin, bnp, ef_percent)
//
// Output message Type: cardiology_report
// Content JSON:        cardioReport (interpretation, stemi_equivalent,
//
//	heart_failure_class, risk_score, urgent, recommendations)
//
// Output routes to:    supervisor (always)
//
// Zero-value-usable: no fields, no construction required.
type CardiologistAgent struct{}

// cardioConsultRequest is the wire format for incoming cardiology_consult
// messages. Fields map directly to the clinical data elements needed by
// the rule engine:
//
//   - ECGFindings: free-text ECG interpretation string (e.g.
//     "ST elevation V1-V4, reciprocal changes in II, III, aVF")
//   - Troponin: high-sensitivity troponin I or T in ng/mL
//   - BNP: B-type natriuretic peptide in pg/mL
//   - EFPercent: left ventricular ejection fraction in percent (0–100)
type cardioConsultRequest struct {
	PatientID   string  `json:"patient_id"`
	CaseID      string  `json:"case_id"`
	ECGFindings string  `json:"ecg_findings"`
	Troponin    float64 `json:"troponin"`
	BNP         float64 `json:"bnp"`
	EFPercent   float64 `json:"ef_percent"`
}

// cardioReport is the wire format for outgoing cardiology_report messages.
// All fields are populated by interpretCardiology — the field semantics
// follow the ACC/AHA/ESC textbook conventions (illustrative, not
// clinical-guideline-grade):
//
//   - Interpretation: narrative summary of the assessment
//   - STEMIEquivalent: true when ST elevation in ≥2 contiguous leads is
//     mentioned in ECGFindings (simple string match)
//   - HeartFailureClass: NYHA/HFrEF/HFpEF label (empty if no CHF signal)
//   - RiskScore: simplified TIMI-ACS-like score 0–7 (illustrative)
//   - Urgent: true when immediate escalation is warranted
//   - Recommendations: prioritised action list
type cardioReport struct {
	Interpretation     string   `json:"interpretation"`
	STEMIEquivalent    bool     `json:"stemi_equivalent"`
	HeartFailureClass  string   `json:"heart_failure_class"`
	RiskScore          float64  `json:"risk_score"`
	Urgent             bool     `json:"urgent"`
	Recommendations    []string `json:"recommendations"`
}

func (a *CardiologistAgent) ID() string   { return "cardiologist" }
func (a *CardiologistAgent) Name() string { return "Cardiology Specialist" }
func (a *CardiologistAgent) Capabilities() []string {
	return []string{"ecg_interpretation", "cardiac_risk_assessment"}
}

// HandleMessage decodes a cardiology_consult message, runs the rule
// engine, and emits a single cardiology_report back to the supervisor.
//
// Error path: if the Content JSON cannot be decoded the agent returns the
// error so the orchestrator can emit a KindAgentError audit event.
// Partial failures inside interpretCardiology (e.g. empty ECG string)
// produce a low-confidence report rather than an error, so the case
// keeps moving.
func (a *CardiologistAgent) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	var req cardioConsultRequest
	if err := json.Unmarshal([]byte(msg.Content), &req); err != nil {
		env.Logf("[cardiologist] invalid cardiology_consult: %v", err)
		return nil, err
	}

	report := interpretCardiology(req)

	env.Logf("[cardiologist] patient=%s stemi=%v urgent=%v risk=%.1f hf_class=%q",
		req.PatientID, report.STEMIEquivalent, report.Urgent, report.RiskScore, report.HeartFailureClass)
	env.Logf("[cardiologist] interpretation: %s", report.Interpretation)

	content, _ := json.Marshal(report)
	meta := copyMeta(msg.Metadata)
	meta["patient_id"] = req.PatientID
	if req.CaseID != "" {
		meta["case_id"] = req.CaseID
	}

	out := agent.NewMessage(a.ID(), "supervisor", agent.RoleAgent,
		"cardiology_report", string(content), meta)
	return []agent.Message{out}, nil
}

// interpretCardiology applies rule-based cardiology interpretation in
// order of clinical urgency. Rules are evaluated in priority order so
// the most critical condition drives the final Urgent flag and the
// recommendation list.
//
// Rule order (highest urgency first):
//
//  1. STEMI equivalent — ST elevation in ≥2 contiguous leads.
//     **Illustrative:** real STEMI requires validated 12-lead ECG criteria
//     per AHA/ACC STEMI guidelines (LBBB, posterior MI, etc.). The
//     string-match approach here is a demo approximation only.
//
//  2. Complete heart block / third-degree AV block — urgent pacing.
//
//  3. NSTEMI/ACS — troponin > 0.04 ng/mL.
//     **Illustrative threshold only.** Modern hs-cTn uses sex-specific
//     cutoffs in the pg/mL range with delta protocols.
//
//  4. Atrial fibrillation — rhythm control recommendation.
//
//  5. Decompensated CHF — EF < 40 (HFrEF) or BNP > 400 (decompensation).
//     **Illustrative:** real NYHA staging requires comprehensive
//     functional assessment.
//
//  6. Risk scoring — simplified TIMI-like score for ACS context.
//     **Illustrative.** Real TIMI uses 7 validated variables from a
//     registry population.
//
// Returns a fully-populated cardioReport; the Interpretation string is
// always non-empty so downstream validators can rely on it.
func interpretCardiology(req cardioConsultRequest) cardioReport {
	ecg := strings.ToLower(req.ECGFindings)
	recs := []string{}
	report := cardioReport{}

	// 1. STEMI equivalent — ST elevation in ≥2 contiguous leads.
	//
	// Simple string match on "st elevation" in the ECG findings text.
	// This approximates the ACC/AHA criterion of ≥1 mm in ≥2 contiguous
	// leads in standard leads (or ≥2 mm in V1–V4). Real interpretation
	// requires a calibrated digitised ECG or a trained human reader.
	// **Not a clinical guideline.**
	if strings.Contains(ecg, "st elevation") {
		report.STEMIEquivalent = true
		report.Urgent = true
		recs = append(recs, "Activate STEMI protocol: immediate cath lab notification")
		recs = append(recs, "Administer dual antiplatelet therapy (aspirin + P2Y12 inhibitor) per protocol")
		recs = append(recs, "Obtain stat 12-lead ECG and continuous cardiac monitoring")
		report.Interpretation = "STEMI equivalent pattern detected. " +
			"ST elevation in ≥2 contiguous leads consistent with acute transmural ischaemia. " +
			"Emergent revascularisation indicated (illustrative — not a clinical guideline)."
	}

	// 2. Complete heart block / third-degree AV block — urgent pacing.
	//
	// Matches "complete heart block" or "third degree" in the ECG string.
	// Real third-degree block requires verification of P-wave/QRS
	// dissociation on a rhythm strip; string matching is a demo proxy.
	// **Not a clinical guideline.**
	if strings.Contains(ecg, "complete heart block") || strings.Contains(ecg, "third degree") {
		report.Urgent = true
		recs = append(recs, "Urgent cardiology consult for transvenous pacing consideration")
		recs = append(recs, "Apply transcutaneous pacing pads; prepare for temporary pacing")
		recs = append(recs, "Hold AV-nodal blocking agents (beta-blockers, calcium channel blockers, digoxin)")
		if report.Interpretation == "" {
			report.Interpretation = "Complete (third-degree) heart block identified. " +
				"AV dissociation carries risk of haemodynamic compromise. " +
				"Urgent pacing evaluation warranted (illustrative — not a clinical guideline)."
		} else {
			report.Interpretation += " Concomitant complete heart block noted — pacing evaluation required."
		}
	}

	// 3. NSTEMI / ACS — troponin > 0.04 ng/mL.
	//
	// **Illustrative threshold only.** The Fourth Universal Definition of
	// MI (2018) requires detection of a rising and/or falling pattern
	// of cardiac troponin with at least one value above the 99th
	// percentile upper reference limit (URL), plus evidence of acute
	// myocardial ischaemia. The 0.04 ng/mL cutoff is a textbook
	// non-high-sensitivity assay reference — real practice uses
	// sex-specific high-sensitivity URLs in the ng/L range.
	if req.Troponin > 0.04 {
		report.Urgent = true
		recs = append(recs, "Serial troponins q3–6h to establish rise-and-fall pattern")
		recs = append(recs, "Urgent cath lab evaluation for possible NSTEMI/ACS")
		recs = append(recs, "Anticoagulation per ACS protocol (heparin or enoxaparin)")
		if report.Interpretation == "" {
			report.Interpretation = "Elevated troponin (>0.04 ng/mL illustrative threshold) consistent with NSTEMI/ACS. " +
				"Serial biomarkers and cardiology evaluation recommended."
		} else {
			report.Interpretation += " Elevated troponin supports acute myocardial injury."
		}
	}

	// 4. Atrial fibrillation — rhythm control recommendation.
	//
	// Matches "atrial fibrillation" or common abbreviations "afib"/"af "
	// in the ECG string. Real AF management depends on haemodynamic
	// stability, symptom duration (>48 h anticoagulation before
	// cardioversion), CHA₂DS₂-VASc score, and comorbidities. This
	// rule adds generic rhythm-control advice only.
	// **Not a clinical guideline.**
	if strings.Contains(ecg, "atrial fibrillation") || strings.Contains(ecg, "afib") ||
		strings.Contains(ecg, "af ") {
		recs = append(recs, "Rate control: consider beta-blocker or non-dihydropyridine calcium channel blocker")
		recs = append(recs, "Assess CHA₂DS₂-VASc score; initiate anticoagulation if score ≥2 (men) or ≥3 (women)")
		recs = append(recs, "Evaluate for reversible causes: thyrotoxicosis, sepsis, PE, electrolyte disturbance")
		if report.Interpretation == "" {
			report.Interpretation = "Atrial fibrillation identified on ECG. Rate and rhythm strategy evaluation needed."
		} else {
			report.Interpretation += " Atrial fibrillation detected — rhythm and anticoagulation management required."
		}
	}

	// 5. Heart failure classification — EF < 40 (HFrEF) and BNP > 400
	//    (decompensation marker).
	//
	// EF thresholds follow the 2022 AHA/ACC/HFSA heart failure guidelines
	// (illustrative): EF < 40% = HFrEF, 40–49% = HFmrEF, ≥50% = HFpEF.
	// BNP > 400 pg/mL indicates likely decompensated heart failure; age-
	// and body-weight-adjusted cut-offs exist in real practice.
	// **Illustrative thresholds only — not clinical guidelines.**
	if req.EFPercent > 0 && req.EFPercent < 40 {
		report.HeartFailureClass = "HFrEF class III/IV"
		recs = append(recs, "Initiate/optimise guideline-directed medical therapy (GDMT): ACEi/ARB/ARNI, beta-blocker, MRA, SGLT2i")
		recs = append(recs, "Cardiac resynchronisation therapy (CRT) evaluation if QRS ≥150 ms with LBBB morphology")
		if report.Interpretation == "" {
			report.Interpretation = "Reduced ejection fraction (EF <40%) consistent with HFrEF class III/IV. GDMT optimisation indicated."
		} else {
			report.Interpretation += " Reduced EF (<40%) indicates HFrEF — GDMT optimisation required."
		}
	}
	if req.BNP > 400 {
		if report.HeartFailureClass == "" {
			report.HeartFailureClass = "decompensated heart failure"
		} else {
			report.HeartFailureClass += " (decompensated)"
		}
		report.Urgent = true
		recs = append(recs, "IV diuresis (furosemide) for acute decompensation; monitor renal function and electrolytes")
		recs = append(recs, "Daily weights, strict fluid balance; consider fluid restriction")
		if report.Interpretation == "" {
			report.Interpretation = "Markedly elevated BNP (>400 pg/mL illustrative threshold) consistent with decompensated heart failure."
		} else {
			report.Interpretation += " Markedly elevated BNP indicates acute decompensation."
		}
	}

	// 6. Simplified TIMI-like risk score for ACS context.
	//
	// The real TIMI Risk Score for UA/NSTEMI (Antman 2000) has 7 binary
	// variables each scored 0 or 1 (age ≥65, ≥3 CAD risk factors, known
	// CAD with ≥50% stenosis, ST deviation ≥0.5mm, ≥2 anginal events in
	// 24h, aspirin use in prior 7 days, elevated cardiac markers).
	// Score ≥5 is high risk (≥40% MACE at 14 days).
	//
	// Here we approximate with available structured fields — each adds 1
	// point to the 0–7 scale. **Illustrative only; not validated.**
	score := computeSimplifiedTIMI(req, ecg)
	report.RiskScore = score
	if score >= 5 {
		recs = append(recs, "High TIMI risk (≥5): early invasive strategy within 24h recommended")
	} else if score >= 3 {
		recs = append(recs, "Intermediate TIMI risk (3–4): invasive strategy within 48–72h; continue monitoring")
	}

	// Fallback interpretation when no rule fired (e.g. normal ECG and
	// normal biomarkers). Produces a non-urgent, no-action report so the
	// supervisor can finalise the consult without hanging.
	if report.Interpretation == "" {
		report.Interpretation = "ECG and biomarker findings within normal limits for the data provided. " +
			"No acute coronary, arrhythmia, or heart failure pattern identified by rule engine. " +
			"Clinical correlation required."
		recs = append(recs, "Routine cardiology follow-up as clinically indicated")
	}

	// Deduplicate recommendations while preserving insertion order.
	// Rules may append similar entries when multiple conditions coexist
	// (e.g. STEMI + complete heart block both add monitoring items).
	report.Recommendations = dedupStrings(recs)
	return report
}

// computeSimplifiedTIMI returns a 0–7 simplified TIMI-like score using
// the fields available in the consult request. Each component adds 1 point:
//
//  1. Troponin > 0.04 ng/mL (elevated cardiac marker)
//  2. BNP > 400 pg/mL (volume overload / ischaemia burden)
//  3. EF < 40% (reduced systolic function — independent risk)
//  4. ST elevation present in ECG (STEMI-equivalent injury pattern)
//  5. Troponin > 1.0 ng/mL (markedly elevated — higher risk ACS)
//  6. Atrial fibrillation present (thromboembolic risk, haemodynamic)
//  7. Complete heart block present (haemodynamic instability risk)
//
// **These weights are illustrative and not validated against any clinical
// outcome dataset. Do not use for real patient care decisions.**
func computeSimplifiedTIMI(req cardioConsultRequest, ecgLower string) float64 {
	var score float64
	if req.Troponin > 0.04 {
		score++
	}
	if req.BNP > 400 {
		score++
	}
	if req.EFPercent > 0 && req.EFPercent < 40 {
		score++
	}
	if strings.Contains(ecgLower, "st elevation") {
		score++
	}
	if req.Troponin > 1.0 {
		score++ // markedly elevated → extra point
	}
	if strings.Contains(ecgLower, "atrial fibrillation") || strings.Contains(ecgLower, "afib") {
		score++
	}
	if strings.Contains(ecgLower, "complete heart block") || strings.Contains(ecgLower, "third degree") {
		score++
	}
	if score > 7 {
		score = 7 // cap at validated TIMI max
	}
	return score
}

// dedupStrings returns a copy of ss with duplicate strings removed,
// preserving first-occurrence order. Used to consolidate recommendations
// when multiple rules fire on the same consult.
func dedupStrings(ss []string) []string {
	seen := make(map[string]bool, len(ss))
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
