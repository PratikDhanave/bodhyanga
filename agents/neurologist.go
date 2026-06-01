package agents

// File neurologist.go: rule-based neurology specialist agent —
// interprets clinical presentations for stroke assessment, seizure
// classification, and neuro scoring for clinical decision support.
//
// Every numeric threshold in this file is illustrative — educational
// demo, not a clinical guideline. The textbook-reasonable cutoffs
// (NIHSS ≥6 for LVO, tPA window 4.5h, BP 185/110 relative contraindication)
// elide real clinical context: AHA/ASA stroke guidelines require validated
// multimodal imaging, IV tPA eligibility requires comprehensive inclusion/
// exclusion criteria per NINDS/ECASS protocols, mechanical thrombectomy
// requires trained interventional neuroradiology, and seizure classification
// follows ILAE 2017 taxonomy with EEG correlation. This file is intentionally
// simple so it can be reasoned about; a clinical-grade engine would be 100×
// larger and require formal validation per FDA SaMD guidance.

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/agent"
)

// NeurologistAgent interprets neurology consult requests.
// It is a rule-based specialist that generates neurology_report messages
// for routing to the supervisor, care planner, or human reviewer.
//
// Input message Type:  neurology_consult
// Content JSON:        neuroConsultRequest (patient_id, case_id, presentation,
//
//	onset_hours, nihss_score, bp_systolic, glucose, imaging)
//
// Output message Type: neurology_report
// Content JSON:        neuroReport (diagnosis, nihss_score, stroke_type,
//
//	lvo_suspected, tpa_eligible, thrombectomy_window,
//	seizure_type, recommendations, urgent)
//
// Output routes to:    supervisor (always)
//
// Zero-value-usable: no fields, no construction required.
type NeurologistAgent struct{}

// neuroConsultRequest is the wire format for incoming neurology_consult
// messages. Fields map directly to the clinical data elements needed by
// the rule engine:
//
//   - Presentation: free-text clinical description (symptoms, history)
//   - OnsetHours: hours since symptom onset (0 = unknown/just occurred)
//   - NIHSSScore: NIH Stroke Scale score (0–42); 0 may mean not assessed
//   - BPSystolic: systolic blood pressure in mmHg (0 = not provided)
//   - Glucose: blood glucose in mmol/L (0 = not provided)
//   - Imaging: free-text imaging results (e.g. "CT shows hyperdense MCA sign")
type neuroConsultRequest struct {
	PatientID  string  `json:"patient_id"`
	CaseID     string  `json:"case_id"`
	Presentation string `json:"presentation"`
	OnsetHours float64 `json:"onset_hours"`
	NIHSSScore *int    `json:"nihss_score,omitempty"`
	BPSystolic *int    `json:"bp_systolic,omitempty"`
	Glucose    *float64 `json:"glucose,omitempty"`
	Imaging    string  `json:"imaging,omitempty"`
}

// neuroReport is the wire format for outgoing neurology_report messages.
// All fields are populated by interpretNeurology — the field semantics
// follow AHA/ASA and ILAE textbook conventions (illustrative, not
// clinical-guideline-grade):
//
//   - Diagnosis: primary diagnosis or differential (narrative string)
//   - NIHSSScore: NIHSS score used (provided or estimated)
//   - StrokeType: cardioembolic | large vessel atherosclerosis | small vessel | undetermined
//   - LVOSuspected: true when large vessel occlusion criteria met (NIHSS ≥6)
//   - TPAEligible: true when within 4.5h window and no flagged contraindications
//   - ThrombectomyWindow: true when within 24h and LVO suspected
//   - SeizureType: generalized tonic-clonic | focal onset | absence | provoked | undetermined
//   - Recommendations: prioritised action list
//   - Urgent: true when immediate escalation is warranted
type neuroReport struct {
	Diagnosis          string   `json:"diagnosis"`
	NIHSSScore         int      `json:"nihss_score"`
	StrokeType         string   `json:"stroke_type,omitempty"`
	LVOSuspected       bool     `json:"lvo_suspected"`
	TPAEligible        bool     `json:"tpa_eligible"`
	ThrombectomyWindow bool     `json:"thrombectomy_window"`
	SeizureType        string   `json:"seizure_type,omitempty"`
	Recommendations    []string `json:"recommendations"`
	Urgent             bool     `json:"urgent"`
}

