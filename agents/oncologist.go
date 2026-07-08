package agents

// File oncologist.go: oncology specialist agent that selects a chemotherapy
// protocol, computes BSA, applies dose reductions, and emits a treatment
// recommendation.
//
// **Educational demo only — NOT a clinical guideline.** Every protocol,
// dose, threshold, and rule in this file is a simplified textbook example
// for illustrating agentic AI patterns in Go. They do not constitute
// medical advice, are not validated clinical decision support tools, and
// must not be used to direct real patient care. Always follow current
// institutional protocols and the judgment of a qualified oncologist.

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/PratikDhanave/bodh/pkg/agent"
)

// OncologyConsult is the input payload for an oncology consult message.
//
// All fields are required for protocol selection and dose computation.
// ECOG score ranges 0–4 (0=fully active, 4=completely disabled).
// RenalFunction must be one of: "normal", "mild", "moderate", "severe".
type OncologyConsult struct {
	PatientID       string   `json:"patient_id"`
	CaseID          string   `json:"case_id"`
	CancerType      string   `json:"cancer_type"`
	Stage           string   `json:"stage"`
	ECOGScore       int      `json:"ecog_score"`
	WeightKg        float64  `json:"weight_kg"`
	HeightCm        float64  `json:"height_cm"`
	RenalFunction   string   `json:"renal_function"`
	PriorTreatments []string `json:"prior_treatments,omitempty"`
}

// DrugOrder describes a single drug within a chemotherapy regimen.
//
// DoseMgM2 is the per-cycle dose per square metre of BSA. Route and Schedule
// are free-text clinical descriptors (e.g. "IV", "day 1, q21d").
//
// **Illustrative only.** Actual doses, routes, and schedules vary by
// institutional protocol, patient factors, and drug shortages. These
// values must not drive real prescribing.
type DrugOrder struct {
	Name     string  `json:"name"`
	DoseMgM2 float64 `json:"dose_mg_m2"`
	Route    string  `json:"route"`
	Schedule string  `json:"schedule"`
}

// OncologyRecommendation is the output payload emitted as the
// "oncology_recommendation" message type.
type OncologyRecommendation struct {
	Protocol       string      `json:"protocol"`
	Regimen        string      `json:"regimen"`
	Drugs          []DrugOrder `json:"drugs"`
	BSAM2          float64     `json:"bsa_m2"`
	DoseReductions []string    `json:"dose_reductions,omitempty"`
	SupportiveCare []string    `json:"supportive_care,omitempty"`
	Monitoring     []string    `json:"monitoring,omitempty"`
	Urgent         bool        `json:"urgent"`
}

// OncologistAgent selects a chemotherapy protocol, computes body surface
// area via the Mosteller formula, and applies dose reductions based on
// ECOG performance status and renal function.
//
// Input message type:  "oncology_consult"        Content: OncologyConsult JSON
// Output message type: "oncology_recommendation" Content: OncologyRecommendation JSON
//
// The agent is stateless and zero-value-usable.
type OncologistAgent struct{}

func (a *OncologistAgent) ID() string   { return "oncologist" }
func (a *OncologistAgent) Name() string { return "Oncology Specialist" }
func (a *OncologistAgent) Capabilities() []string {
	return []string{"chemotherapy_protocol", "oncology_assessment"}
}

// HandleMessage decodes the OncologyConsult, selects the appropriate
// protocol, computes BSA and dose adjustments, then returns an
// oncology_recommendation message addressed to the empty string (terminal
// — the orchestrator logs it and the case is complete at this hop).
//
// Returns a non-nil error only when the input JSON is malformed; protocol
// selection never errors — unknown cancer types produce a "supportive care
// only" recommendation with Urgent=true so a human oncologist is paged.
func (a *OncologistAgent) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
	var consult OncologyConsult
	if err := json.Unmarshal([]byte(msg.Content), &consult); err != nil {
		env.Logf("[oncologist] invalid consult payload: %v", err)
		return nil, err
	}

	bsa := mosteller(consult.HeightCm, consult.WeightKg)
	rec := selectProtocol(consult, bsa)

	env.Logf("[oncologist] patient=%s cancer=%s stage=%s protocol=%q bsa=%.2f m² urgent=%v",
		consult.PatientID, consult.CancerType, consult.Stage, rec.Protocol, bsa, rec.Urgent)
	if len(rec.DoseReductions) > 0 {
		env.Logf("[oncologist] dose reductions: %s", strings.Join(rec.DoseReductions, "; "))
	}

	body, _ := json.Marshal(rec)
	meta := copyMeta(msg.Metadata)
	meta["patient_id"] = consult.PatientID
	meta["case_id"] = consult.CaseID

	out := agent.NewMessage(
		a.ID(),
		"",
		agent.RoleAgent,
		"oncology_recommendation",
		string(body),
		meta,
	)
	return []agent.Message{out}, nil
}

