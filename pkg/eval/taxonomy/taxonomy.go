// Package taxonomy implements the Three Gulfs failure taxonomy from
// "Evals for AI Engineers" (Shankar & Husain). It classifies every observed
// failure into one of three categories:
//
//   - Comprehension: the developer does not understand the data (inputs or
//     outputs) well enough to write correct eval criteria.
//   - Specification: the prompt or instructions do not capture the developer's
//     intent clearly enough for the LLM to follow.
//   - Generalization: the LLM fails to produce the correct output despite
//     clear, well-understood instructions.
//
// The package ships a DefaultMedicalTaxonomy pre-populated with failure modes
// specific to Bodh's clinical AI pipeline: diagnostics, care plans,
// governance, interoperability, agent orchestration, and evaluation.
//
// Typical usage:
//
//	tax := taxonomy.DefaultMedicalTaxonomy()
//	critical := tax.FindBySeverity(taxonomy.SeverityCritical)
//	stats := tax.Stats()
package taxonomy

import "fmt"

// Gulf classifies the root cause of a failure according to the Three Gulfs
// model.
type Gulf string

const (
	// GulfComprehension means the developer does not understand the data
	// well enough to write correct eval criteria.
	GulfComprehension Gulf = "comprehension"

	// GulfSpecification means the prompt or instructions do not capture the
	// developer's intent clearly enough for the LLM to follow.
	GulfSpecification Gulf = "specification"

	// GulfGeneralization means the LLM fails despite clear, well-understood
	// instructions.
	GulfGeneralization Gulf = "generalization"
)

// Severity indicates how dangerous a failure mode is when it fires.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
)

// FailureMode describes one class of failure that can occur in the system.
type FailureMode struct {
	ID          string   `json:"id"`                  // e.g. "FM-DX-001"
	Name        string   `json:"name"`                // human-readable label
	Domain      string   `json:"domain"`              // diagnostic, care, governance, interop, agent, eval
	Gulf        Gulf     `json:"gulf"`                // which gulf the failure belongs to
	Severity    Severity `json:"severity"`            // how dangerous when it fires
	Category    string   `json:"category"`            // operational, safety, compliance, accuracy
	Description string   `json:"description"`         // what goes wrong and why it matters
	Example     string   `json:"example"`             // concrete scenario illustrating the failure
	HIPAARef    string   `json:"hipaa_ref,omitempty"` // HIPAA section reference if applicable
	Evaluator   string   `json:"evaluator,omitempty"` // which evaluator or test catches this
}

// TaxonomyStats summarises a FailureTaxonomy for dashboards and reports.
type TaxonomyStats struct {
	Total      int              `json:"total"`
	ByGulf     map[Gulf]int     `json:"by_gulf"`
	BySeverity map[Severity]int `json:"by_severity"`
	ByDomain   map[string]int   `json:"by_domain"`
	ByCategory map[string]int   `json:"by_category"`
}

// FailureTaxonomy is the registry of all known failure modes. It maintains
// indexes by domain, gulf, and severity for fast lookup.
type FailureTaxonomy struct {
	Modes      []FailureMode
	ByDomain   map[string][]FailureMode
	ByGulf     map[Gulf][]FailureMode
	BySeverity map[Severity][]FailureMode
}

// NewTaxonomy creates an empty FailureTaxonomy with initialised maps.
func NewTaxonomy() *FailureTaxonomy {
	return &FailureTaxonomy{
		ByDomain:   make(map[string][]FailureMode),
		ByGulf:     make(map[Gulf][]FailureMode),
		BySeverity: make(map[Severity][]FailureMode),
	}
}

// Register adds a FailureMode to the taxonomy, updating all indexes.
func (t *FailureTaxonomy) Register(fm FailureMode) {
	t.Modes = append(t.Modes, fm)
	t.ByDomain[fm.Domain] = append(t.ByDomain[fm.Domain], fm)
	t.ByGulf[fm.Gulf] = append(t.ByGulf[fm.Gulf], fm)
	t.BySeverity[fm.Severity] = append(t.BySeverity[fm.Severity], fm)
}

