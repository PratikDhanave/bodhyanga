# Bodhyanga — Domain Data Model

Every clinical type lives in `pkg/medical`. This document is the type catalogue: shape, semantics, when to use, and the JSON encoding contracts.

> Pairs with [agents.md](agents.md) (which agents touch which type) and [api.md](api.md) (HTTP request/response shapes).

---

## Contents

- [Encoding helpers](#encoding-helpers)
- [Diagnostic types](#diagnostic-types)
- [Care-services types](#care-services-types)
- [Test catalog](#test-catalog)
- [HITL types](#hitl-types)
- [Provenance](#provenance)
- [Adding a condition](#adding-a-condition)

---

## Encoding helpers

Every domain type carries `Encode*` / `Decode*` JSON helpers so payloads can move over the bus without sharing concrete types between agents.

```go
medical.EncodePresentation, DecodePresentation
medical.EncodeState,        DecodeState
medical.EncodeCarePlan,     DecodeCarePlan
medical.EncodeDischarge,    DecodeDischarge
medical.EncodeADT,          DecodeADT
medical.EncodeProposal,     DecodeProposal
```

The encoders return `string` (for `msg.Content`); decoders accept `string` and return `(T, error)`. Conventions:

- All times are RFC3339 / ISO 8601 with timezone, UTC preferred.
- Empty maps / slices serialise as `{}` / `[]` (not omitted) so consumers don't have to nil-check.
- `patient_id`, `case_id`, `tenant_id` are opaque strings — never assume they're integers, MRN-shaped, or UUID-shaped specifically.

---

## Diagnostic types

### `PatientPresentation`

Entry point for the diagnostic flow.

```go
type PatientPresentation struct {
    ChiefComplaint   string             `json:"chief_complaint"`
    HPI              string             `json:"hpi"`               // history of present illness
    Demographics     string             `json:"demographics,omitempty"`
    Vitals           string             `json:"vitals,omitempty"`
    Condition        string             `json:"condition,omitempty"`         // hint for test_planner dispatch
    RPMObservations  []RPMObservation   `json:"rpm_observations,omitempty"`
}
```

- `Condition` is the dispatch hint (`cap`, `chf`, `acs`, `sepsis`, `dka`, `asthma`, ...). Empty defaults to `cap` workup.
- `RPMObservations` carries pre-arrival monitoring readings; `rpm_monitor` evaluates them on intake.

### `CaseState`

The single source of truth as the case flows through agents. Each agent reads it, optionally mutates, and re-emits.

```go
type CaseState struct {
    CaseID            string                 `json:"case_id"`
    PatientID         string                 `json:"patient_id,omitempty"`
    Condition         string                 `json:"condition,omitempty"`
    ChiefComplaint    string                 `json:"chief_complaint"`
    HPI               string                 `json:"hpi,omitempty"`
    Demographics      string                 `json:"demographics,omitempty"`
    Vitals            string                 `json:"vitals,omitempty"`
    Questions         []string               `json:"questions"`             // questioner appends
    Answers           map[string]string      `json:"answers"`               // questioner appends
    Hypotheses        []string               `json:"hypotheses"`            // questioner appends
    TestsOrdered      []string               `json:"tests_ordered"`         // test_planner appends
    TestResults       map[string]string      `json:"test_results"`          // test_planner appends
    TotalCostUSD      float64                `json:"total_cost_usd"`        // test_planner accumulates
    MaxCostUSD        float64                `json:"max_cost_usd"`          // intake reads from metadata
    WorkingDiagnosis  string                 `json:"working_diagnosis,omitempty"`    // reasoning_verifier sets
    DifferentialDx    []string               `json:"differential_dx,omitempty"`
    Rationale         string                 `json:"rationale,omitempty"`
    Confidence        float64                `json:"confidence,omitempty"`
    GoldDiagnosis     string                 `json:"gold_diagnosis,omitempty"`       // benchmark only
    RPMObservations   []RPMObservation       `json:"rpm_observations,omitempty"`
    RPMAlerts         []RPMAlert             `json:"rpm_alerts,omitempty"`           // rpm_monitor sets
    Citations         []DiagnosisCitation    `json:"citations,omitempty"`            // RAG citations
}
```

**Critical:** the agents mutate by **replacement** of slices/maps in their copies, not in-place mutation across goroutines. The bus serialises by JSON encoding — each handler decodes a fresh `CaseState` from `msg.Content`.

### `DiagnosisProposal`

What `diagnostician` emits to `reasoning_verifier`.

```go
type DiagnosisProposal struct {
    Diagnosis      string              `json:"diagnosis"`
    Confidence     float64             `json:"confidence"`           // 0.0–1.0
    Differential   []string            `json:"differential,omitempty"`
    Rationale      string              `json:"rationale"`            // required non-empty
    Citations      []DiagnosisCitation `json:"citations,omitempty"`  // RAG citations
    ProvenanceJSON string              `json:"provenance,omitempty"` // see Provenance section
}
```

### `DiagnosticTest`

Single test entry (rarely used directly — `TestCatalog` + `CaseState.TestsOrdered` carry the data in flight).

```go
type DiagnosticTest struct {
    Name    string  `json:"name"`     // matches TestCatalog key
    CostUSD float64 `json:"cost_usd"`
    Result  string  `json:"result,omitempty"`
    Ordered bool    `json:"ordered"`
}
```

### `RPMObservation`

Remote-monitoring reading.

```go
type RPMObservation struct {
    PatientID string    `json:"patient_id"`
    Device    string    `json:"device"`     // scale | bp_cuff | pulse_ox | glucose
    Metric    string    `json:"metric"`     // weight_lb | sbp | dbp | spo2 | glucose
    Value     float64   `json:"value"`
    Unit      string    `json:"unit"`
    TakenAt   time.Time `json:"taken_at"`
}
```

### `RPMAlert`

Derived from observation against threshold.

```go
type RPMAlert struct {
    Metric    string  `json:"metric"`
    Value     float64 `json:"value"`
    Threshold float64 `json:"threshold"`
    Severity  string  `json:"severity"`     // info | warn | urgent
    Reason    string  `json:"reason"`
}
```

### `DiagnosisCitation`

RAG / knowledge-store citation attached to a proposal.

```go
type DiagnosisCitation struct {
    Source  string  `json:"source"`            // "uptodate", "nejm", "internal_kb"
    Title   string  `json:"title"`
    Snippet string  `json:"snippet"`
    Score   float64 `json:"score"`             // 0.0-1.0 retriever score
}
```

---

## Care-services types

### `PatientRecord`

Population-health snapshot. Lives in `medical.PatientRegistry`.

```go
type PatientRecord struct {
    PatientID         string    `json:"patient_id"`
    Name              string    `json:"name,omitempty"`
    Age               int       `json:"age"`
    Sex               string    `json:"sex"`               // M | F | X
    Conditions        []string  `json:"conditions"`        // condition keys
    LastA1c           float64   `json:"last_a1c,omitempty"`
    LastA1cDate       time.Time `json:"last_a1c_date,omitempty"`
    LastBPSystolic    int       `json:"last_bp_systolic,omitempty"`
    LastBPDate        time.Time `json:"last_bp_date,omitempty"`
    LastWeightLb      float64   `json:"last_weight_lb,omitempty"`
    LastWeightDate    time.Time `json:"last_weight_date,omitempty"`
    LastACTScore      float64   `json:"last_act_score,omitempty"`      // asthma (PR #6)
    LastACTDate       time.Time `json:"last_act_date,omitempty"`       // asthma (PR #6)
    LastEncounterDate time.Time `json:"last_encounter_date,omitempty"`
    DischargeDate     time.Time `json:"discharge_date,omitempty"`
    Medications       []string  `json:"medications,omitempty"`         // free-form med strings
    RPMEnrolled       bool      `json:"rpm_enrolled,omitempty"`
}
```

`Conditions` is the list of condition keys (`htn`, `dm`, `chf`, `ckd`, `copd`, `asthma`). Panel rules filter by condition.

### `PatientRegistry`

Thread-safe roster.

```go
type PatientRegistry struct { /* sync.RWMutex + map */ }

func (r *PatientRegistry) Upsert(rec PatientRecord)
func (r *PatientRegistry) Get(id string) (PatientRecord, bool)
func (r *PatientRegistry) List() []PatientRecord
func (r *PatientRegistry) FilterByCondition(code string) []PatientRecord
```

Production swaps to `*pkg/persistence/postgres.PatientRegistryAdapter` which has the same method set + tenant-aware variants on `*Store`.

### `CarePlan`

What `cdm_planner` builds.

```go
type CarePlan struct {
    CaseID      string         `json:"case_id"`
    PatientID   string         `json:"patient_id"`
    Condition   string         `json:"condition"`
    Goals       []Goal         `json:"goals"`
    Medications []Medication   `json:"medications"`
    Monitoring  []Monitoring   `json:"monitoring"`
    Education   []string       `json:"education"`
    NextReview  time.Time      `json:"next_review"`
}

type Goal struct {
    Metric   string  `json:"metric"`        // "a1c" | "sbp" | "act_score" | "weight_lb" | "peak_flow_zone"
    Target   float64 `json:"target"`
    Unit     string  `json:"unit"`          // "%" | "mmHg" | "score" | "lb" | "personal_best_zone"
    WindowDays int   `json:"window_days"`
}

type Medication struct {
    Name      string `json:"name"`
    Dose      string `json:"dose"`
    Frequency string `json:"frequency"`
    Class     string `json:"class"`        // snake_case: "diuretic" | "inhaled_corticosteroid" | ...
}

type Monitoring struct {
    Metric  string `json:"metric"`
    Cadence string `json:"cadence"`        // "daily" | "weekly" | "monthly" | "quarterly"
    Source  string `json:"source"`         // "self_report" | "rpm" | "lab" | "device"
    LowThreshold  *float64 `json:"low_threshold,omitempty"`
    HighThreshold *float64 `json:"high_threshold,omitempty"`
}
```

`medical.BuildCarePlan(caseID, patientID, condition, now)` returns a fully-populated `CarePlan`. Condition keys: `htn`, `dm`, `chf`, `ckd`, `copd`, `asthma`.

### `DischargeSummary`

TCM input.

```go
type DischargeSummary struct {
    PatientID       string    `json:"patient_id"`
    CaseID          string    `json:"case_id"`
    PrimaryDx       string    `json:"primary_dx"`
    Disposition     string    `json:"disposition"`        // "home" | "snf" | "home_with_home_health" | ...
    HomeMeds        []string  `json:"home_meds"`
    NewMeds         []string  `json:"new_meds"`
    StoppedMeds     []string  `json:"stopped_meds"`
    FollowUpDays    int       `json:"follow_up_days"`     // 7 | 14 | 30
    HighRisk        bool      `json:"high_risk"`
    DischargedAt    time.Time `json:"discharged_at"`
}
```

### `MedReconciliation`

`tcm_coordinator` produces this from the three med lists.

```go
type MedReconciliation struct {
    Added     []string `json:"added"`
    Stopped   []string `json:"stopped"`
    Kept      []string `json:"kept"`
    Conflicts []string `json:"conflicts"`    // detected drug-drug / dose conflicts
}
```

### `PanelReport`

`panel_manager` output.

```go
type PanelReport struct {
    PanelSize     int                    `json:"panel_size"`
    Gaps          []CareGap              `json:"gaps"`
    ConditionRoll map[string]int         `json:"condition_roll"`     // condition -> count
}

type CareGap struct {
    PatientID string `json:"patient_id"`
    Rule      string `json:"rule"`             // matches DefaultPanelRules rule names
    Severity  string `json:"severity"`         // info | warn | urgent
    Detail    string `json:"detail"`
}
```

### `ADTEvent`

Admit / Discharge / Transfer.

```go
type ADTEvent struct {
    EventID    string    `json:"event_id"`
    PatientID  string    `json:"patient_id"`
    Type       string    `json:"type"`             // admit | discharge | transfer | update
    Facility   string    `json:"facility"`
    Unit       string    `json:"unit,omitempty"`
    OccurredAt time.Time `json:"occurred_at"`
    PrimaryDx  string    `json:"primary_dx,omitempty"`
}
```

HL7 v2 ADT messages parse into this type via [pkg/hl7v2](../pkg/hl7v2/) (PR #8).

### `ReadmissionRisk`

```go
type ReadmissionRisk struct {
    PatientID string  `json:"patient_id"`
    Score     float64 `json:"score"`        // 0.0–1.0
    Band      string  `json:"band"`         // low | medium | high
    Drivers   []string `json:"drivers"`     // ["recent_discharge", "chf", "age_75+"]
}
```

`medical.ScoreReadmissionRisk` is a transparent rule-based (LACE-inspired) score. **Not** an opaque ML model — every driver is enumerable.

---

## Test catalog

`medical.TestCatalog` is `map[string]float64` keyed by test name → cost USD. **After PR #11**:

| Test key | Cost USD | Indication |
|---|---|---|
| `complete_blood_count` | 45 | infection, anemia |
| `basic_metabolic_panel` | 55 | electrolytes, renal, glucose |
| `chest_xray` | 120 | pulmonary infiltrate, CHF |
| `ecg` | 80 | cardiac (ACS, arrhythmia) |
| `urinalysis` | 35 | UTI, glycosuria, ketones |
| `ct_chest` | 850 | high cost — PE, complex CAP |
| `blood_culture` | 95 | sepsis |
| `arterial_blood_gas` | 75 | DKA, respiratory failure |
| `troponin_i` | 55 | ACS (PR #11) |
| `bnp` | 95 | CHF (PR #11) |
| `lactate` | 35 | sepsis (PR #11) |
| `urine_ketones` | 15 | DKA (PR #11) |
| `procalcitonin` | 75 | sepsis (bacterial vs viral, PR #11) |

`test_planner` dispatches by condition (PR #11). See [agents.md § test_planner](agents.md#test_planner).

---

## HITL types

### `ReviewRequest`

What the `human_review` agent queues.

```go
type ReviewRequest struct {
    ID            string         `json:"id"`
    CreatedAt     time.Time      `json:"created_at"`
    Kind          string         `json:"kind"`              // diagnosis_review | care_plan_review | ...
    Priority      string         `json:"priority"`          // low | normal | high | urgent
    Subject       string         `json:"subject,omitempty"`
    Status        string         `json:"status"`            // pending | approved | rejected | expired
    SLADeadline   *time.Time     `json:"sla_deadline,omitempty"`
    OriginalMsg   protocol.Message `json:"original_msg"`    // the gated payload
    NextTo        string         `json:"next_to"`           // hitl_next_to
    NextType      string         `json:"next_type"`         // hitl_next_type
    DecidedAt     *time.Time     `json:"decided_at,omitempty"`
    ReviewerID    string         `json:"reviewer_id,omitempty"`
    Decision      string         `json:"decision,omitempty"` // approve | approve_with_modifications | reject
    Reason        string         `json:"reason,omitempty"`
    Modification  string         `json:"modification,omitempty"`
}
```

---

## Provenance

`pkg/medical/provenance.go` (added by the HIPAA-compliance commit) tracks the lineage of a `DiagnosisProposal`:

```go
type Provenance struct {
    GeneratedAt time.Time     `json:"generated_at"`
    AgentID     string        `json:"agent_id"`
    Method      string        `json:"method"`         // rule | llm | llm-debate | llm-reflexion | llm-cascade
    Provider    string        `json:"provider,omitempty"`   // anthropic | openai | ollama
    Model       string        `json:"model,omitempty"`
    Inputs      []string      `json:"inputs"`         // citations / test names used
    Confidence  float64       `json:"confidence"`
    Steps       []Step        `json:"steps,omitempty"`     // for multi-call methods
}

type Step struct {
    Round      int       `json:"round"`
    Action     string    `json:"action"`              // produce | critique | revise | escalate
    OutputJSON string    `json:"output_json"`         // serialized intermediate
    At         time.Time `json:"at"`
}
```

Carried as `DiagnosisProposal.ProvenanceJSON`. Used by the LLM-as-judge (for the differential-quality rubric), the audit log (for forensic reconstruction of which model produced which diagnosis), and the bench runner (for per-method comparison).

---

## Adding a condition

The asthma PR (#6) is the canonical worked example. Steps:

1. **`pkg/medical/careplan.go`** — add a `case "asthma":` branch to `BuildCarePlan` mirroring the existing templates (goals, medications, monitoring, education, next review).

2. **`pkg/medical/registry.go`** — if the condition has unique vitals/labs the panel rules need (e.g. asthma's `LastACTScore`), add fields to `PatientRecord` mirroring the existing `LastA1c` pattern.

3. **`pkg/medical/panel.go`** — add panel rules to `DefaultPanelRules()`:
   - Gap-in-care rules (`no_controller_persistent_asthma`)
   - Overdue rules (`act_overdue`)
   - Threshold rules where applicable

4. **`agents/test_planner.go`** — add a `conditionWorkups["X"]` entry with the right tests.

5. **`agents/diagnostician.go`** — add a specialty inference rule to `inferDiagnosis()` with thresholds. **Every threshold gets a comment** noting it's illustrative, not a clinical guideline.

6. **`pkg/medical/types.go`** — extend `TestCatalog` with any new tests needed.

7. **Tests** — table-driven in `pkg/medical/careplan_test.go`, `pkg/medical/panel_test.go`, `agents/test_planner_test.go`, `agents/diagnostician_test.go`.

8. **README** — add to the Condition keys list and the Panel rule set table.

9. **Fixture** — add a synthetic case under `data/cases/` with `condition: "X"` and a `gold_diagnosis`. Update `data/cases/manifest.json` and re-run `cmd/bench -update-baseline`.

**Zero platform / orchestrator / agent-framework changes required.** Pure `pkg/medical` + agent extension. That's the composition pattern working.