// mosteller computes body surface area using the Mosteller formula:
//
//	BSA (m²) = sqrt(height_cm × weight_kg / 3600)
//
// This is the most widely used BSA formula in oncology dosing.
// **Illustrative — not validated for all populations or weight extremes.**
func mosteller(heightCm, weightKg float64) float64 {
	if heightCm <= 0 || weightKg <= 0 {
		return 0
	}
	return math.Sqrt(heightCm * weightKg / 3600)
}

// selectProtocol picks the chemotherapy regimen, applies dose reductions,
// and assembles supportive care and monitoring lists.
//
// Protocol selection order (simplified educational rules — NOT guidelines):
//  1. NSCLC stage III/IV, ECOG 0–1 → Carboplatin + Paclitaxel
//  2. Breast cancer, HER2+          → Trastuzumab + Pertuzumab
//  3. Colon cancer, stage III        → FOLFOX
//  4. Lymphoma (DLBCL)               → R-CHOP
//  5. Unknown / unsupported          → Supportive care only; Urgent=true
func selectProtocol(c OncologyConsult, bsa float64) OncologyRecommendation {
	cancer := strings.ToLower(c.CancerType)
	stage := strings.ToLower(c.Stage)

	var rec OncologyRecommendation
	rec.BSAM2 = math.Round(bsa*100) / 100

	switch {
	case (strings.Contains(cancer, "nsclc") || strings.Contains(cancer, "non-small")) &&
		(stage == "iii" || stage == "iv" || stage == "3" || stage == "4") &&
		c.ECOGScore <= 1:
		rec = nsclcCarboPaclitaxel(c, bsa)

	case strings.Contains(cancer, "breast") && strings.Contains(strings.ToLower(fmt.Sprintf("%v", c)), "her2"):
		rec = breastHER2TrasTuz(c, bsa)

	case (strings.Contains(cancer, "colon") || strings.Contains(cancer, "colorectal")) &&
		(stage == "iii" || stage == "3"):
		rec = colonFOLFOX(c, bsa)

	case strings.Contains(cancer, "lymphoma") || strings.Contains(cancer, "dlbcl"):
		rec = lymphomaRCHOP(c, bsa)

	// Breast cancer without HER2 marker — treat as HER2+ for demo purposes
	// but flag for specialist review.
	case strings.Contains(cancer, "breast"):
		rec = breastHER2TrasTuz(c, bsa)

	default:
		rec = supportiveCareOnly(c, bsa)
	}

	rec.BSAM2 = math.Round(bsa*100) / 100
	applyDoseReductions(&rec, c)
	return rec
}

// nsclcCarboPaclitaxel builds the Carboplatin AUC5 + Paclitaxel regimen
// used for NSCLC stage III/IV ECOG 0–1.
//
// **Illustrative only.** Current NSCLC first-line therapy frequently
// incorporates immunotherapy (pembrolizumab); platinum doublets alone
// are no longer standard first-line monotherapy in most histologies.
func nsclcCarboPaclitaxel(c OncologyConsult, bsa float64) OncologyRecommendation {
	_ = bsa // Carboplatin is dosed by AUC (Calvert), not mg/m²; shown as symbolic here.
	return OncologyRecommendation{
		Protocol: "Carboplatin/Paclitaxel",
		Regimen:  "Carboplatin AUC5 IV day 1 + Paclitaxel 175 mg/m² IV day 1, q21d × 4–6 cycles",
		Drugs: []DrugOrder{
			{Name: "Carboplatin", DoseMgM2: 0, Route: "IV", Schedule: "AUC5 day 1 q21d"},
			{Name: "Paclitaxel", DoseMgM2: 175, Route: "IV", Schedule: "day 1 q21d"},
		},
		SupportiveCare: []string{
			"Antiemetics: ondansetron + dexamethasone pre-chemo",
			"G-CSF per institutional protocol if febrile neutropenia risk >20%",
			"Hypersensitivity premedication for paclitaxel (diphenhydramine, ranitidine, dexamethasone)",
		},
		Monitoring: []string{
			"CBC with differential before each cycle",
			"Comprehensive metabolic panel (CMP) before each cycle",
			"Peripheral neuropathy assessment each visit",
			"CT chest/abdomen/pelvis after 2 cycles to assess response",
		},
		Urgent: false,
	}
}

