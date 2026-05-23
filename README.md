# Bodh — Medical Diagnostic Orchestrator & Care Services Platform (Go)

> **Bodh** (Sanskrit: *bodhi* — knowledge, awakening, insight) is an open-source **medical AI platform** implemented in **Go**. It follows Microsoft's [**Multi-Agent Reference Architecture (MARA)**](https://microsoft.github.io/multi-agent-reference-architecture/), implements a **virtual physician panel** inspired by [**MAI-DxO**](https://microsoft.ai/news/the-path-to-medical-superintelligence/) and the [**Sequential Diagnosis Benchmark (SD Bench)**](https://microsoft.ai/news/the-path-to-medical-superintelligence/), and adds a **care-services layer** (CDM, TCM, panel management, virtual office, RPM, engagement, readmission tracking) on the same bus.

Go · MARA · Medical AI · Sequential Diagnosis · FHIR · HL7 v2 · HIPAA-aware · Open Source

**Module path:** `github.com/PratikDhanave/bodh`

**Architecture diagrams:** [Architecture.png](Architecture.png) (MARA platform) · [architectureflow.png](architectureflow.png) (sequential diagnosis flow) · [docs/diagrams.md](docs/diagrams.md) (25 mermaid views)

---

## ⚠️ Research disclaimer (read first)

**Bodh is a research and engineering reference. It is not approved for clinical use.**

- Do **not** use Bodh for real patient care, triage, treatment, or billing.
- Demo agents use **simulated** answers and **rule-based** inference — not validated models.
- All thresholds (test costs, RPM alert limits, A1c/BP cutoffs, readmission weights) are **illustrative**, not clinical guidelines.
- HIPAA, FDA, ONC, and state-law alignment described below is **architectural guidance**: the codebase is not certified, audited, or under a Business Associate Agreement (BAA).
- Microsoft's MAI-DxO/SD-Bench work itself requires clinical validation, governance, and regulatory review before deployment.
- At the heart of any future clinical product: **safety, trust, transparency, and human oversight.**

---

## What is Bodh?

Bodh is two things in one repository:

1. **A multi-agent diagnostic orchestrator** — a virtual physician panel that asks targeted questions, orders cost-aware tests, infers a working diagnosis, and verifies its reasoning. Inspired by Microsoft's [MAI-DxO](https://microsoft.ai/news/the-path-to-medical-superintelligence/) and benchmarked against the Sequential Diagnosis Benchmark.

2. **A care-services platform** — chronic disease management, transitional care, panel management, virtual office (pre-visit prep, inbox triage, refills, prior auth), patient engagement, and readmission tracking. These run as additional agents on the same MARA bus.

The platform layer (orchestrator, registry, bus, governance, evaluation, observability) follows Microsoft's [Multi-Agent Reference Architecture (MARA)](https://microsoft.github.io/multi-agent-reference-architecture/) — agents do not call each other directly; they emit messages routed by the orchestrator and gated by governance policies.

### Engineering reality

Bodh is a **Go codebase** with rule-based agents and an in-memory bus. It is intentionally:

- **Model-agnostic** — every agent is a struct that implements one interface; an LLM-backed version drops in by composition.
- **Cost-aware** — every diagnostic test has a virtual USD price; a Cost Guardian enforces a per-case budget.
- **Governed** — policies run *before* agents see a message; deny stops processing.
- **Auditable** — every interaction is a `Message`; the orchestrator is the only place that touches the bus + policy + agents.

---

## The one-sentence model

> A user (or API call) publishes a clinical message → the orchestrator delivers it to the right agent → the agent returns zero or more new messages → the orchestrator publishes those → governance gates each hop → the workflow continues until a terminal agent decides we're done.

That is exactly what MARA's building blocks describe: an **orchestrator**, **specialized agents**, a **registry**, **memory**, **governance**, and **observability**.

---

## Table of contents