func (a *NeurologistAgent) ID() string   { return "neurologist" }
func (a *NeurologistAgent) Name() string { return "Neurology Specialist" }
func (a *NeurologistAgent) Capabilities() []string {
	return []string{"stroke_assessment", "seizure_classification", "neuro_scoring"}
}

// HandleMessage decodes a neurology_consult message, runs the rule
// engine, and emits a single neurology_report back to the supervisor.
//
// Error path: if the Content JSON cannot be decoded the agent returns the
// error so the orchestrator can emit a KindAgentError audit event.
// Partial failures inside interpretNeurology (e.g. empty presentation)
// produce a low-confidence report rather than an error, so the case
// keeps moving.
func (a *NeurologistAgent) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	var req neuroConsultRequest
	if err := json.Unmarshal([]byte(msg.Content), &req); err != nil {
		env.Logf("[neurologist] invalid neurology_consult: %v", err)
		return nil, err
	}

	report := interpretNeurology(req)

	env.Logf("[neurologist] patient=%s nihss=%d lvo=%v tpa_eligible=%v urgent=%v stroke_type=%q seizure_type=%q",
		req.PatientID, report.NIHSSScore, report.LVOSuspected, report.TPAEligible,
		report.Urgent, report.StrokeType, report.SeizureType)
	env.Logf("[neurologist] diagnosis: %s", report.Diagnosis)

	content, _ := json.Marshal(report)
	meta := copyMeta(msg.Metadata)
	meta["patient_id"] = req.PatientID
	if req.CaseID != "" {
		meta["case_id"] = req.CaseID
	}

	out := agent.NewMessage(a.ID(), "supervisor", agent.RoleAgent,
		"neurology_report", string(content), meta)
	return []agent.Message{out}, nil
}