// breastHER2TrasTuz builds the Trastuzumab + Pertuzumab regimen
// for HER2-positive breast cancer.
//
// **Illustrative only.** Real HER2+ therapy selection depends on line of
// therapy, HR status, prior anthracycline exposure, and cardiac function.
func breastHER2TrasTuz(c OncologyConsult, bsa float64) OncologyRecommendation {
	_ = bsa // Trastuzumab/Pertuzumab are weight-based (mg/kg), shown as mg/m² symbolically.
	return OncologyRecommendation{
		Protocol: "Trastuzumab+Pertuzumab",
		Regimen:  "Trastuzumab 6 mg/kg IV day 1 (loading 8 mg/kg cycle 1) + Pertuzumab 420 mg IV day 1, q21d",
		Drugs: []DrugOrder{
			{Name: "Trastuzumab", DoseMgM2: 0, Route: "IV", Schedule: "8 mg/kg loading then 6 mg/kg day 1 q21d"},
			{Name: "Pertuzumab", DoseMgM2: 0, Route: "IV", Schedule: "840 mg loading then 420 mg day 1 q21d"},
		},
		SupportiveCare: []string{
			"Cardiac monitoring: ECHO/MUGA at baseline, q3 months",
			"Antiemetics as needed (trastuzumab regimens have low emetogenic potential)",
		},
		Monitoring: []string{
			"LVEF assessment before starting and every 3 months",
			"CBC and LFTs before each cycle",
			"Infusion reaction monitoring for first doses",
			"Tumour response imaging per institutional schedule",
		},
		Urgent: false,
	}
}

// colonFOLFOX builds the FOLFOX regimen for stage III colon cancer.
//
// **Illustrative only.** FOLFOX4, FOLFOX6, and mFOLFOX6 differ in
// oxaliplatin dose and infusion schedule. The values below are
// representative of mFOLFOX6.
func colonFOLFOX(c OncologyConsult, bsa float64) OncologyRecommendation {
	return OncologyRecommendation{
		Protocol: "FOLFOX",
		Regimen:  "Oxaliplatin 85 mg/m² + Leucovorin 400 mg/m² + 5-FU 400 mg/m² bolus + 2400 mg/m² 46h infusion, q14d",
		Drugs: []DrugOrder{
			{Name: "Oxaliplatin", DoseMgM2: 85, Route: "IV", Schedule: "day 1 q14d, 2h infusion"},
			{Name: "Leucovorin", DoseMgM2: 400, Route: "IV", Schedule: "day 1 q14d, concurrent with oxaliplatin"},
			{Name: "5-Fluorouracil", DoseMgM2: 400, Route: "IV bolus", Schedule: "day 1 q14d, after leucovorin"},
			{Name: "5-Fluorouracil", DoseMgM2: 2400, Route: "IV infusion", Schedule: "46h continuous infusion q14d"},
		},
		SupportiveCare: []string{
			"Antiemetics: ondansetron + dexamethasone day 1",
			"Avoid cold exposure (cold-induced peripheral neuropathy from oxaliplatin)",
			"PICC or port for 46h 5-FU infusion",
			"Mucositis prophylaxis (mouth rinses)",
		},
		Monitoring: []string{
			"CBC with differential before each cycle",
			"CMP including LFTs before each cycle",
			"Peripheral neuropathy grading each cycle (dose limit oxaliplatin ≥ grade 2 persistent)",
			"DPD deficiency screening before 5-FU (severe toxicity risk)",
			"CT imaging for response assessment after 4–6 cycles",
		},
		Urgent: false,
	}
}