1. [Quick start](#quick-start)
2. [Architecture at a glance](#architecture-at-a-glance)
3. [Repository layout](#repository-layout)
4. [Background: MAI-DxO and SD Bench](#background-mai-dxo-and-sd-bench)
5. [Why MARA (and not one big LLM call)](#why-mara-and-not-one-big-llm-call)
6. [Care-services catalog](#care-services-catalog)
7. [MARA concepts → Go packages](#mara-concepts--go-packages)
8. [Virtual physician panel](#virtual-physician-panel-mai-dxo-mapping)
9. [Human-in-the-loop (HITL) gate](#human-in-the-loop-hitl--clinical-oversight-gate)
10. [Care-services workflows](#care-services-workflows)
11. [Clinical message protocol](#clinical-message-protocol)
12. [Domain model and package guide](#domain-model-pkgmedical)
13. [Walkthroughs](#walkthrough-cmddemo)
14. [Healthcare standards](#healthcare-standards--interoperability) · [Security & HIPAA compliance](#security--hipaa-compliance) · [Regulatory pathway](#regulatory-pathway)
15. [Operations](#operations) · [Quality & evaluation](#quality--evaluation) · [Develop](#develop)
16. [Reference](#reference)

<details>
<summary>Full section index (71 anchors)</summary>

- Quick start · Architecture at a glance · Repository layout
- Background: MAI-DxO and SD Bench · Why MARA · Care-services catalog
- MARA concepts → Go packages · Virtual physician panel · HITL gate
- Care-services workflows · Clinical message protocol · Domain model
- Package guide · Walkthrough: cmd/demo · Walkthrough: cmd/caredemo
- Healthcare standards & interoperability (see [docs/healthcare-standards.md](docs/healthcare-standards.md))
- Security & HIPAA compliance (see [docs/compliance.md](docs/compliance.md))
- Regulatory pathway · Observability · Audit logging · Configuration
- Deployment topologies · Docker / Kubernetes · Backup / DR · Scaling
- Evaluation (SD Bench) · Test catalog · Sample cases · Cohort analytics
- Add your first agent · Design rules · Testing · Contributing
- HTTP API · Message types · Agent reference · Implemented vs planned
- Roadmap · MARA chapter mapping · Glossary · References · License

</details>

---

## Quick start

```bash
git clone https://github.com/PratikDhanave/bodh.git
cd bodh

# Diagnostic flow (MAI-DxO / SD-Bench style)
go run ./cmd/demo
go run ./cmd/demo -case=data/cases/demo_chf.json   # CHF + RPM

# Care services (CDM, TCM, panel, virtual office, ADT)
go run ./cmd/caredemo
go run ./cmd/caredemo -workflow=cdm
go run ./cmd/caredemo -workflow=tcm
go run ./cmd/caredemo -workflow=panel
go run ./cmd/caredemo -workflow=virtual_office
go run ./cmd/caredemo -workflow=adt

# HTTP API (RPM ingest, ADT, CDM, TCM, Panel, Inbox, Refill, Prior auth, FHIR)
go run ./cmd/care -addr=:8088

# SD-Bench runner — loads every case fixture, prints accuracy/cost table
go run ./cmd/bench

# Operator CLI — manage reviews / tasks / notifications against a running cmd/care
go install ./cmd/bodhctl
bodhctl reviews pending
bodhctl reviews approve <id>

# Common dev workflow via Makefile
make all          # vet + test + build
make ci           # vet + test + govulncheck
make demo         # diagnostic flow
make caredemo     # care workflows (HITL on)
make care         # HTTP API on :8088
make bench        # SD-Bench
make docker       # container build

# Test suite
go test ./...
```

You should see logs like:

```
[bodh] [intake] case=demo-case-001 chief_complaint="Cough and fever"
[bodh] [supervisor] starting sequential diagnosis for case demo-case-001
[bodh] [test_planner] ordering complete_blood_count ($45)
[bodh] [cost_guardian] APPROVE: $220.00 / $500.00 budget (44% utilized)
[bodh] [diagnostician] diagnosis="Community-acquired pneumonia" confidence=87%
[bodh] [reasoning_verifier] VERIFIED diagnosis "Community-acquired pneumonia"
[bodh] [supervisor] gold standard: Community-acquired pneumonia | match=true
```

For the care demo:

```
[bodh] [cdm_planner] plan condition=dm goals=1 meds=1 monitors=1 next_review=...
[bodh] [tcm_coordinator] discharge pt-001 primary=ADHF tasks=5 conflicts=0
[bodh] [panel_manager] panel=3 gaps=4 roll=map[chf:1 copd:1 dm:1 htn:1]
[bodh] [readmission_tracker] patient=pt-001 discharge risk=0.66 band=high
[bodh] [engagement] sms -> pt-002 template=care_gap reason=a1c_overdue
[bodh] === summary === nurse tasks: 12 — notifications sent: 8
```

---

## Architecture at a glance

Two canonical diagrams ship in the repo root. Start here.

### Platform architecture — [`Architecture.png`](Architecture.png)

The Microsoft-style **Multi-Agent Reference Architecture (MARA)** mapping — user, application, orchestrator, classifier, registry, knowledge/storage, supervisor/worker agents, MCP, observability, evaluation.

<p align="center">
  <img src="./Architecture.png" alt="Bodh platform architecture aligned with Microsoft MARA" width="820"/>
</p>

### Clinical flow — [`architectureflow.png`](architectureflow.png)

End-to-end **sequential diagnosis** for one case: presentation → RPM monitor → history → tests (with cost check + rework loop) → diagnosis → reasoning verification → human sign-off → final report.

<p align="center">
  <img src="./architectureflow.png" alt="Sequential diagnosis flow — virtual physician panel" width="820"/>
</p>

### How each box maps to the Go code

| Area on diagram | Bodh implementation | Go path |
|-----------------|---------------------|---------|
| User / User application | Entry that publishes the first `protocol.Message` | `cmd/demo`, `cmd/caredemo`, `cmd/care` (HTTP), `cmd/bodhctl` (CLI) |
| Orchestrator | Subscribes agents, runs governance, calls `HandleMessage`, republishes outputs | `pkg/orchestration` |
| Classifier | Route intent (chest pain vs rash, refill vs urgent) | `inbox_triage` agent · future `pkg/classifier` |
| Agent registry | Lists agents, resolves capabilities | `pkg/registry` |
| Knowledge layer | Guidelines, differentials, RAG | Planned: DB + vector index |
| Storage layer | Case + roster + queue + reviews + audit | `pkg/memory`, `pkg/persistence` (JSON file), `pkg/audit` (append-only) · planned PostgreSQL |
| Supervisor agent | Decompose diagnosis | `agents/supervisor.go` |
| Workers | Intake, questioner, tests, cost, dx, verify, CDM/TCM/panel/VO/RPM/engagement/readmission/human-review | `agents/*.go` (17 agents + HITL gate) |
| Integration / MCP / tools | Labs, imaging, LLM APIs, FHIR, HL7 v2 | `pkg/fhir` (today) · planned MCP tools |
| Governance | Allow/deny boundary; PHI redaction | `pkg/governance`, `pkg/phi` |
| Observability | Logs, traces, clocks, audit | `pkg/observability`, `pkg/audit` |
| Evaluation | Accuracy vs gold, cost metrics | `pkg/eval` + `cmd/bench` |

> Detailed control-plane, state-machine, and deployment diagrams live in [docs/diagrams.md](docs/diagrams.md) — 25 mermaid views, grouped A–F by topic (bird's-eye, control plane, state machines, diagnostic flow, care-services flows, deployment).

---

## Repository layout

```
bodh/
├── Architecture.png             # MARA platform map
├── architectureflow.png         # Sequential diagnosis flow
├── architectureflow.webp        # WebP source for the flow diagram
├── docs/
│   ├── diagrams.md              # 25 mermaid architecture diagrams
│   ├── healthcare-standards.md  # FHIR, HL7v2, SMART, X12, NCPDP, ...
│   └── compliance.md            # HIPAA, HITECH, GDPR, governance policies
├── cmd/
│   ├── demo/                    # Sequential diagnosis entrypoint
│   ├── caredemo/                # Care-services workflows
│   ├── care/                    # HTTP API
│   ├── bench/                   # SD-Bench fixture runner
│   └── bodhctl/                 # Operator CLI
├── agents/                      # Virtual physician panel + care services
│   ├── intake.go
│   ├── supervisor.go            # diagnostic_supervisor (MAI-DxO-style)
│   ├── questioner.go
│   ├── test_planner.go
│   ├── cost_guardian.go
│   ├── diagnostician.go
│   ├── reasoning_verifier.go
│   ├── rpm_monitor.go
│   ├── cdm_planner.go           # Chronic Disease Management
│   ├── tcm_coordinator.go       # Transitional Care Management
│   ├── panel_manager.go         # Panel Management / gap-in-care
│   ├── pre_visit_prep.go        # Virtual Office: pre-visit prep
│   ├── inbox_triage.go          # Virtual Office: inbox
│   ├── refill_manager.go        # Virtual Office: refills
│   ├── prior_auth.go            # Virtual Office: prior auth
│   ├── engagement.go            # Patient outreach
│   └── readmission_tracker.go   # ADT + risk
├── pkg/
│   ├── protocol/                # Message schema (shared wire format)
│   ├── agent/                   # Agent + Environment interfaces
│   ├── registry/                # Agent discovery
│   ├── comm/                    # Bus (in-memory; swappable)
│   ├── orchestration/           # Platform orchestrator
│   ├── governance/              # Policies + medical_policy.go
│   ├── medical/                 # CaseState, CarePlan, Panel, ADT, RPM, ...
│   ├── tasks/                   # Nurse work queue
│   ├── engagement/              # SMS/email/IVR notifier
│   ├── fhir/                    # Minimal FHIR R4 adapter
│   ├── analytics/               # Cohort metrics
│   ├── persistence/             # JSON file store
│   ├── memory/                  # KV store
│   ├── observability/           # Logger + Clock
│   ├── eval/                    # InteractionRecord, CaseMetrics
│   ├── hitl/                    # Human-in-the-loop review queue
│   ├── phi/                     # PHI redaction
│   └── audit/                   # Append-only HIPAA audit log
├── data/
│   └── cases/
│       ├── demo_pneumonia.json
│       └── demo_chf.json
├── go.mod
├── go.sum
└── README.md
```

---

## Background: MAI-DxO and SD Bench

For years, **USMLE-style** multiple-choice questions were the standard AI medical benchmark. Frontier models now score near-perfect on these exams. Microsoft and others argue the benchmarks **overstate** competence — they reward memorization over **iterative clinical reasoning**.

The [Sequential Diagnosis Benchmark](https://microsoft.ai/news/the-path-to-medical-superintelligence/) replaces them with stepwise encounters:

- **304** recent **New England Journal of Medicine (NEJM)** Case Records, turned into stepwise diagnostic encounters.
- An agent (or physician) may **ask questions**, **order tests** (with virtual **cost**), and **update reasoning** until it provides a final diagnosis.
- Performance is measured on **accuracy** and **resource expenditure** — a **Pareto frontier** of diagnosis vs. cost.

MAI-DxO paired with OpenAI's **o3** reportedly reached **~85.5%** accuracy on these cases, vs. **~20%** mean for experienced physicians in their study setup. Microsoft cites up to ~25% of U.S. health spending as low-value or wasted — the cost dimension is real.

### What Bodh implements today

| SD Bench / MAI-DxO idea | Bodh status |
|-------------------------|-------------|
| Sequential ask → test → diagnose | Done — demo workflow |
| Virtual cost per test | Done — `medical.TestCatalog` |
| Cost budget / guardian | Done — `cost_guardian` + `MaxDiagnosticCostPolicy` |
| Self-verification before final diagnosis | Done — `reasoning_verifier` |
| Virtual physician panel | Done — 8 agents |
| NEJM case loader / 304 cases | Planned |
| LLM-backed agents | Planned |
| Public benchmark runner | Planned |

---

## Why MARA (and not one big LLM call)

Microsoft's [Multi-Agent Reference Architecture](https://microsoft.github.io/multi-agent-reference-architecture/) focuses on systems where **many specialized agents** interact. The hard problems are not "write a prompt", but:

| Problem | Why it matters in medicine |
|---------|----------------------------|
| **Routing** | Should this message go to history, labs, or diagnosis? |
| **Decomposition** | Who owns step 1 vs step 2 in a long case? |
| **Governance** | PHI, budget caps, disallowed actions — **before** any agent runs |
| **State** | Case progression, prior questions, test results |
| **Operations** | Audit logs, evaluation vs. gold diagnosis, regression tests |
| **Safety** | Fail closed; explainable denials; human-in-the-loop hooks |

A single monolithic LLM call is hard to **audit**, **cap cost**, **swap models per step**, or **regression-test** when clinical guidelines change. Bodh keeps the orchestrator generic and puts clinical policy in **agents + governance**.

**Further reading:** [MARA Reference Architecture](https://microsoft.github.io/multi-agent-reference-architecture/docs/reference-architecture/Reference-Architecture.html) · [Building blocks](https://microsoft.github.io/multi-agent-reference-architecture/docs/building-blocks/Building-Blocks.html)

---

## Care-services catalog

Each service is one or more Bodh agents subscribing to specific message types, sharing in-memory state (patient registry, nurse task queue, notifier) injected through agent struct fields. The catalog covers the core workflows of value-based primary care.

| Service | Bodh agent(s) | Status |
|------------------------|---------------|--------|
| Remote Patient Monitoring (RPM) | `rpm_monitor` + device webhooks via `pkg/medical/rpm.go` | Done |
| Chronic Disease Management (CDM) | `cdm_planner` + `medical.CarePlan` templates (HTN, DM, CHF, CKD, COPD) | Done |
| Transitional Care Management (TCM) | `tcm_coordinator` + nurse `tasks.Queue` + `medical.ReconcileMedications` | Done |
| Panel Management | `panel_manager` + `DefaultPanelRules` (A1c, BP, CHF weight, annual visit, post-discharge) | Done |
| Virtual Office: Pre-visit prep | `pre_visit_prep` | Done |
| Virtual Office: Inbox | `inbox_triage` (auto-routes refill / PA / urgent) | Done |
| Virtual Office: Refills | `refill_manager` (auto-approves on-plan meds, escalates rest) | Done |
| Virtual Office: Prior auth | `prior_auth` (low-risk auto-approve, others to queue) | Done |
| Patient engagement | `engagement` + `pkg/engagement` (SMS / email / IVR) | Done |
| Readmission tracking | `readmission_tracker` (ADT feed + transparent risk score) | Done |
| EHR integration | `pkg/fhir` (Patient / Observation / Encounter / Condition) | FHIR done · HL7 v2 planned |
| Analytics / outcomes | `pkg/eval` + `pkg/analytics` | Done |

---

## MARA concepts → Go packages

This table is the **map of the codebase**.

| MARA concept | What it does | Go package | Key types / files |
|--------------|--------------|------------|-------------------|
| Message / protocol | Common wire format | `pkg/protocol` | `Message`, `MessageRole`, `NewMessage` |
| Agent | Domain worker — one message in, zero-N out | `pkg/agent` | `Agent`, `Environment` |
| Agent registry | Who exists? What can they do? | `pkg/registry` | `Registry`, `InMemoryRegistry`, `FindByCapability` |
| Communication bus | Routes by `Message.To` | `pkg/comm` | `Bus`, `InMemoryBus` |
| Orchestrator | Subscribe agents, apply policy, dispatch | `pkg/orchestration` | `Orchestrator`, `SimpleEnvironment` |
| Governance | Allow / deny before an agent sees a message | `pkg/governance` | `Policy`, `CompositePolicy`, `medical_policy.go` |
| Memory | Short / long-term context | `pkg/memory` | `KeyValueStore`, `InMemoryKV` |
| Observability | Logs, clocks | `pkg/observability` | `Logger`, `Clock`, `[bodh]` prefix |
| Evaluation | Record runs for offline / online eval | `pkg/eval` | `InteractionRecord`, `CaseMetrics`, `Store` |
| Medical domain | Case state, tests, diagnoses, care plans, panel, ADT, RPM | `pkg/medical` | `CaseState`, `TestCatalog`, `DiagnosisProposal`, `CarePlan`, `PanelReport`, `ADTEvent`, `RPMObservation`, `PatientRegistry` |
| Clinical agents | Virtual physician panel + care services | `agents/` | 17 agents |
| Tasks | Nurse work queue | `pkg/tasks` | `Queue`, `NurseTask` |
| Engagement | Outreach notifier | `pkg/engagement` | `Notifier`, `InMemoryNotifier` |
| FHIR adapter | R4 import/export | `pkg/fhir` | `Patient`, `Observation`, `Encounter`, `Condition` |
| Analytics | Cohort rollups | `pkg/analytics` | `CohortMetrics`, `GapBreakdown` |
| Persistence | JSON file store | `pkg/persistence` | `FileStore` |

---

## Virtual physician panel (MAI-DxO mapping)

### Agent registry table

| Agent ID | Human name | MAI-DxO action | Capability strings | Input `Type` | Output `Type` | Sends to |
|----------|------------|----------------|-------------------|--------------|---------------|----------|
| `intake` | Clinical Intake | (case entry) | `intake_presentation` | `presentation` | `case_state` | `rpm_monitor` |
| `rpm_monitor` | RPM Monitor | RPM triage | `rpm_ingest`, `vitals_triage` | `case_state` | `case_state` | `diagnostic_supervisor` |
| `diagnostic_supervisor` | Diagnostic Orchestrator | Panel coordination | `supervise_diagnosis`, `sequential_diagnosis` | `case_state`, `questions_complete`, `tests_complete`, `diagnosis` | `case_state` / terminal | `questioner`, `test_planner`, `diagnostician` |
| `questioner` | History & Symptom Questioner | **Ask question** | `ask_question`, `history_taking` | `case_state` | `questions_complete` | `diagnostic_supervisor` |
| `test_planner` | Diagnostic Test Planner | **Request test** | `order_test`, `interpret_results` | `case_state` | `case_state` | `cost_guardian` |
| `cost_guardian` | Diagnostic Cost Guardian | **Cost check** | `cost_check`, `pareto_optimization` | `case_state` | `tests_complete` | `diagnostic_supervisor` |
| `diagnostician` | Diagnostician | **Provide diagnosis** | `provide_diagnosis`, `differential_diagnosis` | `case_state` | `diagnosis_proposal` | `reasoning_verifier` |
| `reasoning_verifier` | Clinical Reasoning Verifier | **Verify reasoning** | `verify_reasoning`, `safety_check` | `diagnosis_proposal` | `diagnosis` | `diagnostic_supervisor` |

### Per-agent behavior

**`intake`** ([agents/intake.go](agents/intake.go)). Parses `PatientPresentation` JSON, builds the initial `CaseState`, merges complaint and HPI, reads `case_id`/`max_cost_usd`/`gold_diagnosis`/`condition`/RPM observations from metadata. Initializes empty `Answers` and `TestResults` maps.

**`rpm_monitor`** ([agents/rpm_monitor.go](agents/rpm_monitor.go)). Evaluates `medical.EvaluateRPMAlerts` over `state.RPMObservations`. Demo thresholds: SBP ≥ 160, weight_lb ≥ 185, SpO₂ < 92, HR ≥ 110. Always forwards to supervisor — alerts are advisory, not gating.

**`diagnostic_supervisor`** ([agents/supervisor.go](agents/supervisor.go)). State machine on `msg.Type`:

| Incoming | Action |
|----------|--------|
| `case_state` (from RPM/intake) | forward to `questioner` |
| `questions_complete` | forward to `test_planner` |
| `tests_complete` | forward to `diagnostician` |
| `diagnosis` (verified) | finalize — log, score via `eval.ScoreCase` |

**`questioner`** ([agents/questioner.go](agents/questioner.go)). Demo: four fixed history questions; `simulatedAnswer()` returns canned text. Hypotheses keyed off `state.Condition` (chf → ADHF / COPD-exacerbation / PNA; else CAP / viral URI / PE). Production: one question per turn, or a batched LLM call.

**`test_planner`** ([agents/test_planner.go](agents/test_planner.go)). Demo workup for respiratory case: CBC + chest X-ray + BMP. Accumulates `TotalCostUSD` from `medical.TestCatalog`. Always routes through `cost_guardian` before supervisor sees `tests_complete`.

**`cost_guardian`** ([agents/cost_guardian.go](agents/cost_guardian.go)). Compares `state.TotalCostUSD` to `state.MaxCostUSD`. Deny: log and emit no messages (supervisor handles rework). Approve: emits `tests_complete`.

**`diagnostician`** ([agents/diagnostician.go](agents/diagnostician.go)). `inferDiagnosis()`:
- `Condition=chf` + ≥2 RPM alerts → ADHF @ 0.84
- CXR infiltrate + CBC WBC → CAP @ 0.87
- else low-confidence "undifferentiated"

Sends `diagnosis_proposal` JSON to `reasoning_verifier`.

**`reasoning_verifier`** ([agents/reasoning_verifier.go](agents/reasoning_verifier.go)). Demo rule: `confidence ≥ 0.5` AND non-empty `rationale`. On success: sets `WorkingDiagnosis`, emits `diagnosis` to supervisor. Production: LLM rubric, second model, or rules + human escalation.

---

## Human-in-the-loop (HITL) — clinical oversight gate

Every clinically consequential action in Bodh can be **gated by a human reviewer** before the platform acts on it. This is the FDA Clinical Decision Support (CDS) carve-out criterion #4 ("enable the clinician to independently review the basis for the recommendations") expressed in code: the platform proposes, a clinician disposes.

### How it works

A single `human_review` agent ([agents/human_review.go](agents/human_review.go)) is the gate. Upstream agents that need oversight don't address their output directly to the next agent — they wrap it in a **review envelope** and address it to `human_review` with type `review_request`. The gate records a `medical.ReviewRequest` in the [pkg/hitl](pkg/hitl/queue.go) queue and emits nothing. A reviewer decides via HTTP (`POST /reviews/{id}/decide`) or programmatic call (`HumanReviewAgent.ApplyDecision`); on approval the original payload is republished to its intended recipient.

```
upstream agent ──► human_review ──► (pending in queue)
                                        │
                            reviewer decides (HTTP / API)
                                        │
                            ┌───────────┴───────────┐
                            │                       │
                        approve                  reject
                            │                       │
                            ▼                       ▼
                    next agent (resume)      (no resume; logged)
```

The envelope is opaque metadata, so adding HITL to a new flow doesn't require any changes to governance or the bus:

| Envelope key | Type | Required | Purpose |
|--------------|------|----------|---------|
| `hitl_kind` | string | yes | Review category (e.g. `care_plan_review`) |
| `hitl_next_to` | string | yes | Agent to forward to on approval |
| `hitl_next_type` | string | yes | Message type used on resume |
| `hitl_priority` | string | no | `low` / `normal` / `high` / `urgent` (default `normal`) |
| `hitl_subject` | string | no | Short label shown in the queue UI |
| `hitl_sla_minutes` | float64 | no | Deadline; expired reviews never resume |

### Which flows are gated

Every gateable agent carries a `HITL bool` field. Default is `false` (the diagnostic `cmd/demo` is unaffected). `cmd/caredemo` and `cmd/care` set it to `true`.

| Agent | When HITL=true, this output is gated | Review kind |
|-------|--------------------------------------|-------------|
| `reasoning_verifier` | verified diagnosis → `diagnostic_supervisor` | `diagnosis_review` |
| `cdm_planner` | care plan → `engagement` (panel_manager tracking is direct) | `care_plan_review` |
| `tcm_coordinator` | TCM welcome → `engagement` | `tcm_welcome_review` |
| `panel_manager` | per-gap outreach → `engagement` | `outreach_review` |
| `refill_manager` | refill response → `engagement` | `refill_review` |
| `prior_auth` | PA status → `engagement` | `prior_auth_review` |
| `readmission_tracker` | discharge → `tcm_coordinator` (priority scales with risk band) | `discharge_review` |

Flows that don't get a gate (already safe, or trivially informational): RPM ingest, panel_report broadcast, pre-visit chart prep (read-only), inbox triage (already routes urgent items to a human via the nurse task queue), readmission risk broadcast.

### Cascades

Approvals can trigger downstream agents that themselves emit review_requests (e.g. `discharge_review` approved → `tcm_coordinator` runs → emits `tcm_welcome_review`). The `cmd/caredemo` auto-approver drains the queue across rounds to demonstrate this; in production the reviewer's UI shows the cascaded reviews as they appear.

### Two-phase demo

```bash
go run ./cmd/caredemo
# Phase 1: agents publish review_requests → queued as "pending"
# Phase 2 (round 1): clinician-demo auto-approves 9 reviews → resumed messages publish
# Phase 2 (round 2): cascade from TCM publishes one more review_request → approved
# Phase 3: engagement sends 8 SMS notifications
# Summary: 10 human reviews (all approved), 12 nurse tasks, 8 notifications sent
```

Run with HITL off to see the direct (un-gated) behavior:

```bash
go run ./cmd/caredemo -hitl=false
```

### HTTP endpoints (`cmd/care`)

| Method | Path | Body | Description |
|--------|------|------|-------------|
| `GET`  | `/reviews` | — | All reviews. Query `?status=pending` to filter. |
| `GET`  | `/reviews/{id}` | — | Single review |
| `POST` | `/reviews/{id}/decide` | `{reviewer_id, decision, reason, modification}` | `decision` ∈ `approve` / `approve_with_modifications` / `reject` |

### Why this matters

1. **Patient safety.** A clinician sign-off before the platform acts — sends an SMS, finalizes a diagnosis, submits a prior auth, opens a TCM cascade — is the last line of defense.
2. **Regulatory fit.** Under the 21st Century Cures Act CDS carve-out, software that supports a clinician but lets them independently review the basis for each recommendation can avoid SaMD classification.
3. **Auditability.** Every gated action becomes a `ReviewRequest` row with reviewer id, timestamp, decision, reason — the forensic trail HIPAA §164.312(b) and 21 CFR Part 11 both expect.

The gate is **opt-in per agent** and **lossless on reject** (the original message is logged but doesn't republish, so no half-completed clinical action can leak past the gate).

---

## Care-services workflows

Bodh implements a care-services layer as additional agents on the same bus. They share the platform with the diagnostic panel and add their own message types.

| Service | Agent | Input `Type` | Output `Type` |
|---------|-------|--------------|---------------|
| Chronic Disease Management | `cdm_planner` | `cdm_request` | `care_plan_ready` |
| Transitional Care Management | `tcm_coordinator` | `discharge_event` | `tcm_tasks_created` |
| Panel Management / Gap-in-care | `panel_manager` | `panel_request` | `panel_report`, `engagement_request` |
| Pre-visit chart prep | `pre_visit_prep` | `pre_visit_request` | `pre_visit_ready` |
| Inbox triage | `inbox_triage` | `inbox_message` | `refill_request`, `prior_auth_request`, `inbox_routed` |
| Refill management | `refill_manager` | `refill_request` | `refill_response` |
| Prior authorization | `prior_auth` | `prior_auth_request` | `prior_auth_status` |
| Patient engagement | `engagement` | `care_plan_ready`, `engagement_request`, `tcm_tasks_created`, `refill_response` | `engagement_sent` |
| Readmission tracker | `readmission_tracker` | `adt_event` | `readmission_risk`, `discharge_event` |

### Shared state (dependency-injected)

- `medical.PatientRegistry` — population roster (CDM/TCM/panel/refill/readmission read & write)
- `tasks.Queue` — nurse work queue (TCM/inbox/refill/PA write; HTTP `GET /tasks` reads)
- `engagement.Notifier` — SMS / email / IVR sink (engagement writes; HTTP `GET /notifications` reads)

### Care-plan templates

`medical.BuildCarePlan(caseID, patientID, condition, now)` returns a deterministic plan. Condition keys: `htn`, `dm`, `chf`, `ckd`, `copd`. Each plan has:

- **Goals** (SMART-style — metric, target, unit, window)
- **Medications** (name, dose, frequency, class)
- **Monitoring** (metric, cadence, source, thresholds)
- **Education** (patient teaching topics)
- **NextReview** date

### Panel rule set

`medical.DefaultPanelRules()` (see [pkg/medical/panel.go](pkg/medical/panel.go)):

| Rule | Trigger | Severity |
|------|---------|----------|
| `a1c_overdue` | DM patient with no A1c in last 180 d | warn |
| `a1c_uncontrolled` | A1c ≥ 9 | urgent |
| `bp_uncontrolled` | HTN patient SBP ≥ 160 | urgent |
| `bp_elevated` | HTN patient SBP ≥ 140 | warn |
| `annual_visit_due` | No encounter in 365 d | info |
| `chf_weight_alert` | CHF patient weight ≥ 190 lb | urgent |
| `tcm_followup_open` | Discharged 7–30 d ago with no post-discharge visit | urgent |

### Readmission risk

`medical.ScoreReadmissionRisk` is a transparent rule-based (LACE-inspired) score, returning 0..1 and a low/medium/high band. Inputs: age, condition burden, recent discharge, BP, A1c. **Not** an opaque ML model.

### Run the care workflows

```bash
go run ./cmd/caredemo                      # all five workflows
go run ./cmd/caredemo -workflow=cdm
go run ./cmd/caredemo -workflow=tcm
go run ./cmd/caredemo -workflow=panel
go run ./cmd/caredemo -workflow=virtual_office
go run ./cmd/caredemo -workflow=adt
```

---

## Clinical message protocol

All coordination uses `pkg/protocol.Message` (re-exported as `agent.Message`).

### `Message` struct

```go
type Message struct {
    ID        string         // UUID — audit / causal graph
    From      string         // sender agent id or "user"
    To        string         // recipient agent id (routing address)
    Role      MessageRole    // user | system | agent | tool | observer | evaluator
    Type      string         // handler branch
    Content   string         // primary payload (JSON for clinical data)
    CreatedAt time.Time      // UTC
    Metadata  map[string]any // case_id, patient_id, trace_id, costs, ...
}
```

### Message roles

| Role | Use in Bodh |
|------|-------------|
| `user` | Initial `presentation` / API call |
| `agent` | Plans, case state, delegations |
| `tool` | Future: lab feed, imaging AI output |
| `system` | Policy broadcasts, config |
| `observer` | Audit / telemetry events |
| `evaluator` | Automated rubric scores vs gold diagnosis |

### Diagnostic-flow `Type` values

| Type | Direction | Content shape | Governance |
|------|-----------|---------------|------------|
| `presentation` | user → `intake` | `PatientPresentation` JSON | requires `case_id` |
| `case_state` | various | `CaseState` JSON | requires `case_id` |
| `questions_complete` | `questioner` → supervisor | `CaseState` JSON | requires `case_id` |
| `tests_complete` | `cost_guardian` → supervisor | `CaseState` JSON | requires `case_id` |
| `diagnosis_proposal` | `diagnostician` → verifier | `DiagnosisProposal` JSON | requires `case_id` |
| `diagnosis` | verifier → supervisor | `CaseState` JSON (final) | requires `case_id` |
| `order_test` | (future per-test messages) | test JSON | `MaxDiagnosticCostPolicy` applies |
| `ask_question` | (future single-Q turn) | question string | requires `case_id` |
| `verify_reasoning` | (reserved) | rubric payload | requires `case_id` |

### Care-services `Type` values

| Type | Direction | Content shape |
|------|-----------|---------------|
| `cdm_request` | user → `cdm_planner` | `{patient_id, condition, case_id}` |
| `care_plan_ready` | `cdm_planner` → engagement / panel_manager | `CarePlan` JSON |
| `discharge_event` | user/ADT → `tcm_coordinator` | `DischargeSummary` JSON |
| `tcm_tasks_created` | `tcm_coordinator` → engagement | `{patient_id, case_id, task_count}` |
| `panel_request` | user → `panel_manager` | `{condition?}` |
| `panel_report` | `panel_manager` → (broadcast) | `PanelReport` JSON |
| `engagement_request` | `panel_manager` → engagement | `CareGap` JSON |
| `pre_visit_request` | user → `pre_visit_prep` | `{patient_id, encounter_id}` |
| `pre_visit_ready` | `pre_visit_prep` → (broadcast) | summary JSON |
| `inbox_message` | user → `inbox_triage` | `{patient_id, subject, body}` |
| `inbox_routed` | `inbox_triage` → (broadcast) | `{category}` |
| `refill_request` | user/inbox → `refill_manager` | `{patient_id, medication}` |
| `refill_response` | `refill_manager` → engagement | `{status, reason}` |
| `prior_auth_request` | user/inbox → `prior_auth` | `{patient_id, code, payer}` |
| `prior_auth_status` | `prior_auth` → (broadcast) | `{status}` |
| `adt_event` | user → `readmission_tracker` | `ADTEvent` JSON |
| `readmission_risk` | `readmission_tracker` → (broadcast) | `{score, band}` |
| `engagement_sent` | `engagement` → (broadcast) | `{id, channel, template}` |

### Required metadata keys

| Key | Type | Required when | Purpose |
|-----|------|---------------|---------|
| `case_id` | string | All clinical `Type`s | Correlation; **never** log MRNs in clear |
| `patient_id` | string | Care-services types | Population correlation |
| `trace_id` | string | Recommended | Distributed tracing |
| `max_cost_usd` | float64 | Intake / case budget | SD-Bench cost ceiling |
| `gold_diagnosis` | string | Benchmarks only | Offline eval |
| `spent_usd`, `test_cost_usd` | float64 | Test ordering | Per-message budget check |
| `inbox_category` | string | Inbox routing | Triage decision recorded for audit |

### Example: starting a case

```json
{
  "from": "user",
  "to": "intake",
  "role": "user",
  "type": "presentation",
  "content": "{\"chief_complaint\":\"Cough and fever\",\"hpi\":\"58y, 5d productive cough, fever 38.9°C\",\"demographics\":\"58M\",\"vitals\":\"T 38.9, HR 98, RR 22, SpO2 94% RA\"}",
  "metadata": {
    "case_id": "demo-case-001",
    "trace_id": "trace-demo-001",
    "max_cost_usd": 500,
    "gold_diagnosis": "Community-acquired pneumonia"
  }
}
```

---

## Domain model (`pkg/medical`)

### Diagnostic types

| Type | Description |
|------|-------------|
| `PatientPresentation` | chief_complaint, hpi, demographics, vitals, condition, rpm_observations |
| `CaseState` | sequential-diagnosis source of truth — questions, answers, tests, results, hypotheses, working diagnosis, costs, RPM alerts |
| `DiagnosisProposal` | diagnosis, confidence (0–1), differential, rationale |
| `DiagnosticTest` | name, cost_usd, result, ordered |
| `TestCatalog` | `map[string]float64` — 8 demo tests, $35–$850 |
| `RPMObservation` | device, metric, value, unit, taken_at |

### Care-services types

| Type | Description |
|------|-------------|
| `PatientRecord` | Population-health snapshot (conditions, last labs/vitals, meds, RPM-enrolled, discharge date) |
| `PatientRegistry` | Thread-safe roster (Upsert/Get/List/FilterByCondition) |
| `CarePlan` | Goals, medications, monitoring, education, next_review |
| `DischargeSummary` | TCM input — primary_dx, disposition, home/new/stopped meds, follow_up_days, high_risk |
| `MedReconciliation` | Added / Stopped / Kept / Conflict |
| `PanelReport` | panel_size, gaps, condition_roll |
| `CareGap` | patient_id, rule, severity, detail |
| `ADTEvent` | event_id, patient_id, type (admit/discharge/transfer), facility, occurred_at, primary_dx |
| `ReadmissionRisk` | score (0–1), band (low/medium/high) |

### Encoding helpers

Every domain type carries `Encode*` / `Decode*` JSON helpers so payloads move over the bus without sharing concrete types between agents:

```go
medical.EncodePresentation, DecodePresentation
medical.EncodeState,        DecodeState
medical.EncodeCarePlan,     DecodeCarePlan
medical.EncodeDischarge,    DecodeDischarge
medical.EncodeADT,          DecodeADT
```

---

## Package guide

### `pkg/protocol` — the wire format

No import cycles. `pkg/agent` re-exports the types.

### `pkg/agent` — the worker contract

```go
type Agent interface {
    ID() string
    Name() string
    Capabilities() []string
    HandleMessage(ctx context.Context, msg Message, env Environment) ([]Message, error)
}

type Environment interface {
    Now() time.Time
    Logf(format string, args ...any)
}
```

Extend `Environment` via composition (memory, LLM client, FHIR client) rather than bloating the interface.

### `pkg/registry` — discovery

```go
type Registry interface {
    Register(ctx context.Context, a agent.Agent) error
    Get(ctx context.Context, id string) (agent.Agent, error)
    List(ctx context.Context) []agent.Agent
    FindByCapability(ctx context.Context, capability string) []agent.Agent
}
```

### `pkg/comm` — message bus

```go
type Bus interface {
    Subscribe(agentID string, h Handler) (unsubscribe func())
    Publish(ctx context.Context, msg protocol.Message)
}
```

Routing (`InMemoryBus`):
- `msg.To` non-empty → subscribers for that agent ID (async goroutines)
- `agentID == ""` subscription → broadcast

**Production swap:** Kafka, NATS, RabbitMQ, GCP Pub/Sub, AWS EventBridge — each implements `Bus` without touching agent code.

**Demo caveat:** `cmd/demo` and `cmd/caredemo` use a short `time.Sleep` so `main` does not exit before async handlers finish. Use `WaitGroup`, done messages, or context cancellation in production.

### `pkg/orchestration`

```go
type Orchestrator struct { reg; bus; policy; env }

// Start:
//   1) List() agents from registry
//   2) bus.Subscribe(agentID, handler) for each
//   3) On each message:
//        a) policy.Evaluate → deny stops processing
//        b) agent.HandleMessage(...) with the message
//        c) bus.Publish(each output)
```

No clinical logic here — matches MARA's orchestrator role.

### `pkg/governance` — safety at the boundary

```go
type Policy interface {
    Evaluate(ctx context.Context, msg protocol.Message) (PolicyResult, error)
}
```

`CompositePolicy` denies if any child denies (fail fast).

### `pkg/memory`, `pkg/observability`, `pkg/eval`

| Package | Today | Direction |
|---------|-------|-----------|
| `memory` | `InMemoryKV` | per-case scratchpad; conversation STM |
| `observability` | `[bodh]` stdout logger | `trace_id`, OpenTelemetry |
| `eval` | `InteractionRecord` + `CaseMetrics` | SD-Bench accuracy/cost reports |

### `pkg/tasks` — nurse work queue

`Create`, `UpdateStatus`, `List` (sorted by DueAt), `FilterOpen`, `CountByKind`.

### `pkg/engagement` — outreach notifier

Channels: SMS, email, IVR. Tracks acks and computes `AdherenceRate`.

### `pkg/fhir` — minimal FHIR R4 adapter

Patient → `PatientRecord`; Observation → `RPMObservation`; Encounter → `ADTEvent`; Condition → patient condition code.

### `pkg/analytics` — cohort metrics

`SummarizeCases([]eval.InteractionRecord)` and `GapBreakdown(PanelReport)`.

### `pkg/persistence` — JSON file store

Implements both `eval.Store` (records) and `memory.KeyValueStore` (KV) in a single file.

---

## Walkthrough: `cmd/demo`

Open [cmd/demo/main.go](cmd/demo/main.go) alongside this section and [`architectureflow.png`](architectureflow.png).

```go
// 1. observability + environment
logger := observability.NewStdLogger()
env := &orchestration.SimpleEnvironment{Logger: logger, Clock: observability.SystemClock{}}

// 2. platform building blocks
reg := registry.NewInMemory()
bus := comm.NewInMemoryBus()
policy := governance.NewComposite(
    governance.MaxContentLengthPolicy{Max: 65536},
    governance.RequireCaseIDPolicy{},
    governance.MaxDiagnosticCostPolicy{},
    governance.ResearchDisclaimerPolicy{},
)

// 3. register the virtual physician panel
for _, a := range []agent.Agent{
    &agents.IntakeAgent{}, &agents.RPMMonitorAgent{}, &agents.DiagnosticSupervisor{},
    &agents.QuestionerAgent{}, &agents.TestPlannerAgent{}, &agents.CostGuardianAgent{},
    &agents.DiagnosticianAgent{}, &agents.ReasoningVerifierAgent{},
} { _ = reg.Register(ctx, a) }

// 4. start orchestration (subscriptions + governance)
orch := orchestration.NewOrchestrator(reg, bus, policy, env)
orch.Start(ctx)

// 5. publish the initial presentation
start := agent.NewMessage("user", "intake", agent.RoleUser, "presentation",
    medical.EncodePresentation(presentation), meta)
bus.Publish(ctx, start)

// 6. wait for async handlers (production: use a done message)
time.Sleep(3 * time.Second)
```

### Complete message trace (demo-case-001)

| Step | From | To | Type | Notes |
|------|------|-----|------|-------|
| 1 | `user` | `intake` | `presentation` | chief complaint + HPI |
| 2 | `intake` | `rpm_monitor` | `case_state` | no RPM observations, pass-through |
| 3 | `rpm_monitor` | `diagnostic_supervisor` | `case_state` | |
| 4 | `supervisor` | `questioner` | `case_state` | start history |
| 5 | `questioner` | `supervisor` | `questions_complete` | 4 Q&A, 3 hypotheses |
| 6 | `supervisor` | `test_planner` | `case_state` | order labs/imaging |
| 7 | `test_planner` | `cost_guardian` | `case_state` | total $220 |
| 8 | `cost_guardian` | `supervisor` | `tests_complete` | budget OK |
| 9 | `supervisor` | `diagnostician` | `case_state` | infer |
| 10 | `diagnostician` | `reasoning_verifier` | `diagnosis_proposal` | 87% CAP |
| 11 | `reasoning_verifier` | `supervisor` | `diagnosis` | verified |
| 12 | `supervisor` | — | (terminal) | logs final + gold match |

**Total virtual diagnostic cost:** $220 (CBC $45 + CXR $120 + BMP $55).

---

## Walkthrough: `cmd/caredemo`

[cmd/caredemo/main.go](cmd/caredemo/main.go) seeds a `PatientRegistry` with three demo patients (CHF/HTN, DM, COPD) then exercises five workflows:

- **CDM** — publish `cdm_request` for pt-002 (DM) → care plan, SMS welcome
- **TCM** — publish `discharge_event` for pt-001 (high-risk CHF) → 5 nurse tasks, welcome SMS
- **Panel** — publish `panel_request` → run all rules → 4 gaps detected → 4 outreach SMS
- **Virtual office** — pre-visit prep for pt-001, urgent inbox for pt-001, refill for pt-002 (auto-approved), MRI prior auth for pt-003 (queued)
- **ADT** — discharge for pt-001 → readmission risk 0.66 (high) → triggers TCM

Final summary line:

```
[bodh] === summary ===
[bodh] nurse tasks: 12 open / 12 total — kinds=map[inbox:1 prior_auth:1 tcm_follow_up:10]
[bodh] notifications sent: 8
```

---

## Healthcare standards & interoperability

Real-world clinical software must speak the lingua franca of healthcare IT. Bodh tracks the following standards; each section in [docs/healthcare-standards.md](docs/healthcare-standards.md) explains scope, why it matters for Bodh, and current status.

- **FHIR R4** — implemented for `Patient`, `Observation`, `Encounter`, `Condition` ([pkg/fhir](pkg/fhir/)). HTTP endpoints under `/fhir/*`.
- **HL7 v2** — planned `pkg/hl7v2`; ADT, ORM, ORU, SIU, DFT, MDM message types map onto bus messages.
- **SMART on FHIR / OAuth 2.0 / OIDC** — planned `pkg/auth/smart`; EHR launch, scopes, PKCE.
- **FHIR Bulk Data API** — planned `$export` for panel-management ingest.
- **SMART Health Cards / Links** — planned for patient-mediated data sharing.
- **Clinical terminologies** — SNOMED CT, LOINC, ICD-10-CM, CPT, HCPCS, RxNorm, NDC, CVX, UCUM, NUCC. Stored as strings today.
- **DICOM** — planned imaging-specialist agent that consumes DICOM SR via DICOMweb.
- **X12 EDI** — 270/271 (eligibility), 837 (claims), 835 (ERA), 276/277 (status), 278 (prior auth), 999 (ack). Planned.
- **NCPDP SCRIPT** — planned Surescripts integration for refills/NewRx.
- **Direct Project** — planned S/MIME secure clinician messaging.
- **CDA / C-CDA** — planned `pkg/cda` for Discharge Summary export.

Full detail: [docs/healthcare-standards.md](docs/healthcare-standards.md).

---

## Security & HIPAA compliance

> Bodh is a research demo. The architecture below is the **target** for a production HIPAA-aligned deployment. The current codebase does **not** ship with a BAA, encryption-at-rest, identity provider, or audit pipeline.

The deep version of this section lives in [docs/compliance.md](docs/compliance.md). It covers:

- **PHI handling principles** — minimum necessary, opaque identifiers, fail closed, audit everything, no PHI in third-party telemetry, de-identify for evaluation, encrypt in transit and at rest, support right to delete.
- **Security architecture** — IdP → API gateway (TLS 1.3, JWT, WAF) → Bodh API (scope/tenant binding) → orchestrator + bus (trace_id, tenant_id) → persistence (column-level PHI encryption, RLS) → observability and audit sinks.
- **Authn / authz** — OIDC + SMART scopes, mTLS service-to-service, fine-grained governance policies, patient-context binding.
- **Cryptography & key management** — AES-256-GCM with KMS, EdDSA/RSA-PSS signing, `crypto/rand`, CMK in cloud KMS, annual rotation drills.
- **HIPAA Privacy Rule** — uses/disclosures, patient rights, BAA elements, BAA-eligible subcontractors.
- **HIPAA Security Rule** — administrative, physical, and technical safeguards; code-level cross-walk with `pkg/governance`.
- **HIPAA Breach Notification** — 60-day deadlines and the encryption Safe Harbor.
- **HITECH and the Information Blocking Rule** — eight named exceptions; implications for the FHIR adapter.
- **42 CFR Part 2** — substance-use record segmentation.
- **State privacy laws** — CMIA, NY SHIELD, Texas HB 300, BIPA, MHMDA, CCPA/CPRA.
- **GDPR / international privacy** — Art. 5, 9, 15, 17, 22, 25, 32, 33/34, 35; UK GDPR, PIPEDA, LGPD, India DPDP.
- **HITRUST CSF, SOC 2, ISO 27001/27799, NIST SP 800-66, NIST AI RMF** — framework choices.
- **De-identification** — Safe Harbor and Expert Determination methods; re-identification risk drivers.
- **Governance policies in Bodh** — live policies (`MaxContentLengthPolicy`, `RequireCaseIDPolicy`, `MaxDiagnosticCostPolicy`, `ResearchDisclaimerPolicy`) and the planned set (`RequireTenantPolicy`, `RequireScopePolicy`, `RedactNarrativePolicy`, `Part2ConsentPolicy`, `BreakGlassPolicy`, `RateLimitPolicy`).

Run the governance tests:

```bash
go test ./pkg/governance/... -v
go test ./pkg/medical/...     -v
```

---

## Regulatory pathway

### FDA — Software as a Medical Device (SaMD)

The FDA regulates software as a medical device when it is **intended for use in the diagnosis, treatment, mitigation, cure, or prevention of disease.** Sequential-diagnosis tooling like MAI-DxO / Bodh sits squarely in that zone.

| Risk class | Examples | Pathway |
|------------|----------|---------|
| Class I | General wellness, low risk | Largely enforcement discretion |
| Class II | Most clinical decision support | 510(k) premarket notification |
| Class III | Life-supporting / sustaining | PMA — premarket approval |

Track the FDA AI/ML SaMD Action Plan, Predetermined Change Control Plan (PCCP) guidance, and Good Machine Learning Practice (GMLP).

### Clinical Decision Support (CDS) carve-out

Under **section 3060 of the 21st Century Cures Act** (codified at 21 USC § 360j(o)), certain CDS functions are exempt from device classification if they meet all four criteria:

1. **Not** intended to acquire, process, or analyze a medical image or signal.
2. Display, analyze, or print medical information about a patient.
3. Support or provide recommendations to a **health care professional** about prevention, diagnosis, or treatment.
4. Enable the HCP to **independently review** the basis for the recommendations.

Bodh's transparency goals (explanations in `rationale`, rule-based gap detection, score breakdowns) target criterion (4). If an LLM-backed Bodh ever opaques the rationale, criterion (4) is at risk and SaMD classification becomes likely.

### Other regulatory regimes

- **21 CFR Part 11** — electronic records & signatures for FDA-regulated submissions or clinical trials. Bodh's audit-log pattern is compatible; production deployments add cryptographic timestamping (RFC 3161).
- **ONC Health IT Certification** — 2015 Edition Cures Update criteria: USCDI, FHIR R4 with US Core, SMART App Launch, Bulk Data, Information Blocking compliance, patient API access.
- **CMS quality programs** — MIPS, MSSP/ACOs, HRRP (readmission tracker), CCM/PCM/RPM/RTM/TCM (CDM + TCM agents), BPCI-A, REACH.

### Clinical validation pathway

1. **Retrospective validation** — held-out dataset (NEJM SD Bench, MIMIC-IV) vs gold standard.
2. **Silent prospective deployment** — run alongside clinicians without showing outputs.
3. **Shadow review** — clinicians see recommendations, a second clinician reviews Bodh output.
4. **IRB-approved pragmatic trial** — measure outcomes under randomization.
5. **Post-market surveillance** — drift detection, equity audits, adverse-event reporting.

### Bias, equity, and model governance

A clinical AI system can hurt patients via:

| Failure mode | Example | Mitigation |
|--------------|---------|-----------|
| Training data bias | Underrepresented populations get worse triage | Stratified evaluation across age/sex/race/SES |
| Label leakage | `gold_diagnosis` appears in agent input during eval | Strict separation between input and label in benchmarks |
| Spec gaming | Cost-optimizer skips tests that *would* find the disease | Multi-objective eval (accuracy AND cost AND safety events) |
| Distribution shift | Trained on academic-center data, deployed in community | Drift monitor + alert + retraining |
| Automation bias | Clinicians over-trust the recommendation | UI shows confidence + rationale + dissenting hypotheses |
| Cybersecurity attacks | Prompt injection in clinical narrative | Sanitize Content before LLM call; structured outputs |

**Model governance** for a future LLM-backed Bodh: pin per-agent model versions, A/B canary, rollback on metric regression, scrub then log prompts/responses, annual bias audit and public report.

---

## Operations

### Observability

Today: stdout `Logger` with `[bodh]` prefix and microsecond timestamps. Logs every `agent.HandleMessage` entry/exit, every governance decision (`allow` / `deny` with `reason`), and every clinical inference (diagnosis, care plan, gap, risk band).

Production target:

| Signal | Tool |
|--------|------|
| Structured logs | zap / zerolog → Loki / Cloud Logging; redact `Content` before export |
| Metrics | `expvar` → Prometheus; SLI dashboards |
| Traces | OpenTelemetry; `trace_id` already a `Metadata` key |
| Events | CloudEvents on Kafka / NATS |

Recommended SLIs / SLOs:

| SLI | SLO (illustrative) |
|-----|-------------------|
| End-to-end diagnostic case duration p95 | < 60 s |
| RPM ingest → alert publish p99 | < 5 s |
| Engagement deliverability rate | ≥ 98 % |
| Panel-rule run latency for 10k patients | < 30 s |
| Governance policy deny rate (per type) | tracked, alert on spike |
| Refill auto-approve rate | tracked, alert on drop |

### Audit logging

`pkg/orchestration.Orchestrator` logs at four key points: policy evaluation (`allow`/`deny`), agent invocation, agent errors, each emitted output.

Production audit-log requirements for HIPAA / 21 CFR Part 11 / SOC 2:

- **Append-only** — write-once / immutable sink (S3 Object Lock, Azure Immutable Blob, Loki + retention lock).
- **Time-synced** — NTP / chrony required; log `monotonic` source if possible.
- **Tenant-segregated** — `tenant_id` field; cross-tenant query forbidden.
- **PHI-redacted** — narrative removed before persisting beyond the regulated boundary.
- **Tamper-evident** — periodic chain hash (Merkle / hash-linked).
- **Retention** — 6 years for HIPAA; longer for some state laws and trials.

### Configuration

Current entry points read flags only:

| Cmd | Flag | Default |
|-----|------|---------|
| `cmd/demo` | `-case` | (none — uses built-in pneumonia case) |
| `cmd/caredemo` | `-workflow` | `all` |
| `cmd/care` | `-addr` | `:8088` |

Production target: read from environment / file / Kubernetes ConfigMap. Recommended layout:

```
BODH_LISTEN_ADDR=:8088
BODH_TLS_CERT=/etc/bodh/tls.crt
BODH_TLS_KEY=/etc/bodh/tls.key
BODH_OIDC_ISSUER=https://idp.example.com
BODH_OIDC_AUDIENCE=bodh-care
BODH_BUS=nats://nats:4222
BODH_DB_DSN=postgres://...
BODH_LOG_LEVEL=info
BODH_TRACE_ENDPOINT=http://otel-collector:4318
BODH_TENANT_ID=tenant-acme
BODH_MAX_COST_USD=500
```

### Deployment topologies

| Topology | When | Pros | Cons |
|----------|------|------|------|
| **Single binary** (today) | Demo, single-tenant pilot | One process, easy to reason about, low cost | No HA; bus is in-memory; restart loses state |
| **Single binary + Postgres + Kafka** | Small production | HA via replicas, persistent bus, durable state | Operational overhead |
| **Microservices per agent** | Large scale, multi-team | Independent deploy, per-agent scaling, blast-radius limits | Network overhead, distributed tracing burden |
| **Serverless functions** (one per `HandleMessage`) | Bursty workloads | Auto-scale to zero, pay per call | Cold starts; observability harder |

The MARA abstractions make this a deployment choice, not a code rewrite — every interface (`Bus`, `Registry`, `Policy`, `Environment`) has both an in-memory and a swappable shape.

### Docker and Kubernetes

Minimal Dockerfile (planned):

```dockerfile
# Build
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -o /out/care ./cmd/care

# Run (distroless / minimal base)
FROM gcr.io/distroless/static-debian12
COPY --from=build /out/care /care
USER nonroot:nonroot
EXPOSE 8088
ENTRYPOINT ["/care"]
```

Minimal Kubernetes Deployment (planned):

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: bodh-care
spec:
  replicas: 3
  selector: { matchLabels: { app: bodh-care } }
  template:
    metadata: { labels: { app: bodh-care } }
    spec:
      serviceAccountName: bodh-care
      containers:
      - name: care
        image: ghcr.io/PratikDhanave/bodh-care:latest
        readinessProbe:
          httpGet: { path: /healthz, port: 8088 }
        livenessProbe:
          httpGet: { path: /healthz, port: 8088 }
        env:
        - name: BODH_OIDC_ISSUER
          valueFrom: { configMapKeyRef: { name: bodh-config, key: oidcIssuer } }
        resources:
          limits: { cpu: 500m, memory: 512Mi }
        securityContext:
          allowPrivilegeEscalation: false
          readOnlyRootFilesystem: true
          runAsNonRoot: true
          capabilities: { drop: ["ALL"] }
```

**NetworkPolicy** (planned): default-deny, allow ingress only from the API gateway, egress only to OIDC + DB + Kafka.

### Backup, disaster recovery, business continuity

| Concern | Target |
|---------|--------|
| RPO (max acceptable data loss) | 15 min |
| RTO (max acceptable downtime) | 4 h for full service; 30 min for read-only |
| Backup cadence | Continuous WAL + daily snapshot |
| Backup encryption | KMS-managed; cross-region copy |
| Backup retention | 30 days hot, 6 years cold (HIPAA) |
| DR drill | Quarterly tabletop, annual game-day |
| BCP plan | Documented; communication tree; payer + provider notifications |

### Scaling, throughput, and tenancy

- **Bus throughput** — InMemoryBus is per-process. Production bus must be horizontally scalable (NATS JetStream / Kafka).
- **Stateless agents** — agents hold no per-request state; they read from `Environment` / injected stores. Horizontal scaling is trivial.
- **Sticky tenancy** — partition by `tenant_id` at the bus level; per-tenant DB schemas or row-level security in shared schemas.
- **Cell architecture** — for the largest scale, isolate tenant cells with their own bus + DB + monitoring; blast radius is bounded to one cell.

---

## Quality & evaluation

### Evaluation (SD Bench alignment)

`pkg/eval` ([eval.go](pkg/eval/eval.go), [metrics.go](pkg/eval/metrics.go)) provides:

```go
type CaseMetrics struct {
    DiagnosisMatch  bool
    Accuracy        float64
    CostUSD         float64
    CostUtilization float64
    MessageSteps    int
    RPMAlertCount   int
    UnderBudget     bool
}

ScoreCase(state medical.CaseState, messageSteps int) CaseMetrics
```

Each completed case yields a `CaseMetrics` that becomes an `InteractionRecord` for cohort rollup.

Planned `cmd/bench` metrics:

| Metric | Definition |
|--------|------------|
| **Diagnostic accuracy** | `working_diagnosis` matches `gold_diagnosis` (exact or ICD-normalized) |
| **Mean test cost** | `total_cost_usd` averaged over cases |
| **Pareto score** | Accuracy vs cost scatter |
| **Steps to diagnosis** | Messages / questions / tests |
| **Safety events** | Confidence < threshold yet no human escalation |

### Diagnostic test catalog and costs

Defined in [pkg/medical/types.go](pkg/medical/types.go) (illustrative USD, research demo):

| Test key | Virtual cost (USD) | Demo result summary |
|----------|-------------------|---------------------|
| `complete_blood_count` | 45 | WBC 14.2 K, neutrophil predominance |
| `basic_metabolic_panel` | 55 | Within normal limits |
| `chest_xray` | 120 | RLL infiltrate / consolidation |
| `ecg` | 80 | (not used in default demo) |
| `urinalysis` | 35 | — |
| `ct_chest` | 850 | High cost — use when budget allows |
| `blood_culture` | 95 | — |
| `arterial_blood_gas` | 75 | — |

**MAI-DxO insight:** the best agents sit on the **Pareto frontier** — high accuracy, **lower** average test cost than humans who may over-order.

### Sample cases and fixtures

| File | Description |
|------|-------------|
| [`data/cases/demo_pneumonia.json`](data/cases/demo_pneumonia.json) | 58M cough/fever; gold CAP; $500 budget |
| [`data/cases/demo_chf.json`](data/cases/demo_chf.json) | 72F CHF with RPM observations; gold ADHF; $600 budget |

Future layout:

```
data/cases/
├── demo_pneumonia.json
├── demo_chf.json
├── nejm_001.json          # (planned) NEJM-style narrative
└── manifest.json          # (planned) SD Bench index
```

### Cohort analytics

`pkg/analytics.SummarizeCases` aggregates `InteractionRecord`s into: mean accuracy, mean cost USD, under-budget percentage, mean RPM alert count per case. `pkg/analytics.GapBreakdown` turns a `PanelReport` into `{rule: count}` for dashboards.

---

## Develop

### Add your first Bodh agent

**1. Create `agents/radiologist.go`:**

```go
package agents

import (
    "context"
    "github.com/PratikDhanave/bodh/pkg/agent"
    "github.com/PratikDhanave/bodh/pkg/medical"
)

type RadiologistAgent struct{}

func (a *RadiologistAgent) ID() string   { return "radiologist" }
func (a *RadiologistAgent) Name() string { return "Imaging Specialist" }
func (a *RadiologistAgent) Capabilities() []string {
    return []string{"interpret_imaging"}
}

func (a *RadiologistAgent) HandleMessage(ctx context.Context, msg agent.Message, env agent.Environment) ([]agent.Message, error) {
    state, err := medical.DecodeState(msg.Content)
    if err != nil {
        return nil, err
    }
    env.Logf("[radiologist] reviewing %d studies", len(state.TestsOrdered))
    // TODO: LLM or PACS integration
    out := agent.NewMessage(a.ID(), "diagnostic_supervisor", agent.RoleAgent,
        "imaging_complete", medical.EncodeState(state), copyMeta(msg.Metadata))
    return []agent.Message{out}, nil
}
```

**2. Register in `cmd/demo/main.go` (or `cmd/care/main.go`):**

```go
_ = reg.Register(ctx, &agents.RadiologistAgent{})
```

**3. Route from supervisor.** Add a branch in `DiagnosticSupervisor.routeNext` for `imaging_complete`, or delegate only when `ct_chest` appears in `tests_ordered`.

**4. PR checklist:**

- [ ] Table-driven test for `HandleMessage` on each `msg.Type`
- [ ] Capability documented in README agent table
- [ ] No direct agent-to-agent function calls
- [ ] Governance: `case_id`, budget, content length
- [ ] Logs use `env.Logf`; no raw MRN/SSN
- [ ] Domain types live in `pkg/medical`, not in the agent file

### Design rules (from MARA)

1. **Orchestrator coordinates; agents do not call each other.** Use `Message.To`.
2. **One agent = one meaningful clinical capability**, not one FHIR endpoint.
3. **Registry for discovery; bus for delivery.** Do not mix responsibilities.
4. **Governance runs before `HandleMessage`.** Fail closed.
5. **Prefer message-driven flows** for multi-step diagnosis; supervisor agent plans steps.
6. **Version agents and prompts**; rollback when eval metrics drop.
7. **Clinical safety:** outputs are decision **support**, not autonomous care; human attestation required for production.
8. **Cost-aware:** explicit `max_cost_usd`; avoid defaulting to "order everything."
9. **PHI hygiene:** opaque `case_id`; never log full narrative in production info-level logs.
10. **Standards-first:** prefer FHIR / HL7 / SNOMED / LOINC over custom shapes when integrating externally.

### Testing

```bash
go test ./...               # all packages
go test ./pkg/governance/... -v
go test ./pkg/medical/...    -v
go vet ./...
```

Today's coverage spans `pkg/governance`, `pkg/medical`, `pkg/tasks`, `pkg/engagement`, `pkg/fhir`, `pkg/analytics`, `pkg/persistence`, and `pkg/eval`. Planned: table-driven tests for every agent's `HandleMessage`.

### Contributing

- Open issues for bugs, agent ideas, and benchmark integrations.
- **Diagram hygiene:** if a PR changes platform boxes or agent flow, update [`Architecture.png`](Architecture.png), [`architectureflow.png`](architectureflow.png), and the matching mermaid view in [docs/diagrams.md](docs/diagrams.md).
- **Good first tasks:**
  - Loader: read `data/cases/*.json` and publish `presentation` messages from `cmd/bench`
  - Table-driven tests for each agent's `HandleMessage`
  - `docs/messages.md` generated from README clinical types table
  - Stricter `inferDiagnosis` using SNOMED codes
  - HL7 v2 parser in `pkg/hl7v2`
  - OpenTelemetry export from `pkg/observability`
- Follow [Design rules](#design-rules-from-mara) and run `go test ./...` before PRs.

---

## Reference

### HTTP API reference (`cmd/care`)

Start the server: `go run ./cmd/care -addr=:8088`

All endpoints accept and return JSON. **No authentication is enforced today; do not put PHI through this binary.**

#### Patient registry

| Method | Path | Body | Description |
|--------|------|------|-------------|
| `POST` | `/patients` | `medical.PatientRecord` | Upsert into in-memory roster |
| `GET`  | `/patients` | — | List all registered patients |

#### Care services

| Method | Path | Body | Description |
|--------|------|------|-------------|
| `POST` | `/cdm` | `{patient_id,condition,case_id}` | Publish `cdm_request` |
| `POST` | `/tcm/discharge` | `medical.DischargeSummary` | Trigger TCM follow-ups |
| `POST` | `/panel` | `{condition:"all|htn|dm|chf"}` | Run panel rules |
| `POST` | `/pre_visit` | `{patient_id,encounter_id}` | Pre-visit summary |
| `POST` | `/inbox` | `{patient_id,subject,body}` | Triage inbound message |
| `POST` | `/refill` | `{patient_id,medication}` | Refill decision |
| `POST` | `/priorauth` | `{patient_id,code,payer}` | Prior-auth status |

#### Events

| Method | Path | Body | Description |
|--------|------|------|-------------|
| `POST` | `/adt` | `medical.ADTEvent` | Admit / discharge / transfer |
| `POST` | `/rpm/observation` | `medical.RPMObservation` | Single RPM reading → returns alerts |

#### FHIR

| Method | Path | Body | Description |
|--------|------|------|-------------|
| `POST` | `/fhir/Patient` | FHIR R4 Patient | Upsert into registry |
| `POST` | `/fhir/Encounter` | FHIR R4 Encounter | Map to ADT event |
| `POST` | `/fhir/Condition` | FHIR R4 Condition | Append condition code |

#### Read

| Method | Path | Description |
|--------|------|-------------|
| `GET`  | `/tasks` | Nurse work queue (TCM / refills / PAs / inbox) |
| `GET`  | `/notifications` | Outreach log + adherence |

### Message-type reference

See [Clinical message protocol](#clinical-message-protocol) above for the full list — diagnostic flow + care-services flow.

### Agent reference

See [Virtual physician panel](#virtual-physician-panel-mai-dxo-mapping) and [Care-services workflows](#care-services-workflows).

### What is implemented today vs planned

| Layer | Status | Where |
|-------|--------|-------|
| MARA platform | Done | `pkg/protocol`, `pkg/agent`, `pkg/registry`, `pkg/comm`, `pkg/orchestration`, `pkg/governance`, `pkg/observability`, `pkg/memory`, `pkg/eval` |
| Architecture diagrams | Done | `Architecture.png`, `architectureflow.png`, `docs/diagrams.md` |
| Medical domain | Done | `pkg/medical` |
| Medical governance | Done | `pkg/governance/medical_policy.go` |
| Virtual physician panel | Done — rule-based | `agents/*.go` |
| Sequential diagnosis demo | Done | `cmd/demo` |
| Chronic Disease Management | Done | `agents/cdm_planner.go`, `pkg/medical/careplan.go` |
| Transitional Care Management | Done | `agents/tcm_coordinator.go`, `pkg/medical/discharge.go`, `pkg/tasks` |
| Panel Management + care gaps | Done | `agents/panel_manager.go`, `pkg/medical/panel.go` |
| Virtual Office | Done | `pre_visit_prep`, `inbox_triage`, `refill_manager`, `prior_auth` |
| Patient engagement | Done | `agents/engagement.go`, `pkg/engagement` |
| Readmission tracker | Done | `agents/readmission_tracker.go`, `pkg/medical/adt.go` |
| Human-in-the-loop gate | Done | `agents/human_review.go`, `pkg/hitl`, `pkg/medical/review.go` |
| Cost-rework on budget deny | Done | `cost_guardian` → `tests_revise` → `supervisor.reviseTests` |
| PHI redaction (logs) | Done | `pkg/phi` |
| HIPAA audit log | Done | `pkg/audit` (in-memory + file-backed append-only JSON-lines) |
| Multi-tenant + scope policies | Done | `governance.RequireTenantPolicy`, `RequireScopePolicy` |
| SD-Bench runner | Done | `cmd/bench` |
| Operator CLI | Done | `cmd/bodhctl` |
| FHIR R4 adapter | Done — minimal | `pkg/fhir` |
| Cohort analytics | Done | `pkg/analytics` |
| Persistence | Done — JSON file | `pkg/persistence` |
| HTTP API | Done | `cmd/care` |
| Care-services demo | Done | `cmd/caredemo` |
| Project infra | Done | `Makefile`, `Dockerfile`, `.github/workflows/ci.yml` |
| LLM-backed agents | Planned | Per-agent prompts + tool use |
| HL7 v2 adapter | Planned | `pkg/hl7v2` |
| SMART on FHIR auth | Planned | `pkg/auth/smart` |
| FHIR Bulk Data | Planned | `$export` + NDJSON pipeline |
| NEJM / SD Bench loader | Planned | `cmd/bench`, `data/cases/` |
| OpenTelemetry | Planned | `pkg/observability` extension |
| PostgreSQL persistence | Planned | Replace JSON file store |
| Dashboard | Planned | Case timeline UI |
| HIPAA audit pipeline | Planned | Append-only sink + retention |
| 21 CFR Part 11 e-signature | Planned | Required for trial deployments |
| ONC Certification artifacts | Planned | USCDI conformance |

### Roadmap

| Phase | Goal | Deliverables |
|-------|------|--------------|
| 1 (done) | MARA platform + diagnostic panel + RPM/CHF | `cmd/demo`, 8 agents, `pkg/eval`, governance tests |
| 2 (done) | Care services layer | 9 additional agents, `pkg/tasks`/`engagement`/`fhir`/`analytics`/`persistence`, `cmd/caredemo`, `cmd/care` |
| 3 | Authentication + multi-tenancy | SMART on FHIR, `RequireScopePolicy`, tenant isolation |
| 4 | LLM integration | `Environment` with model router; prompt templates per agent; safety harness |
| 5 | SD-Bench evaluation | Case manifest, accuracy + cost report, Pareto plots |
| 6 | EHR integration | HL7 v2, FHIR Bulk Data, ADT pipelines |
| 7 | Production hardening | Postgres persistence, OTel, immutable audit, BAA-eligible deployments |
| 8 | Clinical partnerships | Validation protocol, IRB-approved pilot |

### MARA chapters — what to read and when

| Chapter | Link | When you need it |
|---------|------|------------------|
| Overview | [index](https://microsoft.github.io/multi-agent-reference-architecture/index.html) | Day 1 — vocabulary |
| Building blocks | [Building blocks](https://microsoft.github.io/multi-agent-reference-architecture/docs/building-blocks/Building-Blocks.html) | Before your first agent |
| Design options | [Design options](https://microsoft.github.io/multi-agent-reference-architecture/docs/design-options/Design-Options.html) | Monolith vs services |
| Agent registry | [Agent registry](https://microsoft.github.io/multi-agent-reference-architecture/docs/agent-registry/Agent-Registry.html) | Dynamic routing |
| Memory | [Memory](https://microsoft.github.io/multi-agent-reference-architecture/docs/memory/Memory.html) | Per-case session state |
| Agents communication | [Agents communication](https://microsoft.github.io/multi-agent-reference-architecture/docs/agents-communication/Agents-Communication.html) | Bus vs RPC |
| Observability | [Observability](https://microsoft.github.io/multi-agent-reference-architecture/docs/observability/Observability.html) | Tracing clinical flows |
| Evaluation | [Evaluation](https://microsoft.github.io/multi-agent-reference-architecture/docs/evaluation/Evaluation.html) | SD-Bench metrics |
| Security | [Security](https://microsoft.github.io/multi-agent-reference-architecture/docs/security/Security.html) | PHI, tool authz |
| Governance | [Governance](https://microsoft.github.io/multi-agent-reference-architecture/docs/governance/Governance.html) | Responsible AI |
| Reference Architecture | [Reference Architecture](https://microsoft.github.io/multi-agent-reference-architecture/docs/reference-architecture/Reference-Architecture.html) | Full diagram |

### Glossary

| Term | Meaning |
|------|---------|
| **ADT** | Admit / Discharge / Transfer — HL7 v2 event class |
| **BAA** | Business Associate Agreement — required HIPAA contract |
| **C-CDA** | Consolidated Clinical Document Architecture |
| **CCM** | Chronic Care Management (CPT 99490 family) |
| **CDS** | Clinical Decision Support |
| **CDM** | Chronic Disease Management |
| **DICOM** | Imaging interchange standard |
| **EHR / EMR** | Electronic Health / Medical Record |
| **ePHI** | Electronic Protected Health Information |
| **FHIR** | Fast Healthcare Interoperability Resources |
| **HEDIS** | Healthcare Effectiveness Data and Information Set |
| **HIE** | Health Information Exchange |
| **HITECH** | Health Information Technology for Economic and Clinical Health Act |
| **HL7** | Health Level Seven International |
| **HRRP** | Hospital Readmission Reduction Program |
| **LACE** | Length, Acuity, Comorbidity, Emergency — readmission risk score |
| **LOINC** | Logical Observation Identifiers Names and Codes |
| **MAI-DxO** | Microsoft AI Diagnostic Orchestrator |
| **MARA** | Microsoft Multi-Agent Reference Architecture |
| **MRN** | Medical Record Number |
| **NCPDP** | National Council for Prescription Drug Programs |
| **NDC** | National Drug Code |
| **ONC** | Office of the National Coordinator for Health IT |
| **PHI** | Protected Health Information |
| **PMA** | Pre-Market Approval (FDA) |
| **RPM / RTM** | Remote Physiologic / Therapeutic Monitoring |
| **SaMD** | Software as a Medical Device |
| **SCRIPT** | NCPDP's e-prescribing standard |
| **SD Bench** | Sequential Diagnosis Benchmark |
| **SMART on FHIR** | OAuth 2.0 + OIDC profile for EHR-launched apps |
| **SNOMED CT** | Systematized Nomenclature of Medicine — Clinical Terms |
| **TCM** | Transitional Care Management (CPT 99495/99496) |
| **UCUM** | Unified Code for Units of Measure |
| **USCDI** | US Core Data for Interoperability |
| **X12** | ANSI EDI standard family used for healthcare administrative txns |

### References

**Microsoft research & architecture**
- [The Path to Medical Superintelligence (MAI-DxO, SD Bench)](https://microsoft.ai/news/the-path-to-medical-superintelligence/)
- [Multi-Agent Reference Architecture (overview)](https://microsoft.github.io/multi-agent-reference-architecture/)
- [Reference Architecture chapter](https://microsoft.github.io/multi-agent-reference-architecture/docs/reference-architecture/Reference-Architecture.html)

**Healthcare standards**
- HL7 FHIR R4 — https://www.hl7.org/fhir/R4/
- HL7 v2.x — https://www.hl7.org/implement/standards/product_brief.cfm?product_id=185
- SMART App Launch — https://hl7.org/fhir/smart-app-launch/
- FHIR Bulk Data — https://hl7.org/fhir/uv/bulkdata/
- US Core profiles — https://hl7.org/fhir/us/core/
- SNOMED CT — https://www.snomed.org/
- LOINC — https://loinc.org/
- ICD-10-CM (CDC/NCHS) — https://www.cdc.gov/nchs/icd/icd-10-cm.htm
- RxNorm (NLM) — https://www.nlm.nih.gov/research/umls/rxnorm/
- CPT (AMA) — https://www.ama-assn.org/practice-management/cpt
- DICOM — https://www.dicomstandard.org/

**Regulation**
- HIPAA (HHS) — https://www.hhs.gov/hipaa/
- 21st Century Cures Act / Information Blocking — https://www.healthit.gov/topic/information-blocking
- FDA SaMD — https://www.fda.gov/medical-devices/digital-health-center-excellence
- ONC Certification — https://www.healthit.gov/topic/certification-ehrs

**In-repo visuals**
- MARA platform architecture — [`Architecture.png`](Architecture.png)
- Sequential diagnosis flow — [`architectureflow.png`](architectureflow.png)
- 25 mermaid views — [`docs/diagrams.md`](docs/diagrams.md)

**Clinical AI context**
- NEJM Case Records of the MGH (source material for SD Bench — used by Microsoft with permission per their blog)
- USMLE as contrast to sequential diagnosis benchmarks

---

## License / status

Reference **Go** implementation of MARA patterns for **medical sequential diagnosis** and **value-based care services**. Extend agents and policies incrementally; keep `pkg/orchestration` thin and stable.

**Not a medical device. Not clinical software. Research and education only.**