// interpretNeurology applies rule-based neurology interpretation in order
// of clinical urgency. Rules are evaluated in priority order so the most
// critical condition drives the final Urgent flag and the recommendation list.
//
// Rule order (highest urgency first):
//
//  1. Stroke recognition — FAST+ criteria (facial droop, arm weakness,
//     speech difficulty, sudden headache).
//     **Illustrative:** real stroke assessment requires trained clinical
//     evaluation and validated multimodal imaging.
//
//  2. tPA eligibility — onset < 4.5h and no flagged contraindications
//     (BP >185/110 is noted as relative contraindication).
//     **Illustrative threshold only.** Real IV alteplase eligibility requires
//     comprehensive NINDS/ECASS III inclusion/exclusion criteria including
//     prior anticoagulation, recent surgery, platelet count, INR, etc.
//
//  3. Thrombectomy window — onset < 24h and LVO suspected (NIHSS ≥6).
//     **Illustrative.** Real endovascular therapy eligibility per DAWN/DEFUSE-3
//     requires perfusion imaging, ASPECTS score, and interventional team.
//
//  4. Stroke type classification — cardioembolic, large vessel atherosclerosis,
//     small vessel (lacunar syndromes).
//     **Illustrative.** Real TOAST classification requires full diagnostic workup.
//
//  5. Seizure classification — ILAE 2017-inspired string matching on presentation.
//     **Illustrative.** Real seizure classification requires EEG, neuroimaging,
//     and clinical correlation by a trained epileptologist.
//
// Returns a fully-populated neuroReport; the Diagnosis string is always
// non-empty so downstream validators can rely on it.
func interpretNeurology(req neuroConsultRequest) neuroReport {
	pres := strings.ToLower(req.Presentation)
	img := strings.ToLower(req.Imaging)
	recs := []string{}
	report := neuroReport{}

	// Resolve NIHSS score: use provided value, or estimate from presentation.
	nihss := estimateNIHSS(req)
	report.NIHSSScore = nihss

	// Determine whether this looks like a stroke presentation (FAST+ criteria).
	//
	// FAST+ expands the classic FAST mnemonic to include sudden severe headache
	// ("thunderclap"), which is highly specific for subarachnoid haemorrhage.
	// The AHA/ASA recommends BE-FAST (Balance, Eyes, Face, Arm, Speech, Time).
	// String-matching on a free-text presentation is a demo approximation only.
	// **Not a clinical guideline.**
	strokeSuspect := hasStrokeCriteria(pres)

	if strokeSuspect {
		report.Urgent = true
		recs = append(recs, "Activate stroke protocol: immediate non-contrast CT head")
		recs = append(recs, "Obtain stat labs: CBC, BMP, coagulation studies, point-of-care glucose")
		recs = append(recs, "Establish large-bore IV access; NPO until swallow screen")
		recs = append(recs, "Continuous cardiac monitoring; 12-lead ECG for AF detection")
		report.Diagnosis = "Acute stroke presentation suspected (FAST+ criteria). " +
			"Emergent neuroimaging and stroke team activation required. " +
			"(Illustrative assessment — not a clinical guideline.)"

		// LVO suspected when NIHSS ≥ 6 — correlates with proximal vessel occlusion.
		//
		// NIHSS ≥6 is a commonly cited threshold associated with LVO in the
		// literature (e.g. Heldner 2013, Kim 2019). The actual LVO prediction
		// tools (RACE, LAMS, FAST-ED, VAN) use validated score components.
		// **Illustrative threshold only.**
		if nihss >= 6 {
			report.LVOSuspected = true
			recs = append(recs, "LVO suspected (NIHSS ≥6): obtain CT angiography head and neck")
			recs = append(recs, "Alert interventional neuroradiology / thrombectomy team")
		}

		// tPA eligibility window: onset < 4.5 hours.
		//
		// IV alteplase is approved within 3h (NINDS) and extended to 4.5h for
		// selected patients (ECASS III). The 4.5h cutoff is the outer boundary
		// of current guidelines; many centres use advanced imaging (CT/MR
		// perfusion) to extend beyond this in "wake-up" stroke.
		// **Illustrative. Real eligibility requires full NINDS/ECASS III
		// inclusion/exclusion checklist.**
		if req.OnsetHours > 0 && req.OnsetHours < 4.5 {
			report.TPAEligible = true
			recs = append(recs, "Within tPA window (<4.5h): review full inclusion/exclusion criteria immediately")
			recs = append(recs, "If tPA eligible: target door-to-needle time <60 minutes")
		} else if req.OnsetHours == 0 {
			recs = append(recs, "Onset time unknown: consider advanced perfusion imaging to assess treatment eligibility")
		}

		// BP contraindication for tPA: systolic >185 mmHg.
		//
		// AHA/ASA guidelines list BP >185/110 mmHg as a contraindication to
		// IV tPA unless BP can be safely lowered. Labetalol or nicardipine
		// are first-line agents for BP reduction prior to tPA.
		// **Illustrative threshold only.**
		if req.BPSystolic != nil && *req.BPSystolic > 185 {
			if report.TPAEligible {
				report.TPAEligible = false
				recs = append(recs, "BP >185 mmHg: tPA relative contraindication — attempt BP reduction with IV labetalol or nicardipine before tPA")
			} else {
				recs = append(recs, "BP >185 mmHg: target BP <185/110 mmHg; consider IV labetalol or nicardipine")
			}
		}

		// Thrombectomy window: onset < 24 hours and LVO suspected.
		//
		// DAWN (2018) and DEFUSE-3 (2018) trials extended thrombectomy to 24h
		// in selected patients with perfusion-diffusion mismatch. Standard
		// selection uses ASPECTS ≥6 and clinical-imaging mismatch criteria.
		// **Illustrative. Real selection requires perfusion imaging and
		// interventional neuroradiology.**
		if report.LVOSuspected && (req.OnsetHours == 0 || req.OnsetHours < 24) {
			report.ThrombectomyWindow = true
			recs = append(recs, "Within thrombectomy window: urgent CTA/CTP to assess LVO and penumbra")
		}

		// Imaging: check for hyperdense vessel sign (CT marker of acute thrombus).
		if strings.Contains(img, "hyperdense") {
			recs = append(recs, "Hyperdense vessel sign on CT: consistent with acute large vessel thrombus — urgent CTA for confirmation")
		}

		// Stroke type classification.
		report.StrokeType = classifyStrokeType(pres)
		if report.StrokeType == "cardioembolic" {
			recs = append(recs, "Cardioembolic source suspected: echo, prolonged cardiac monitoring, anticoagulation planning post-acute phase")
		} else if report.StrokeType == "large vessel atherosclerosis" {
			recs = append(recs, "Large vessel atherosclerosis pattern: carotid/vertebral imaging if not already obtained; antiplatelet therapy per guidelines")
		} else if report.StrokeType == "small vessel" {
			recs = append(recs, "Lacunar/small vessel pattern: intensive risk factor management (BP, glucose, lipids)")
		}
	}

	// Seizure classification path — evaluated independently of stroke.
	//
	// A patient may present with both (e.g. stroke-related seizure), so
	// seizure classification is always attempted when seizure language is
	// present in the presentation string.
	//
	// Classification follows ILAE 2017 operational seizure type taxonomy:
	// focal onset, generalised onset, unknown onset.
	// **Illustrative. Real classification requires EEG and clinical evaluation.**
	seizureType := classifySeizure(pres)
	if seizureType != "" {
		report.SeizureType = seizureType
		if !report.Urgent {
			report.Urgent = true
			report.Diagnosis = buildSeizureDiagnosis(seizureType)
		} else {
			report.Diagnosis += " Concurrent seizure activity identified."
		}
		recs = append(recs, "Obtain urgent EEG; continuous EEG monitoring if status epilepticus suspected")
		recs = append(recs, "Secure airway, breathing, circulation; place in lateral recovery position if safe")
		recs = append(recs, "Stat glucose (hypoglycaemia is a common provoked seizure cause)")

		if isProvokedSeizure(pres) {
			recs = append(recs, "Likely provoked seizure: identify and treat underlying cause (metabolic, toxic, structural)")
			recs = append(recs, "First provoked seizure may not require long-term AED — neurology follow-up for risk stratification")
		} else {
			recs = append(recs, "Unprovoked seizure: epilepsy workup indicated — MRI brain, EEG, neurology follow-up")
			recs = append(recs, "Discuss AED initiation if risk of recurrence >60% (per ILAE definition of epilepsy)")
		}
	}

	// Fallback diagnosis when no rule fired.
	if report.Diagnosis == "" {
		report.Diagnosis = "No acute stroke or seizure pattern identified by rule engine for the data provided. " +
			"Clinical correlation required. Routine neurology evaluation as indicated."
		recs = append(recs, "Routine neurology follow-up as clinically indicated")
		recs = append(recs, "Consider headache diary, sleep study, or EEG if symptoms recur")
	}

	// Deduplicate recommendations while preserving insertion order.
	report.Recommendations = dedupStrings(recs)
	return report
}

