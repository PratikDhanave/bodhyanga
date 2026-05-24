# Bodh — Agent Reference

Every Bodh agent implements the same five-method interface. This document is the field guide: what each agent does, what messages it accepts, what it emits, what shared state it touches, and what its failure modes are.

> See [architecture.md](architecture.md) for the orchestrator pattern, [api.md](api.md) for the HTTP surface, [data-model.md](data-model.md) for the clinical types.

---

## Contents

- [Agent contract recap](#agent-contract-recap)
- [Diagnostic panel](#diagnostic-panel-8-agents) — intake, rpm_monitor, supervisor, questioner, test_planner, cost_guardian, diagnostician, reasoning_verifier
- [Diagnostic LLM variants](#diagnostic-llm-variants) — LLMDiagnostician, DebateDiagnostician, ReflexionDiagnostician, CascadeDiagnostician, LLMDiagnosticianVision
- [Care services](#care-services-9-agents) — cdm_planner, tcm_coordinator, panel_manager, pre_visit_prep, inbox_triage, refill_manager, prior_auth, engagement, readmission_tracker
- [HITL](#hitl) — human_review
- [Capability index](#capability-index)
- [Shared state injected at construction](#shared-state-injected-at-construction)

---

## Agent contract recap

```go
type Agent interface {
    ID() string
    Name() string
    Capabilities() []string
    HandleMessage(ctx context.Context, msg Message, env Environment) ([]Message, error)
}
```

Every section below uses the same template:

- **ID** — bus address
- **Capabilities** — strings returned by `Capabilities()`
- **Input types** — `msg.Type` values the agent handles
- **Output types** — `msg.Type` values it emits
- **Shared state** — fields injected at construction
- **Behaviour** — what it does
- **HITL gate** — what gets gated when `HITL=true`
- **Failure modes** — what happens when things break

---

## Diagnostic panel (8 agents)

### `intake`

- **ID:** `intake` · **Name:** `Clinical Intake` · **Source:** [agents/intake.go](../agents/intake.go)
- **Capabilities:** `intake_presentation`
- **Input types:** `presentation`
- **Output types:** `case_state`
- **Shared state:** none
- **Behaviour:** Parses `PatientPresentation` JSON; constructs the initial `CaseState`. Merges complaint and HPI; reads `case_id` / `max_cost_usd` / `gold_diagnosis` / `condition` / RPM observations from `msg.Metadata`. Initializes empty `Answers` and `TestResults` maps.
- **HITL gate:** none
- **Failure modes:** Returns error on malformed JSON. Orchestrator records `KindAgentError` audit event; case ends before reaching the panel.

### `rpm_monitor`

- **ID:** `rpm_monitor` · **Name:** `Remote Patient Monitoring` · **Source:** [agents/rpm_monitor.go](../agents/rpm_monitor.go)
- **Capabilities:** `rpm_ingest`, `vitals_triage`
- **Input types:** `case_state`
- **Output types:** `case_state`
- **Shared state:** none
- **Behaviour:** Evaluates `medical.EvaluateRPMAlerts` over `state.RPMObservations`. Demo thresholds: SBP ≥ 160, weight_lb ≥ 185, SpO₂ < 92, HR ≥ 110. **Always forwards** to supervisor — alerts are advisory, not gating.
- **HITL gate:** none
- **Failure modes:** Returns error if `case_state` JSON malformed. RPM alerts are appended to `state.RPMAlerts` — downstream agents read them; `diagnostician` uses alert count in `chf` specialty rule.

### `diagnostic_supervisor`

- **ID:** `diagnostic_supervisor` · **Name:** `Diagnostic Supervisor` · **Source:** [agents/supervisor.go](../agents/supervisor.go)
- **Capabilities:** `supervise_diagnosis`, `sequential_diagnosis`
- **Input types:** `case_state`, `questions_complete`, `tests_complete`, `tests_revise`, `diagnosis`
- **Output types:** `case_state` (delegating), terminal log on `diagnosis`
- **Shared state:** none
- **Behaviour:** Pure state machine on `msg.Type`:

  | Incoming | Action |
  |---|---|
  | `case_state` (from RPM/intake) | forward to `questioner` |
  | `questions_complete` | forward to `test_planner` |
  | `tests_complete` | forward to `diagnostician` |
  | `tests_revise` | reroute to `test_planner` with budget-aware constraint |
  | `diagnosis` (verified) | finalize — log, score via `eval.ScoreCase`, write `InteractionRecord` if `Store` wired |

- **HITL gate:** none directly (the `reasoning_verifier` is what gates final diagnosis)
- **Failure modes:** Unknown `msg.Type` is logged and dropped. Multiple cases in flight share no state — supervisor is stateless.

### `questioner`

- **ID:** `questioner` · **Name:** `History & Symptom Questioner` · **Source:** [agents/questioner.go](../agents/questioner.go)
- **Capabilities:** `ask_question`, `history_taking`
- **Input types:** `case_state`
- **Output types:** `questions_complete`
- **Shared state:** none
- **Behaviour:** Demo: four fixed history questions; `simulatedAnswer()` returns canned text. Hypotheses keyed off `state.Condition`:
  - `chf` → ADHF / COPD-exacerbation / PNA
  - else → CAP / viral URI / PE
- **LLM variant:** `LLMQuestioner` ([agents/questioner_llm.go](../agents/questioner_llm.go)) produces 3–5 context-relevant questions with simulated patient answers + 3 starting hypotheses via the configured LLM provider, with rule-based fallback.
- **HITL gate:** none
- **Failure modes:** LLM variant falls back to rule-based on any per-call error.

### `test_planner`

- **ID:** `test_planner` · **Name:** `Diagnostic Test Planner` · **Source:** [agents/test_planner.go](../agents/test_planner.go)
- **Capabilities:** `order_test`, `interpret_results`
- **Input types:** `case_state`
- **Output types:** `case_state`
- **Shared state:** none
- **Behaviour:** **After PR #11**, dispatches on `state.Condition`:

  | Condition | Workup |
  |---|---|
  | `cap` / default | CBC + CXR + BMP |
  | `chf` | BNP + CXR + ECG + BMP |
  | `acs` | troponin_i + ECG + BMP + CBC |
  | `sepsis` | blood_culture + lactate + CBC + BMP + urinalysis |
  | `dka` | BMP + urine_ketones + ABG + CBC |

  Costs accumulate in `state.TotalCostUSD` from `medical.TestCatalog`. `simulatedTestResult(name, condition)` returns condition-appropriate values so the diagnostician's specialty rules actually trigger. Always routes through `cost_guardian` before supervisor sees `tests_complete`.
- **HITL gate:** none
- **Failure modes:** Unknown condition falls through to default respiratory workup (cap).

### `cost_guardian`

- **ID:** `cost_guardian` · **Name:** `Diagnostic Cost Guardian` · **Source:** [agents/cost_guardian.go](../agents/cost_guardian.go)
- **Capabilities:** `cost_check`, `pareto_optimization`
- **Input types:** `case_state`
- **Output types:** `tests_complete` (on approve), `tests_revise` (on deny with budget-aware constraint)
- **Shared state:** none
- **Behaviour:** Compares `state.TotalCostUSD` to `state.MaxCostUSD`. Approve → emits `tests_complete`. Deny → emits `tests_revise` with a downgraded test list and a rationale; supervisor loops back to `test_planner`. Max rework rounds: 3.
- **HITL gate:** none (budget enforcement is automatic)
- **Failure modes:** If even the downgraded workup exceeds budget, the case finalizes with `state.WorkingDiagnosis = "Insufficient data within budget"`.

### `diagnostician`

- **ID:** `diagnostician` · **Name:** `Diagnostician` · **Source:** [agents/diagnostician.go](../agents/diagnostician.go)
- **Capabilities:** `provide_diagnosis`, `differential_diagnosis`
- **Input types:** `case_state`
- **Output types:** `diagnosis_proposal`
- **Shared state:** none
- **Behaviour:** **After PR #11**, `inferDiagnosis()` runs specialty rules in order:

  | Condition | Trigger | Diagnosis | Confidence |
  |---|---|---|---|
  | `acs` | troponin > 0.04 ng/mL | NSTEMI | 0.85 |
  | `dka` | glucose > 250 AND bicarb < 18 AND ketones positive | DKA | 0.88 |
  | `sepsis` | lactate > 2 AND WBC > 12K | Urosepsis | 0.82 |
  | `chf` | BNP > 400 OR ≥2 RPM alerts | ADHF | 0.84 |
  | `cap` | CXR infiltrate | CAP | 0.87 |
  | else | low-confidence | Undifferentiated | 0.4 |

  All thresholds carry **"illustrative — educational demo, not a clinical guideline"** comments. Helpers `extractFirstNumber` (marker-after semantics) and `extractWBC` parse simulated test text.

  Sends `diagnosis_proposal` JSON to `reasoning_verifier`.
- **LLM variant:** [LLMDiagnostician](#llmdiagnostician) — see below.
- **HITL gate:** none directly (the `reasoning_verifier` gates the final diagnosis)
- **Failure modes:** Returns low-confidence "Undifferentiated" rather than no diagnosis. Reasoning verifier may still reject — see below.

### `reasoning_verifier`

- **ID:** `reasoning_verifier` · **Name:** `Clinical Reasoning Verifier` · **Source:** [agents/reasoning_verifier.go](../agents/reasoning_verifier.go)
- **Capabilities:** `verify_reasoning`, `safety_check`
- **Input types:** `diagnosis_proposal`
- **Output types:** `diagnosis` (on accept), `review_request` (when HITL=true)
- **Shared state:** optional `*hitl.Queue` for HITL routing
- **Behaviour:** Demo rule: `confidence ≥ 0.5` AND non-empty `rationale`. On accept: sets `state.WorkingDiagnosis`, emits `diagnosis` to supervisor. On reject: emits a low-confidence "Undifferentiated" diagnosis (so the supervisor still finalises) — except `demo_uri.json` hits an edge case where rejection bypasses finalisation (tracked follow-up).
- **LLM variant:** `LLMReasoningVerifier` ([agents/reasoning_verifier_llm.go](../agents/reasoning_verifier_llm.go)) judges whether the rationale is grounded in `CaseState` and confidence isn't overstated.
- **HITL gate:** when `HITL=true`, wraps the verified diagnosis in a `review_request` envelope addressed to `human_review` with `hitl_kind=diagnosis_review`. On approval, the original `diagnosis` message resumes to `diagnostic_supervisor`.
- **Failure modes:** Reject path documented above.

---

## Diagnostic LLM variants

All variants share `agents/diagnostician_factory.go` (`NewDiagnostician(provider)`) and the same fallback contract.

### `LLMDiagnostician`

- **Source:** [agents/diagnostician_llm.go](../agents/diagnostician_llm.go)
- **CLI:** `-diagnostician=llm-anthropic | llm-openai | llm-ollama`
- **Behaviour:** Builds the same `DiagnosisProposal` JSON shape from the same `CaseState` input as the rule-based agent. The cost guardian, reasoning verifier, HITL gate, and supervisor logic are unchanged. RAG support via `knowledge.HybridStore` when `BODH_KNOWLEDGE_CORPUS` is set.
- **Fallback:** Any per-call failure (timeout, malformed JSON, transport error, confidence below `MinConfidence`, hallucination check, refusal phrase) routes to `DiagnosticianAgent`.

### `LLMDiagnosticianVision`

- **Source:** [agents/diagnostician_vision.go](../agents/diagnostician_vision.go)
- **Behaviour:** When `image_path` appears in `msg.Metadata`, reads the file, sniffs PNG/JPEG, attaches as `Message.Images[0]`. All other behaviour (RAG, fallback, citations) inherited.
- **Provider support:** Anthropic (Sonnet, Opus), OpenAI (gpt-4o, gpt-4o-mini), Ollama (llama3.2-vision, bakllava, llava:13b).
- **Fallback:** Read failures and unsupported MIMEs fall through to text-only — never error out the case.

### `DebateDiagnostician`

- **Source:** [agents/diagnostician_debate.go](../agents/diagnostician_debate.go)
- **CLI:** `-diagnostician=llm-anthropic-debate` etc.
- **Behaviour:** Multiple specialist personas (cardiologist, pulmonologist, ID) each produce a proposal in parallel. A "chair" synthesises a final answer. Higher cost; useful when disagreement signal matters.

### `ReflexionDiagnostician`

- **Source:** [agents/diagnostician_reflexion.go](../agents/diagnostician_reflexion.go)
- **Behaviour:** Self-critique loop: produce → critique → revise. Up to N rounds (default 2). Stops early when critique returns "no objections."

### `CascadeDiagnostician`

- **Source:** [agents/diagnostician_cascade.go](../agents/diagnostician_cascade.go)
- **Behaviour:** Cheap small model first. Escalate to a larger model if confidence below threshold. Optimises mean cost.

### Diagnostician with tool use

- **Source:** [agents/diagnostician_tools.go](../agents/diagnostician_tools.go)
- **Behaviour:** Extension of `LLMDiagnostician` that supports `llm.Request.Tools`. Tools can be lab feeds, imaging AI, RxNorm lookup. Tool definitions live in `pkg/tools`.

---

## Care services (9 agents)

### `cdm_planner` — Chronic Disease Management

- **ID:** `cdm_planner` · **Source:** [agents/cdm_planner.go](../agents/cdm_planner.go)
- **Capabilities:** `cdm_plan`
- **Input types:** `cdm_request`
- **Output types:** `care_plan_ready`
- **Shared state:** `*medical.PatientRegistry`
- **Behaviour:** Builds a `medical.CarePlan` from condition-specific templates: `htn`, `dm`, `chf`, `ckd`, `copd`, `asthma` (added by PR #6). Each plan has goals, medications, monitoring, education, next_review.
- **HITL gate:** when `HITL=true`, gates `care_plan_ready` → `engagement` via `care_plan_review`.

### `tcm_coordinator` — Transitional Care Management

- **ID:** `tcm_coordinator` · **Source:** [agents/tcm_coordinator.go](../agents/tcm_coordinator.go)
- **Capabilities:** `tcm_coordination`
- **Input types:** `discharge_event`
- **Output types:** `tcm_tasks_created`
- **Shared state:** `*medical.PatientRegistry`, `*tasks.Queue`
- **Behaviour:** Reads `DischargeSummary` → emits N nurse tasks (med reconciliation, post-discharge call within 48h, post-discharge visit within 14d, etc.). Uses `medical.ReconcileMedications` to detect conflicts.
- **HITL gate:** when `HITL=true`, gates TCM welcome SMS → `engagement` via `tcm_welcome_review`.

### `panel_manager` — Panel Management / Gap-in-care

- **ID:** `panel_manager` · **Source:** [agents/panel_manager.go](../agents/panel_manager.go)
- **Capabilities:** `panel_management`, `gap_in_care`
- **Input types:** `panel_request`
- **Output types:** `panel_report`, `engagement_request` (per-gap)
- **Shared state:** `*medical.PatientRegistry`
- **Behaviour:** Runs `medical.DefaultPanelRules()` over the population roster. **After PR #6**, the rules are:

  | Rule | Severity | Trigger |
  |---|---|---|
  | `a1c_overdue` | warn | DM patient with no A1c in last 180 d |
  | `a1c_uncontrolled` | urgent | A1c ≥ 9 |
  | `bp_uncontrolled` | urgent | HTN patient SBP ≥ 160 |
  | `bp_elevated` | warn | HTN patient SBP ≥ 140 |
  | `annual_visit_due` | info | No encounter in 365 d |
  | `chf_weight_alert` | urgent | CHF patient weight ≥ 190 lb |
  | `tcm_followup_open` | urgent | Discharged 7–30 d ago with no post-discharge visit |
  | `no_controller_persistent_asthma` | urgent | asthma patient missing any med of class `inhaled_corticosteroid` |
  | `act_overdue` | warn | asthma patient with no ACT score in last 180 d |

- **HITL gate:** when `HITL=true`, gates per-gap outreach → `engagement` via `outreach_review`.

### `pre_visit_prep` — Virtual Office: Pre-visit chart prep

- **ID:** `pre_visit_prep` · **Source:** [agents/pre_visit_prep.go](../agents/pre_visit_prep.go)
- **Capabilities:** `pre_visit_prep`
- **Input types:** `pre_visit_request`
- **Output types:** `pre_visit_ready`
- **Shared state:** `*medical.PatientRegistry`
- **Behaviour:** Pre-visit summary for the clinician: open gaps, recent labs, medication list, last visit notes. Read-only — never modifies state.
- **HITL gate:** none (already a clinician aid; not action-taking)

### `inbox_triage` — Virtual Office: Inbox triage

- **ID:** `inbox_triage` · **Source:** [agents/inbox_triage.go](../agents/inbox_triage.go)
- **Capabilities:** `inbox_routing`
- **Input types:** `inbox_message`
- **Output types:** `refill_request`, `prior_auth_request`, `inbox_routed`
- **Shared state:** `*tasks.Queue` (for `urgent_clinical` items)
- **Behaviour:** Routes inbound patient messages into `refill | prior_auth | urgent_clinical | general`. Conservative: prefers `urgent_clinical` when in doubt. Urgent items create nurse tasks.
- **LLM variant:** `LLMInboxTriage` ([agents/inbox_triage_llm.go](../agents/inbox_triage_llm.go)) classifies and extracts medication / procedure code.
- **HITL gate:** none (already routes urgent items to a human via the queue)

### `refill_manager` — Virtual Office: Refills

- **ID:** `refill_manager` · **Source:** [agents/refill_manager.go](../agents/refill_manager.go)
- **Capabilities:** `refill_management`
- **Input types:** `refill_request`
- **Output types:** `refill_response`
- **Shared state:** `*medical.PatientRegistry`, `*tasks.Queue`
- **Behaviour:** Auto-approves refills for on-plan medications (medication appears in patient's `CarePlan.Medications`). Otherwise escalates to nurse task.
- **HITL gate:** when `HITL=true`, gates `refill_response` → `engagement` via `refill_review`.

### `prior_auth` — Virtual Office: Prior authorization

- **ID:** `prior_auth` · **Source:** [agents/prior_auth.go](../agents/prior_auth.go)
- **Capabilities:** `prior_auth`
- **Input types:** `prior_auth_request`
- **Output types:** `prior_auth_status`
- **Shared state:** `*tasks.Queue`
- **Behaviour:** Auto-approves low-risk procedures (defined inline; e.g. routine imaging). Others queue as nurse task.
- **HITL gate:** when `HITL=true`, gates `prior_auth_status` → `engagement` via `prior_auth_review`.

### `engagement` — Patient outreach

- **ID:** `engagement` · **Source:** [agents/engagement.go](../agents/engagement.go)
- **Capabilities:** `patient_engagement`
- **Input types:** `care_plan_ready`, `engagement_request`, `tcm_tasks_created`, `refill_response`
- **Output types:** `engagement_sent`
- **Shared state:** `engagement.Notifier`
- **Behaviour:** Routes outbound communication: SMS / email / IVR depending on `Notifier` configuration. Templates: `care_gap`, `welcome`, `refill_status`, `pa_status`, `appointment_reminder`. Tracks adherence (ack rate).
- **HITL gate:** none directly (other agents gate before reaching engagement when `HITL=true`)

### `readmission_tracker` — ADT + readmission risk

- **ID:** `readmission_tracker` · **Source:** [agents/readmission_tracker.go](../agents/readmission_tracker.go)
- **Capabilities:** `readmission_tracking`
- **Input types:** `adt_event`
- **Output types:** `readmission_risk`, `discharge_event`
- **Shared state:** `*medical.PatientRegistry`
- **Behaviour:** On discharge ADT events, computes `medical.ScoreReadmissionRisk` (transparent rule-based LACE-inspired score: age, condition burden, recent discharge, BP, A1c) and publishes `readmission_risk` plus `discharge_event` to trigger TCM.
- **HITL gate:** when `HITL=true`, gates the `discharge_event` → `tcm_coordinator` handoff via `discharge_review`. Priority scales with risk band (high → `urgent` SLA).

---

## HITL

### `human_review`

- **ID:** `human_review` · **Source:** [agents/human_review.go](../agents/human_review.go)
- **Capabilities:** `hitl_review`
- **Input types:** `review_request`
- **Output types:** the resumed message (after a reviewer approves via HTTP / API call)
- **Shared state:** `*hitl.Queue`
- **Behaviour:** Records a `medical.ReviewRequest` in the queue and emits **nothing**. A reviewer decides via `POST /reviews/{id}/decide` or programmatic `HumanReviewAgent.ApplyDecision(...)`. On approval, the original payload (carried in the envelope) is republished to its intended recipient. On reject, logged as a `KindReviewDecision` audit event and dropped.
- **Failure modes:** Lossless on reject. SLA expiry (when `hitl_sla_minutes` is set) never resumes — the review stays in the queue with `status=expired` so reviewers can still see and clear it.

See [architecture.md § Human-in-the-loop pattern](architecture.md#human-in-the-loop-pattern) for the full envelope spec.

---

## Capability index

| Capability | Agents |
|---|---|
| `intake_presentation` | `intake` |
| `rpm_ingest`, `vitals_triage` | `rpm_monitor` |
| `supervise_diagnosis`, `sequential_diagnosis` | `diagnostic_supervisor` |
| `ask_question`, `history_taking` | `questioner` |
| `order_test`, `interpret_results` | `test_planner` |
| `cost_check`, `pareto_optimization` | `cost_guardian` |
| `provide_diagnosis`, `differential_diagnosis` | `diagnostician`, LLM variants |
| `verify_reasoning`, `safety_check` | `reasoning_verifier` |
| `cdm_plan` | `cdm_planner` |
| `tcm_coordination` | `tcm_coordinator` |
| `panel_management`, `gap_in_care` | `panel_manager` |
| `pre_visit_prep` | `pre_visit_prep` |
| `inbox_routing` | `inbox_triage` |
| `refill_management` | `refill_manager` |
| `prior_auth` | `prior_auth` |
| `patient_engagement` | `engagement` |
| `readmission_tracking` | `readmission_tracker` |
| `hitl_review` | `human_review` |

---

## Shared state injected at construction

| State | Type | Agents that touch it |
|---|---|---|
| `*medical.PatientRegistry` | struct | `cdm_planner`, `tcm_coordinator`, `panel_manager`, `pre_visit_prep`, `refill_manager`, `readmission_tracker` |
| `*tasks.Queue` | struct | `tcm_coordinator` (write), `inbox_triage` (write), `refill_manager` (write), `prior_auth` (write); HTTP `GET /tasks` reads |
| `engagement.Notifier` | interface | `engagement` (write); HTTP `GET /notifications` reads |
| `*hitl.Queue` | struct | `human_review` (read/write); other agents do NOT touch directly |
| `llm.Client` | interface | every `LLM*` variant |
| `embeddings.Embedder` | interface | RAG-enabled diagnosticians |
| `*knowledge.HybridStore` | struct | RAG-enabled diagnosticians |

In production, swap the in-memory implementations for Postgres-backed adapters (`*pkg/persistence/postgres.PatientRegistryAdapter`). Agent code does not change.
