package medical

import "encoding/json"

// CaseState tracks sequential diagnosis progress (SD Bench / MAI-DxO style).
type CaseState struct {
	CaseID           string            `json:"case_id"`
	Presentation     string            `json:"presentation"`
	QuestionsAsked   []string          `json:"questions_asked,omitempty"`
	Answers          map[string]string `json:"answers,omitempty"`
	TestsOrdered     []DiagnosticTest  `json:"tests_ordered,omitempty"`
	TestResults      map[string]string `json:"test_results,omitempty"`
	Hypotheses       []string          `json:"hypotheses,omitempty"`
	WorkingDiagnosis string            `json:"working_diagnosis,omitempty"`
	TotalCostUSD     float64           `json:"total_cost_usd"`
	MaxCostUSD       float64           `json:"max_cost_usd"`
	GoldDiagnosis    string            `json:"gold_diagnosis,omitempty"` // for eval/benchmark only
	Condition        string            `json:"condition,omitempty"`      // e.g. chf, cap, dm
	RPMObservations  []RPMObservation  `json:"rpm_observations,omitempty"`
	RPMAlerts        []string          `json:"rpm_alerts,omitempty"`
}

// DiagnosticTest represents a virtual investigation with cost (SD Bench methodology).
type DiagnosticTest struct {
	Name    string  `json:"name"`
	CostUSD float64 `json:"cost_usd"`
	Result  string  `json:"result,omitempty"`
	Ordered bool    `json:"ordered"`
}

// Common test catalog with illustrative USD costs (research demo values).
// All cost values are illustrative only and are NOT clinical guidelines or
// reflective of real-world billing.
var TestCatalog = map[string]float64{
	"complete_blood_count":  45,
	"basic_metabolic_panel": 55,
	"chest_xray":            120,
	"ecg":                   80,
	"urinalysis":            35,
	"ct_chest":              850,
	"blood_culture":         95,
	"arterial_blood_gas":    75,
	// Specialty additions for SD-Bench cardiac/sepsis/endocrine cases.
	// Illustrative values only — not clinical guidelines.
	"troponin_i":    55, // ACS marker
	"bnp":           95, // CHF marker
	"lactate":       35, // sepsis severity marker
	"urine_ketones": 15, // DKA marker
	"procalcitonin": 75, // bacterial sepsis marker (optional)
}

// PatientPresentation is the initial case intake payload.
type PatientPresentation struct {
	ChiefComplaint  string           `json:"chief_complaint"`
	HPI             string           `json:"hpi"`
	Demographics    string           `json:"demographics,omitempty"`
	Vitals          string           `json:"vitals,omitempty"`
	Condition       string           `json:"condition,omitempty"`
	RPMObservations []RPMObservation `json:"rpm_observations,omitempty"`
}

// DiagnosisProposal is emitted when the virtual panel is ready to diagnose.
type DiagnosisProposal struct {
	Diagnosis    string              `json:"diagnosis"`
	Confidence   float64             `json:"confidence"`
	Differential []string            `json:"differential,omitempty"`
	Rationale    string              `json:"rationale"`
	Citations    []DiagnosisCitation `json:"citations,omitempty"`
}

// DiagnosisCitation captures one piece of supporting evidence retrieved from
// the knowledge corpus and surfaced on a DiagnosisProposal. Populated only on
// the RAG-enabled LLM diagnostician path; absent on rule-based / non-RAG output.
type DiagnosisCitation struct {
	Source  string  `json:"source"`
	Title   string  `json:"title"`
	Snippet string  `json:"snippet"`
	Score   float64 `json:"score,omitempty"`
}

// EncodeState serializes case state into message Content.
func EncodeState(s CaseState) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// DecodeState parses case state from message Content.
func DecodeState(content string) (CaseState, error) {
	var s CaseState
	if err := json.Unmarshal([]byte(content), &s); err != nil {
		return CaseState{}, err
	}
	return s, nil
}

// EncodePresentation serializes intake payload.
func EncodePresentation(p PatientPresentation) string {
	b, _ := json.Marshal(p)
	return string(b)
}

// DecodePresentation parses intake payload.
func DecodePresentation(content string) (PatientPresentation, error) {
	var p PatientPresentation
	if err := json.Unmarshal([]byte(content), &p); err != nil {
		return PatientPresentation{}, err
	}
	return p, nil
}