// hasStrokeCriteria returns true when the presentation contains at least one
// FAST+ criterion:
//   - facial droop (F)
//   - arm weakness or leg weakness (A)
//   - speech difficulty / slurred speech / dysarthria / aphasia (S)
//   - sudden severe headache / thunderclap (T+)
//
// A single positive criterion is sufficient to trigger the stroke pathway
// per AHA/ASA "treat-until-proven-otherwise" recommendation for time-critical
// emergency assessment.
// **Not a clinical guideline.**
func hasStrokeCriteria(presLower string) bool {
	fastCriteria := []string{
		"facial droop",
		"face droop",
		"facial weakness",
		"arm weakness",
		"leg weakness",
		"limb weakness",
		"hemiparesis",
		"hemiplegia",
		"speech difficulty",
		"slurred speech",
		"dysarthria",
		"aphasia",
		"sudden headache",
		"thunderclap",
		"worst headache",
		"sudden vision",
		"vision loss",
		"diplopia",
		"ataxia",
		"vertigo",
		"stroke",
	}
	for _, criterion := range fastCriteria {
		if strings.Contains(presLower, criterion) {
			return true
		}
	}
	return false
}

// estimateNIHSS returns a NIHSS score. If the request provides one, it is
// used directly. Otherwise a keyword-based estimate is computed from the
// presentation string.
//
// Keyword scoring (illustrative — not the validated NIHSS instrument):
//   - Each major deficit domain (motor, speech, vision, sensation) adds
//     2–4 points depending on severity language.
//   - "mild" modifiers halve the domain contribution.
//   - "severe"/"complete" modifiers increase contribution.
//
// The NIHSS has 11 items each scored 0–4 (total 0–42). A real NIHSS
// requires bedside evaluation by a trained examiner (or validated
// video/telemedicine equivalent). **Illustrative estimate only.**
func estimateNIHSS(req neuroConsultRequest) int {
	if req.NIHSSScore != nil {
		return *req.NIHSSScore
	}

	pres := strings.ToLower(req.Presentation)
	score := 0

	// Motor (item 5+6): arm/leg weakness
	if strings.Contains(pres, "hemiplegia") || strings.Contains(pres, "complete paralysis") {
		score += 4
	} else if strings.Contains(pres, "hemiparesis") || strings.Contains(pres, "arm weakness") || strings.Contains(pres, "leg weakness") {
		if strings.Contains(pres, "severe") {
			score += 3
		} else if strings.Contains(pres, "mild") {
			score += 1
		} else {
			score += 2
		}
	}

	// Language/speech (items 9+10): aphasia, dysarthria
	if strings.Contains(pres, "aphasia") || strings.Contains(pres, "global aphasia") {
		score += 4
	} else if strings.Contains(pres, "dysarthria") || strings.Contains(pres, "slurred speech") || strings.Contains(pres, "speech difficulty") {
		score += 2
	}

	// Gaze (item 2): forced gaze deviation
	if strings.Contains(pres, "gaze deviation") || strings.Contains(pres, "forced gaze") {
		score += 2
	}

	// Visual (item 3): visual field defect
	if strings.Contains(pres, "hemianopia") || strings.Contains(pres, "visual field") {
		score += 2
	} else if strings.Contains(pres, "vision loss") || strings.Contains(pres, "sudden vision") {
		score += 1
	}

	// Facial palsy (item 4)
	if strings.Contains(pres, "facial droop") || strings.Contains(pres, "facial palsy") || strings.Contains(pres, "face droop") {
		score += 2
	}

	// Ataxia (item 7): limb ataxia
	if strings.Contains(pres, "ataxia") {
		score += 1
	}

	// Sensory (item 8)
	if strings.Contains(pres, "hemisensory") || strings.Contains(pres, "sensory loss") || strings.Contains(pres, "numbness") {
		score += 1
	}

	// Level of consciousness (item 1a): altered consciousness
	if strings.Contains(pres, "unconscious") || strings.Contains(pres, "unresponsive") || strings.Contains(pres, "coma") {
		score += 3
	} else if strings.Contains(pres, "stupor") || strings.Contains(pres, "obtunded") {
		score += 2
	} else if strings.Contains(pres, "drowsy") || strings.Contains(pres, "lethargic") {
		score += 1
	}

	if score > 42 {
		score = 42
	}
	return score
}

