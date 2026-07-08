package agents

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/agent"
)

// InfectiousDiseaseAgent provides antibiotic stewardship recommendations using
// a rule-based engine modelled on a simplified Sanford Guide decision tree.
//
// Input message Type:  id_consult
//
//	Content: {"patient_id","case_id","suspected_organism","site","severity","allergies","culture_pending"}
//
// Output message Type: id_recommendation
//
//	Content: {"antibiotic","dose","route","duration_days","de_escalation_criteria","isolation_precautions","rationale"}
//
// The agent selects a first-line agent based on the infection site (cap, uti,
// cellulitis, sepsis, mrsa) and severity (mild, moderate, severe), then
// adjusts for documented penicillin allergy. When culture results are
// pending the recommendation always includes a de-escalation reminder.
type InfectiousDiseaseAgent struct{}

// idConsult is the wire format of an id_consult message Content.
type idConsult struct {
	PatientID         string   `json:"patient_id"`
	CaseID            string   `json:"case_id,omitempty"`
	SuspectedOrganism string   `json:"suspected_organism,omitempty"`
	Site              string   `json:"site"`
	Severity          string   `json:"severity"`
	Allergies         []string `json:"allergies,omitempty"`
	CulturePending    bool     `json:"culture_pending"`
}

// idRecommendation is the wire format of an id_recommendation message Content.
type idRecommendation struct {
	Antibiotic           string `json:"antibiotic"`
	Dose                 string `json:"dose"`
	Route                string `json:"route"`
	DurationDays         int    `json:"duration_days"`
	DeEscalationCriteria string `json:"de_escalation_criteria"`
	IsolationPrecautions string `json:"isolation_precautions"`
	Rationale            string `json:"rationale"`
}

func (a *InfectiousDiseaseAgent) ID() string   { return "infectious_disease" }
func (a *InfectiousDiseaseAgent) Name() string { return "Infectious Disease Specialist" }
func (a *InfectiousDiseaseAgent) Capabilities() []string {
	return []string{"antibiotic_stewardship", "infection_management"}
}

// HandleMessage decodes an id_consult, selects an antibiotic regimen, and
// returns a single id_recommendation addressed back to the sender.
func (a *InfectiousDiseaseAgent) HandleMessage(_ context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	var req idConsult
	if err := json.Unmarshal([]byte(msg.Content), &req); err != nil {
		env.Logf("[infectious_disease] invalid id_consult: %v", err)
		return nil, err
	}

	rec := selectRegimen(req)

	env.Logf("[infectious_disease] patient=%s site=%s severity=%s → %s %s %s x%dd",
		req.PatientID, req.Site, req.Severity, rec.Antibiotic, rec.Dose, rec.Route, rec.DurationDays)

	recJSON, err := json.Marshal(rec)
	if err != nil {
		return nil, err
	}

	meta := copyMeta(msg.Metadata)
	meta["patient_id"] = req.PatientID
	if req.CaseID != "" {
		meta["case_id"] = req.CaseID
	}

	out := agent.NewMessage(a.ID(), msg.From, agent.RoleAgent, "id_recommendation", string(recJSON), meta)
	return []agent.Message{out}, nil
}

// hasPenicillinAllergy returns true when the patient's allergy list contains
// any penicillin-class drug name (case-insensitive substring match).
func hasPenicillinAllergy(allergies []string) bool {
	penWords := []string{"penicillin", "amoxicillin", "ampicillin", "piperacillin", "nafcillin"}
	for _, a := range allergies {
		al := strings.ToLower(a)
		for _, w := range penWords {
			if strings.Contains(al, w) {
				return true
			}
		}
	}
	return false
}