// FindByDomain returns all failure modes for the given domain (e.g.
// "diagnostic", "care", "governance"). Returns nil when no modes match.
func (t *FailureTaxonomy) FindByDomain(domain string) []FailureMode {
	return t.ByDomain[domain]
}

// FindByGulf returns all failure modes for the given gulf.
func (t *FailureTaxonomy) FindByGulf(gulf Gulf) []FailureMode {
	return t.ByGulf[gulf]
}

// FindBySeverity returns all failure modes at the given severity level.
func (t *FailureTaxonomy) FindBySeverity(sev Severity) []FailureMode {
	return t.BySeverity[sev]
}

// FindByID returns the first failure mode with the given ID and true, or the
// zero value and false if no match exists.
func (t *FailureTaxonomy) FindByID(id string) (FailureMode, bool) {
	for _, fm := range t.Modes {
		if fm.ID == id {
			return fm, true
		}
	}
	return FailureMode{}, false
}

// Stats returns aggregate counts across every dimension.
func (t *FailureTaxonomy) Stats() TaxonomyStats {
	s := TaxonomyStats{
		Total:      len(t.Modes),
		ByGulf:     make(map[Gulf]int),
		BySeverity: make(map[Severity]int),
		ByDomain:   make(map[string]int),
		ByCategory: make(map[string]int),
	}
	for _, fm := range t.Modes {
		s.ByGulf[fm.Gulf]++
		s.BySeverity[fm.Severity]++
		s.ByDomain[fm.Domain]++
		s.ByCategory[fm.Category]++
	}
	return s
}

// String returns a human-readable summary, useful for debugging.
func (t *FailureTaxonomy) String() string {
	s := t.Stats()
	return fmt.Sprintf("FailureTaxonomy: %d modes (%d comprehension, %d specification, %d generalization)",
		s.Total,
		s.ByGulf[GulfComprehension],
		s.ByGulf[GulfSpecification],
		s.ByGulf[GulfGeneralization],
	)
}