// classifyStrokeType returns one of: "cardioembolic", "large vessel atherosclerosis",
// "small vessel", or "" (undetermined / not enough information).
//
// Classification follows a simplified TOAST (Trial of Org 10172 in Acute
// Stroke Treatment) schema:
//   - Cardioembolic: AF/AFib, atrial fibrillation, valvular heart disease,
//     patent foramen ovale, intracardiac thrombus
//   - Large vessel: carotid stenosis, vertebral stenosis, intracranial stenosis
//   - Small vessel (lacunar): pure motor, pure sensory, sensorimotor,
//     ataxic hemiparesis, dysarthria-clumsy hand
//
// **Illustrative.** Real TOAST classification requires full workup including
// cardiac echo, prolonged cardiac monitoring, and vascular imaging.
func classifyStrokeType(presLower string) string {
	// Cardioembolic markers
	if strings.Contains(presLower, "atrial fibrillation") ||
		strings.Contains(presLower, "afib") ||
		strings.Contains(presLower, "af ") ||
		strings.Contains(presLower, "valvular") ||
		strings.Contains(presLower, "intracardiac thrombus") ||
		strings.Contains(presLower, "patent foramen ovale") ||
		strings.Contains(presLower, "pfo") {
		return "cardioembolic"
	}

	// Large vessel atherosclerosis markers
	if strings.Contains(presLower, "carotid stenosis") ||
		strings.Contains(presLower, "vertebral stenosis") ||
		strings.Contains(presLower, "intracranial stenosis") ||
		strings.Contains(presLower, "carotid occlusion") {
		return "large vessel atherosclerosis"
	}

	// Small vessel / lacunar syndromes
	lacunarPatterns := []string{
		"pure motor",
		"pure sensory",
		"sensorimotor",
		"ataxic hemiparesis",
		"dysarthria-clumsy hand",
		"dysarthria clumsy hand",
		"lacunar",
	}
	for _, pat := range lacunarPatterns {
		if strings.Contains(presLower, pat) {
			return "small vessel"
		}
	}

	return ""
}