// selectRegimen applies the simplified Sanford Guide-style decision tree and
// returns the recommended regimen. All strings are lowercased before
// comparison so callers are not required to canonicalise the input.
func selectRegimen(req idConsult) idRecommendation {
	site := strings.ToLower(strings.TrimSpace(req.Site))
	severity := strings.ToLower(strings.TrimSpace(req.Severity))
	penAllergy := hasPenicillinAllergy(req.Allergies)

	var rec idRecommendation

	switch {
	// ── Community-acquired pneumonia ──────────────────────────────────────
	case strings.Contains(site, "cap") || strings.Contains(site, "pneumonia"):
		rec = selectCAP(severity, penAllergy)

	// ── Urinary tract infection ───────────────────────────────────────────
	case strings.Contains(site, "uti") || strings.Contains(site, "urinary"):
		rec = selectUTI(severity)

	// ── Cellulitis / skin-and-soft-tissue infection ───────────────────────
	case strings.Contains(site, "cellulitis") || strings.Contains(site, "ssti") || strings.Contains(site, "skin"):
		rec = selectCellulitis(severity, penAllergy)

	// ── Sepsis ────────────────────────────────────────────────────────────
	case strings.Contains(site, "sepsis") || strings.Contains(site, "bacteremia"):
		rec = selectSepsis(penAllergy)

	// ── MRSA ──────────────────────────────────────────────────────────────
	case strings.Contains(site, "mrsa") || strings.Contains(req.SuspectedOrganism, "mrsa"):
		rec = selectMRSA()

	default:
		// Unknown site — fallback to broad-spectrum empiric coverage.
		rec = idRecommendation{
			Antibiotic:           "vancomycin + piperacillin-tazobactam",
			Dose:                 "vancomycin 15-20 mg/kg IV q8-12h + pip-tazo 4.5 g IV q6h",
			Route:                "IV",
			DurationDays:         7,
			DeEscalationCriteria: "narrow based on culture and sensitivity results",
			IsolationPrecautions: "contact precautions until MRSA ruled out",
			Rationale:            "Empiric broad-spectrum coverage for unspecified site; de-escalate per cultures",
		}
	}

	// Universal de-escalation reminder when cultures are still pending.
	if req.CulturePending && !strings.Contains(rec.DeEscalationCriteria, "culture") {
		rec.DeEscalationCriteria = "de-escalate to narrow-spectrum agent once culture and sensitivity results available; " + rec.DeEscalationCriteria
	}

	return rec
}

func selectCAP(severity string, penAllergy bool) idRecommendation {
	switch severity {
	case "mild":
		if penAllergy {
			return idRecommendation{
				Antibiotic:           "doxycycline",
				Dose:                 "100 mg",
				Route:                "PO",
				DurationDays:         5,
				DeEscalationCriteria: "reassess at 48–72h; step down if afebrile and improving",
				IsolationPrecautions: "standard precautions",
				Rationale:            "Doxycycline selected for mild CAP due to macrolide/penicillin allergy",
			}
		}
		return idRecommendation{
			Antibiotic:           "azithromycin",
			Dose:                 "500 mg day 1, then 250 mg days 2–5",
			Route:                "PO",
			DurationDays:         5,
			DeEscalationCriteria: "reassess at 48–72h; step down if afebrile and improving",
			IsolationPrecautions: "standard precautions",
			Rationale:            "Azithromycin for mild CAP without comorbidities",
		}
	case "moderate":
		if penAllergy {
			return idRecommendation{
				Antibiotic:           "levofloxacin",
				Dose:                 "750 mg",
				Route:                "PO",
				DurationDays:         5,
				DeEscalationCriteria: "reassess at 72h; step down if clinical improvement",
				IsolationPrecautions: "standard precautions",
				Rationale:            "Respiratory fluoroquinolone selected for moderate CAP with penicillin allergy",
			}
		}
		return idRecommendation{
			Antibiotic:           "amoxicillin-clavulanate + azithromycin",
			Dose:                 "875/125 mg BID + azithromycin 500 mg daily",
			Route:                "PO",
			DurationDays:         7,
			DeEscalationCriteria: "reassess at 72h; consider step-down to amoxicillin alone if atypical coverage not needed",
			IsolationPrecautions: "standard precautions",
			Rationale:            "Beta-lactam + macrolide combination for moderate CAP",
		}
	default: // severe
		if penAllergy {
			return idRecommendation{
				Antibiotic:           "aztreonam + azithromycin",
				Dose:                 "aztreonam 2 g IV q8h + azithromycin 500 mg IV daily",
				Route:                "IV",
				DurationDays:         7,
				DeEscalationCriteria: "de-escalate based on culture results; consider PO step-down at 72h if improving",
				IsolationPrecautions: "standard precautions; airborne if TB not excluded",
				Rationale:            "Aztreonam (no penicillin cross-reactivity) + macrolide for severe CAP with penicillin allergy",
			}
		}
		return idRecommendation{
			Antibiotic:           "piperacillin-tazobactam + azithromycin",
			Dose:                 "4.5 g IV q6h + azithromycin 500 mg IV daily",
			Route:                "IV",
			DurationDays:         7,
			DeEscalationCriteria: "de-escalate based on culture results; step down to PO at 72h if tolerating oral and clinically improving",
			IsolationPrecautions: "standard precautions; airborne if TB not excluded",
			Rationale:            "Broad-spectrum IV beta-lactam + macrolide for severe CAP",
		}
	}
}