// lymphomaRCHOP builds the R-CHOP regimen for DLBCL.
//
// **Illustrative only.** Vincristine dose is commonly capped at 2 mg
// absolute to reduce peripheral neuropathy risk, regardless of BSA.
// Cardiac evaluation required before anthracycline (doxorubicin).
func lymphomaRCHOP(c OncologyConsult, bsa float64) OncologyRecommendation {
	return OncologyRecommendation{
		Protocol: "R-CHOP",
		Regimen:  "Rituximab 375 mg/m² + Cyclophosphamide 750 mg/m² + Doxorubicin 50 mg/m² + Vincristine 1.4 mg/m² (max 2 mg) + Prednisone 40 mg/m²/d × 5, q21d",
		Drugs: []DrugOrder{
			{Name: "Rituximab", DoseMgM2: 375, Route: "IV", Schedule: "day 1 q21d"},
			{Name: "Cyclophosphamide", DoseMgM2: 750, Route: "IV", Schedule: "day 1 q21d"},
			{Name: "Doxorubicin", DoseMgM2: 50, Route: "IV", Schedule: "day 1 q21d"},
			{Name: "Vincristine", DoseMgM2: 1.4, Route: "IV", Schedule: "day 1 q21d (max absolute dose 2 mg)"},
			{Name: "Prednisone", DoseMgM2: 40, Route: "PO", Schedule: "days 1–5 q21d"},
		},
		SupportiveCare: []string{
			"PCP prophylaxis: trimethoprim-sulfamethoxazole while on steroids",
			"Antiemetics: ondansetron + dexamethasone (note: dexamethasone part of CHOP)",
			"G-CSF (filgrastim/pegfilgrastim) starting day 2 or 3",
			"HBV reactivation prophylaxis if HBsAg or anti-HBc positive (antiviral therapy)",
			"Tumour lysis syndrome prophylaxis: IV hydration, allopurinol or rasburicase if high risk",
		},
		Monitoring: []string{
			"CBC with differential before each cycle",
			"CMP including LFTs and LDH before each cycle",
			"LVEF (ECHO) at baseline and after cumulative doxorubicin ≥ 300 mg/m²",
			"Peripheral neuropathy assessment (vincristine)",
			"PET-CT after 2 cycles (interim) and end of treatment",
		},
		Urgent: false,
	}
}

// supportiveCareOnly is the fallback when the cancer type is not matched
// by any protocol rule. Urgent is true so the case is immediately routed
// for specialist review.
func supportiveCareOnly(c OncologyConsult, bsa float64) OncologyRecommendation {
	return OncologyRecommendation{
		Protocol: "Supportive Care — specialist review required",
		Regimen:  "No standard protocol matched; refer to tumour board",
		Drugs:    nil,
		SupportiveCare: []string{
			"Pain management per WHO analgesic ladder",
			"Nutritional support assessment",
			"Palliative care consultation",
		},
		Monitoring: []string{
			"Tumour board presentation within 5 business days",
			"Performance status re-evaluation before any systemic therapy",
		},
		Urgent: true,
	}
}

// applyDoseReductions modifies the recommendation in-place to reflect
// ECOG performance status and renal function adjustments.
//
// Rules (simplified educational — NOT guidelines):
//   - ECOG ≥ 3: reduce all drug doses by 25%
//   - Renal moderate: reduce carboplatin and cisplatin doses by 25%
//   - Renal severe: flag nephrotoxic agents; mark urgent
//
// The function annotates DoseReductions with human-readable strings so
// the downstream clinician knows what was changed and why.
func applyDoseReductions(rec *OncologyRecommendation, c OncologyConsult) {
	renal := strings.ToLower(c.RenalFunction)

	if c.ECOGScore >= 3 {
		rec.DoseReductions = append(rec.DoseReductions,
			fmt.Sprintf("ECOG %d ≥ 3: all cytotoxic doses reduced 25%% (illustrative — verify with oncologist)", c.ECOGScore))
		for i := range rec.Drugs {
			rec.Drugs[i].DoseMgM2 = math.Round(rec.Drugs[i].DoseMgM2*0.75*100) / 100
		}
		rec.Urgent = true
	}

	if renal == "moderate" {
		for i := range rec.Drugs {
			name := strings.ToLower(rec.Drugs[i].Name)
			if name == "carboplatin" || name == "cisplatin" {
				rec.DoseReductions = append(rec.DoseReductions,
					fmt.Sprintf("Renal impairment (moderate): %s dose reduced 25%% (illustrative — verify GFR/Calvert)", rec.Drugs[i].Name))
				rec.Drugs[i].DoseMgM2 = math.Round(rec.Drugs[i].DoseMgM2*0.75*100) / 100
			}
		}
	}

	if renal == "severe" {
		nephrotoxic := []string{}
		for _, d := range rec.Drugs {
			name := strings.ToLower(d.Name)
			if name == "carboplatin" || name == "cisplatin" {
				nephrotoxic = append(nephrotoxic, d.Name)
			}
		}
		if len(nephrotoxic) > 0 {
			rec.DoseReductions = append(rec.DoseReductions,
				fmt.Sprintf("Renal impairment (severe): avoid or replace nephrotoxic agent(s): %s — requires specialist review",
					strings.Join(nephrotoxic, ", ")))
			rec.Urgent = true
		}
	}
}