// classifySeizure returns a seizure type string based on ILAE 2017 taxonomy,
// or "" if no seizure language is detected.
//
// Supported classifications:
//   - "generalized tonic-clonic": tonic-clonic, grand mal
//   - "focal onset": focal, partial, focal with impaired awareness
//   - "absence seizure": absence, staring spell, staring episode
//
// **Illustrative.** Real seizure classification requires clinical interview,
// eyewitness account, EEG (ictal and interictal), and neuroimaging per ILAE
// 2017 operational classification of seizure types.
func classifySeizure(presLower string) string {
	// Generalised tonic-clonic (formerly grand mal)
	if strings.Contains(presLower, "tonic-clonic") ||
		strings.Contains(presLower, "tonic clonic") ||
		strings.Contains(presLower, "grand mal") ||
		strings.Contains(presLower, "generalized seizure") ||
		strings.Contains(presLower, "generalised seizure") {
		return "generalized tonic-clonic"
	}

	// Absence seizures
	if strings.Contains(presLower, "absence") ||
		strings.Contains(presLower, "staring spell") ||
		strings.Contains(presLower, "staring episode") ||
		strings.Contains(presLower, "petit mal") {
		return "absence seizure"
	}

	// Focal onset (formerly partial)
	if strings.Contains(presLower, "focal seizure") ||
		strings.Contains(presLower, "partial seizure") ||
		strings.Contains(presLower, "focal onset") ||
		strings.Contains(presLower, "focal with impaired") ||
		strings.Contains(presLower, "complex partial") ||
		strings.Contains(presLower, "simple partial") {
		return "focal onset"
	}

	// Generic seizure language without specific subtype
	if strings.Contains(presLower, "seizure") || strings.Contains(presLower, "convulsion") {
		return "undetermined onset"
	}

	return ""
}

// isProvokedSeizure returns true when the presentation contains language
// suggesting a provoked (acute symptomatic) seizure — i.e. one occurring
// in close temporal relationship to an acute brain insult or systemic
// disturbance (metabolic, toxic, infectious, structural).
//
// Per ILAE 2014 definition, a provoked seizure is not considered evidence
// of epilepsy and has different management implications.
// **Illustrative keyword matching only.**
func isProvokedSeizure(presLower string) bool {
	provokedTerms := []string{
		"provoked",
		"hypoglycemia", "hypoglycaemia",
		"hyperglycemia", "hyperglycaemia",
		"hyponatremia", "hyponatraemia",
		"metabolic",
		"alcohol withdrawal",
		"drug withdrawal",
		"toxic",
		"fever", "febrile",
		"meningitis", "encephalitis",
		"eclampsia",
		"uremia", "uraemia",
	}
	for _, term := range provokedTerms {
		if strings.Contains(presLower, term) {
			return true
		}
	}
	return false
}

// buildSeizureDiagnosis returns a narrative diagnosis string for a seizure
// presentation where no stroke criteria have already set the diagnosis.
func buildSeizureDiagnosis(seizureType string) string {
	switch seizureType {
	case "generalized tonic-clonic":
		return "Generalised tonic-clonic seizure. " +
			"Emergent evaluation for provoking causes and epilepsy workup required. " +
			"(Illustrative assessment — not a clinical guideline.)"
	case "absence seizure":
		return "Absence (petit mal) seizure pattern. " +
			"Neurology referral for EEG and epilepsy evaluation. " +
			"(Illustrative assessment — not a clinical guideline.)"
	case "focal onset":
		return "Focal onset seizure. " +
			"MRI brain and EEG required to identify structural aetiology. " +
			"(Illustrative assessment — not a clinical guideline.)"
	default:
		return "Seizure of undetermined onset type. " +
			"Comprehensive epilepsy evaluation including EEG and neuroimaging required. " +
			"(Illustrative assessment — not a clinical guideline.)"
	}
}