func selectUTI(severity string) idRecommendation {
	switch severity {
	case "mild", "uncomplicated":
		return idRecommendation{
			Antibiotic:           "nitrofurantoin",
			Dose:                 "100 mg (modified release)",
			Route:                "PO",
			DurationDays:         5,
			DeEscalationCriteria: "reassess at 48h; if no improvement check urine culture and sensitivities",
			IsolationPrecautions: "standard precautions",
			Rationale:            "Nitrofurantoin first-line for uncomplicated UTI; minimal collateral damage to gut flora",
		}
	default: // complicated / moderate / severe
		return idRecommendation{
			Antibiotic:           "ciprofloxacin",
			Dose:                 "500 mg BID",
			Route:                "PO",
			DurationDays:         7,
			DeEscalationCriteria: "narrow based on urine culture and local antibiogram; consider TMP-SMX if susceptible",
			IsolationPrecautions: "standard precautions",
			Rationale:            "Fluoroquinolone for complicated UTI; verify local resistance rates before prescribing",
		}
	}
}

func selectCellulitis(severity string, penAllergy bool) idRecommendation {
	switch severity {
	case "mild":
		if penAllergy {
			return idRecommendation{
				Antibiotic:           "clindamycin",
				Dose:                 "300–450 mg TID",
				Route:                "PO",
				DurationDays:         5,
				DeEscalationCriteria: "reassess at 48h; extend if spreading erythema",
				IsolationPrecautions: "standard precautions",
				Rationale:            "Clindamycin for mild cellulitis with penicillin allergy; covers streptococcal and some MSSA",
			}
		}
		return idRecommendation{
			Antibiotic:           "cephalexin",
			Dose:                 "500 mg QID",
			Route:                "PO",
			DurationDays:         5,
			DeEscalationCriteria: "reassess at 48h; extend if spreading erythema",
			IsolationPrecautions: "standard precautions",
			Rationale:            "Cephalexin for mild cellulitis; covers Streptococcus and MSSA",
		}
	default: // moderate/severe
		if penAllergy {
			return idRecommendation{
				Antibiotic:           "vancomycin",
				Dose:                 "15–20 mg/kg IV q8–12h",
				Route:                "IV",
				DurationDays:         7,
				DeEscalationCriteria: "de-escalate based on wound culture; step down to PO clindamycin or TMP-SMX when improving",
				IsolationPrecautions: "contact precautions if purulent",
				Rationale:            "IV vancomycin for moderate/severe cellulitis with penicillin allergy; MRSA coverage",
			}
		}
		return idRecommendation{
			Antibiotic:           "cefazolin",
			Dose:                 "1–2 g IV q8h",
			Route:                "IV",
			DurationDays:         7,
			DeEscalationCriteria: "step down to cephalexin PO when tolerating oral and improving; de-escalate per cultures",
			IsolationPrecautions: "contact precautions if purulent",
			Rationale:            "IV cefazolin for moderate/severe cellulitis; step down to PO once clinically improving",
		}
	}
}

func selectSepsis(penAllergy bool) idRecommendation {
	if penAllergy {
		return idRecommendation{
			Antibiotic:           "vancomycin + aztreonam",
			Dose:                 "vancomycin 15–20 mg/kg IV q8–12h + aztreonam 2 g IV q8h",
			Route:                "IV",
			DurationDays:         7,
			DeEscalationCriteria: "narrow based on blood culture results within 48–72h; reassess daily for de-escalation opportunity",
			IsolationPrecautions: "contact precautions until MRSA ruled out",
			Rationale:            "Vancomycin (MRSA) + aztreonam (gram-negative; no penicillin cross-reactivity) for sepsis with penicillin allergy",
		}
	}
	return idRecommendation{
		Antibiotic:           "vancomycin + piperacillin-tazobactam",
		Dose:                 "vancomycin 15–20 mg/kg IV q8–12h + piperacillin-tazobactam 4.5 g IV q6h",
		Route:                "IV",
		DurationDays:         7,
		DeEscalationCriteria: "narrow based on blood culture results within 48–72h; reassess daily for de-escalation opportunity",
		IsolationPrecautions: "contact precautions until MRSA ruled out",
		Rationale:            "Broad-spectrum empiric coverage: vancomycin for MRSA + pip-tazo for gram-negative/anaerobic organisms",
	}
}

func selectMRSA() idRecommendation {
	return idRecommendation{
		Antibiotic:           "vancomycin",
		Dose:                 "15–20 mg/kg IV q8–12h (target AUC/MIC 400–600)",
		Route:                "IV",
		DurationDays:         14,
		DeEscalationCriteria: "confirm MRSA on culture; consider PO linezolid or TMP-SMX step-down for uncomplicated SSTI when improving",
		IsolationPrecautions: "contact precautions; private room if available",
		Rationale:            "Vancomycin is first-line for MRSA; monitor renal function and trough/AUC levels",
	}
}