// DefaultMedicalTaxonomy returns a FailureTaxonomy pre-populated with
// Bodh-specific failure modes spanning diagnostics, care plans, governance,
// interoperability, agent orchestration, and evaluation.
func DefaultMedicalTaxonomy() *FailureTaxonomy {
	t := NewTaxonomy()

	// ── Diagnostic domain ──────────────────────────────────────────────

	t.Register(FailureMode{
		ID:          "FM-DX-001",
		Name:        "Missing diagnostic constraint",
		Domain:      "diagnostic",
		Gulf:        GulfGeneralization,
		Severity:    SeverityHigh,
		Category:    "accuracy",
		Description: "LLM omits a clinically required constraint (e.g. age, pregnancy status) that narrows the differential.",
		Example:     "Diagnostician suggests methotrexate without checking pregnancy status.",
		Evaluator:   "constraint_checker",
	})

	t.Register(FailureMode{
		ID:          "FM-DX-002",
		Name:        "Hallucinated diagnosis",
		Domain:      "diagnostic",
		Gulf:        GulfGeneralization,
		Severity:    SeverityCritical,
		Category:    "safety",
		Description: "LLM invents a condition unsupported by the presented evidence.",
		Example:     "Patient presents with cough; LLM diagnoses pheochromocytoma with no supporting labs.",
		Evaluator:   "llm_judge",
	})

	t.Register(FailureMode{
		ID:          "FM-DX-003",
		Name:        "Wrong differential ranking",
		Domain:      "diagnostic",
		Gulf:        GulfGeneralization,
		Severity:    SeverityHigh,
		Category:    "accuracy",
		Description: "The correct diagnosis appears in the differential but is ranked too low to influence care.",
		Example:     "ACS listed 5th when chest pain + troponin elevation should place it first.",
		Evaluator:   "llm_judge",
	})

	t.Register(FailureMode{
		ID:          "FM-DX-004",
		Name:        "Fallback not triggered on LLM timeout",
		Domain:      "diagnostic",
		Gulf:        GulfSpecification,
		Severity:    SeverityCritical,
		Category:    "operational",
		Description: "When the LLM provider times out the system does not fall back to the rule-based diagnostician.",
		Example:     "Anthropic API returns 504; pipeline stalls instead of switching to rule engine.",
		Evaluator:   "timeout_monitor",
	})

	t.Register(FailureMode{
		ID:          "FM-DX-005",
		Name:        "Diagnosis contradicts chief complaint",
		Domain:      "diagnostic",
		Gulf:        GulfGeneralization,
		Severity:    SeverityHigh,
		Category:    "accuracy",
		Description: "Working diagnosis is anatomically or physiologically inconsistent with the chief complaint.",
		Example:     "Chief complaint: acute knee pain; diagnosis: migraine.",
		Evaluator:   "llm_judge",
	})

	// ── Care plan domain ───────────────────────────────────────────────

	t.Register(FailureMode{
		ID:          "FM-CP-001",
		Name:        "Incomplete care plan",
		Domain:      "care",
		Gulf:        GulfSpecification,
		Severity:    SeverityMedium,
		Category:    "accuracy",
		Description: "Generated care plan is missing required sections (e.g. follow-up, patient education).",
		Example:     "Diabetes care plan has no follow-up HbA1c schedule.",
		Evaluator:   "care_plan_validator",
	})

	t.Register(FailureMode{
		ID:          "FM-CP-002",
		Name:        "Wrong medication dose",
		Domain:      "care",
		Gulf:        GulfGeneralization,
		Severity:    SeverityCritical,
		Category:    "safety",
		Description: "Medication dose is outside the safe therapeutic range for the patient's weight or renal function.",
		Example:     "Metformin 5000mg daily recommended for a patient with eGFR 25.",
		Evaluator:   "drug_dose_checker",
	})

	t.Register(FailureMode{
		ID:          "FM-CP-003",
		Name:        "Missing drug interaction check",
		Domain:      "care",
		Gulf:        GulfGeneralization,
		Severity:    SeverityHigh,
		Category:    "safety",
		Description: "Care plan prescribes medications with known dangerous interactions without flagging.",
		Example:     "Warfarin + aspirin prescribed simultaneously with no bleeding risk note.",
		Evaluator:   "interaction_checker",
	})

	t.Register(FailureMode{
		ID:          "FM-CP-004",
		Name:        "Care plan ignores patient allergy",
		Domain:      "care",
		Gulf:        GulfComprehension,
		Severity:    SeverityCritical,
		Category:    "safety",
		Description: "The allergy list was present in the input but the LLM prescribed a contraindicated drug.",
		Example:     "Penicillin allergy documented; care plan prescribes amoxicillin.",
		Evaluator:   "allergy_checker",
	})

	t.Register(FailureMode{
		ID:          "FM-CP-005",
		Name:        "Non-formulary medication suggested",
		Domain:      "care",
		Gulf:        GulfSpecification,
		Severity:    SeverityLow,
		Category:    "operational",
		Description: "The suggested medication is not on the institution's formulary.",
		Example:     "Brand-name drug recommended when generic equivalent is available and required by policy.",
		Evaluator:   "formulary_checker",
	})

	// ── Governance domain ──────────────────────────────────────────────

	t.Register(FailureMode{
		ID:          "FM-GV-001",
		Name:        "PHI leak in audit log",
		Domain:      "governance",
		Gulf:        GulfGeneralization,
		Severity:    SeverityCritical,
		Category:    "compliance",
		Description: "Protected health information (names, MRNs) appears in an audit log or OTel span.",
		Example:     "Full patient name written to bodh.case_id span attribute instead of SHA-256 hash.",
		HIPAARef:    "HIPAA §164.312",
		Evaluator:   "phi_scanner",
	})

	t.Register(FailureMode{
		ID:          "FM-GV-002",
		Name:        "Governance bypass on malformed message",
		Domain:      "governance",
		Gulf:        GulfSpecification,
		Severity:    SeverityHigh,
		Category:    "compliance",
		Description: "A malformed bus message skips governance checks and reaches an agent unchecked.",
		Example:     "Empty JSON body passes validation and reaches the diagnostician without safety checks.",
		Evaluator:   "fuzz_tester",
	})

	t.Register(FailureMode{
		ID:          "FM-GV-003",
		Name:        "Break-glass without audit event",
		Domain:      "governance",
		Gulf:        GulfSpecification,
		Severity:    SeverityCritical,
		Category:    "compliance",
		Description: "Emergency override (break-glass) is exercised but no audit event is recorded.",
		Example:     "Admin overrides safety gate; audit log has no entry for the override.",
		HIPAARef:    "HIPAA §164.312(b)",
		Evaluator:   "audit_completeness",
	})

	t.Register(FailureMode{
		ID:          "FM-GV-004",
		Name:        "Audit log tampering not detected",
		Domain:      "governance",
		Gulf:        GulfSpecification,
		Severity:    SeverityHigh,
		Category:    "compliance",
		Description: "Append-only audit log does not detect or prevent retroactive modification.",
		Example:     "Admin deletes an audit entry; no integrity check fires.",
		HIPAARef:    "HIPAA §164.312(b)",
		Evaluator:   "audit_integrity",
	})

	// ── Interop (HL7/FHIR) domain ─────────────────────────────────────

	t.Register(FailureMode{
		ID:          "FM-HL-001",
		Name:        "HL7 parse failure on valid message",
		Domain:      "interop",
		Gulf:        GulfComprehension,
		Severity:    SeverityMedium,
		Category:    "operational",
		Description: "The HL7 v2 parser rejects a syntactically valid ADT message.",
		Example:     "ADT^A01 with optional Z-segments causes parser panic.",
		Evaluator:   "hl7_conformance",
	})

	t.Register(FailureMode{
		ID:          "FM-HL-002",
		Name:        "Wrong patient ID mapping",
		Domain:      "interop",
		Gulf:        GulfGeneralization,
		Severity:    SeverityHigh,
		Category:    "safety",
		Description: "Patient identifiers from the HL7 message are mapped to the wrong internal case.",
		Example:     "Two patients share an MRN in different namespaces; system conflates them.",
		Evaluator:   "patient_id_audit",
	})

	t.Register(FailureMode{
		ID:          "FM-HL-003",
		Name:        "FHIR resource version conflict",
		Domain:      "interop",
		Gulf:        GulfComprehension,
		Severity:    SeverityMedium,
		Category:    "operational",
		Description: "Concurrent updates to the same FHIR resource lead to silent data loss.",
		Example:     "Two nurses update the same observation; later write silently overwrites earlier one.",
		Evaluator:   "fhir_version_check",
	})

	// ── Agent domain ───────────────────────────────────────────────────

	t.Register(FailureMode{
		ID:          "FM-AG-001",
		Name:        "Agent panic leaks PHI",
		Domain:      "agent",
		Gulf:        GulfGeneralization,
		Severity:    SeverityCritical,
		Category:    "compliance",
		Description: "An agent panics and the stack trace includes patient data from its context.",
		Example:     "Diagnostician panics on malformed input; Go stack dump includes patient name.",
		HIPAARef:    "HIPAA §164.312",
		Evaluator:   "panic_handler",
	})

	t.Register(FailureMode{
		ID:          "FM-AG-002",
		Name:        "Bus message lost silently",
		Domain:      "agent",
		Gulf:        GulfGeneralization,
		Severity:    SeverityHigh,
		Category:    "operational",
		Description: "A message on the orchestration bus is dropped without error or audit entry.",
		Example:     "Test planner publishes results but diagnostician never receives them due to channel overflow.",
		Evaluator:   "bus_delivery_audit",
	})

	t.Register(FailureMode{
		ID:          "FM-AG-003",
		Name:        "Agent retry storm",
		Domain:      "agent",
		Gulf:        GulfSpecification,
		Severity:    SeverityMedium,
		Category:    "operational",
		Description: "An agent retries a failed LLM call indefinitely, consuming resources and budget.",
		Example:     "Diagnostician agent retries 200 times against a rate-limited API.",
		Evaluator:   "retry_budget_monitor",
	})

	t.Register(FailureMode{
		ID:          "FM-AG-004",
		Name:        "Agent deadlock on HITL wait",
		Domain:      "agent",
		Gulf:        GulfSpecification,
		Severity:    SeverityHigh,
		Category:    "operational",
		Description: "The pipeline blocks indefinitely waiting for human-in-the-loop feedback that never arrives.",
		Example:     "Care plan requires attending sign-off; attending is off-shift; pipeline stalls for hours.",
		Evaluator:   "hitl_timeout_monitor",
	})

	// ── Eval domain ────────────────────────────────────────────────────

	t.Register(FailureMode{
		ID:          "FM-EV-001",
		Name:        "LLM-as-Judge disagrees with human",
		Domain:      "eval",
		Gulf:        GulfGeneralization,
		Severity:    SeverityMedium,
		Category:    "accuracy",
		Description: "The LLM judge grades an output as passing when a human reviewer marks it failing, or vice versa.",
		Example:     "Judge gives 0.9 to a diagnosis that a clinician rated as dangerously wrong.",
		Evaluator:   "judge_agreement",
	})

	t.Register(FailureMode{
		ID:          "FM-EV-002",
		Name:        "Eval data contamination",
		Domain:      "eval",
		Gulf:        GulfComprehension,
		Severity:    SeverityHigh,
		Category:    "accuracy",
		Description: "The eval dataset overlaps with the LLM's training data, inflating accuracy.",
		Example:     "SD-Bench cases used verbatim in both training and evaluation.",
		Evaluator:   "contamination_detector",
	})

	t.Register(FailureMode{
		ID:          "FM-EV-003",
		Name:        "Metric ceiling effect",
		Domain:      "eval",
		Gulf:        GulfComprehension,
		Severity:    SeverityLow,
		Category:    "accuracy",
		Description: "All models score near 1.0 on an eval, making it impossible to distinguish quality.",
		Example:     "Binary accuracy metric on easy cases gives every provider 98%+.",
		Evaluator:   "metric_distribution_check",
	})

	t.Register(FailureMode{
		ID:          "FM-EV-004",
		Name:        "Drift baseline stale",
		Domain:      "eval",
		Gulf:        GulfSpecification,
		Severity:    SeverityMedium,
		Category:    "operational",
		Description: "The drift monitor baseline has not been updated after a model or prompt change, producing false alerts.",
		Example:     "Baseline from GPT-4o era triggers alarms after migrating to Claude.",
		Evaluator:   "drift_baseline_age",
	})

	t.Register(FailureMode{
		ID:          "FM-EV-005",
		Name:        "Equity gap undetected",
		Domain:      "eval",
		Gulf:        GulfComprehension,
		Severity:    SeverityHigh,
		Category:    "safety",
		Description: "Aggregate accuracy hides disparate performance across demographic subgroups.",
		Example:     "Overall accuracy 92%, but accuracy for patients over 80 is only 71%.",
		Evaluator:   "equity_analyzer",
	})

	// ── Knowledge / RAG domain ─────────────────────────────────────────

	t.Register(FailureMode{
		ID:          "FM-KN-001",
		Name:        "Stale knowledge corpus",
		Domain:      "knowledge",
		Gulf:        GulfComprehension,
		Severity:    SeverityMedium,
		Category:    "accuracy",
		Description: "The RAG corpus contains outdated clinical guidelines that contradict current standards of care.",
		Example:     "2019 sepsis guidelines retrieved when 2024 Surviving Sepsis update is available.",
		Evaluator:   "corpus_freshness",
	})

	t.Register(FailureMode{
		ID:          "FM-KN-002",
		Name:        "Retrieval returns irrelevant chunks",
		Domain:      "knowledge",
		Gulf:        GulfGeneralization,
		Severity:    SeverityMedium,
		Category:    "accuracy",
		Description: "The retrieval stage returns semantically similar but clinically irrelevant documents.",
		Example:     "Query about pediatric asthma retrieves adult COPD management guidelines.",
		Evaluator:   "retrieval_relevance",
	})

	return t
}
