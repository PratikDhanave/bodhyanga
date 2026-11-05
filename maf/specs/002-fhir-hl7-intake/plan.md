# Implementation Plan: FHIR/HL7 Intake

**Branch**: `feat/fhir-hl7-intake` | **Date**: 2026-07-16 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/002-fhir-hl7-intake/spec.md`

## Summary

Add a *translation front-door* that turns an inbound HL7 v2 ADT message or a FHIR R4 bundle
into exactly one `agents.PatientCase` — the type the existing intake executor already consumes
(`pkg/agents/types.go:13`, `pkg/agents/intake.go:14`) — so a real hospital front door
(ADT feed, FHIR endpoint) can drive the *unchanged* MARA slice pipeline. The front-door is a
new library package `pkg/intake` plus a translation *executor node* prepended to the graph;
it is admitted, audited, and fail-closed exactly like every specs/001 hop, and it never leaks
PHI to the trail. bodhyanga's ADT parser (`bodhyanga/pkg/hl7v2`) and FHIR resource types
(`bodhyanga/pkg/fhir`) are imported unchanged via the existing `replace ../bodhyanga`; the only new
parsing is a FHIR *bundle envelope* decoder, which bodhyanga lacks. The real design work is the
`medical.* → agents.PatientCase` mapping and the fail-closed rejection taxonomy.

## Technical Context

**Language/Version**: Go 1.26 (module `github.com/PratikDhanave/bodhyanga-maf`)

**Primary Dependencies**:
- `github.com/PratikDhanave/bodhyanga` via `replace ../bodhyanga` — **already wired** (go.mod:5).
  New imports: `bodhyanga/pkg/hl7v2` (ADT parse, `ParseMessage` / `MessageToADTEvent` /
  `triggerToType`), `bodhyanga/pkg/fhir` (resource types `Patient`/`Condition`/`Observation`/
  `Encounter` + `ConditionCode` / `ObservationToRPM`), `bodhyanga/pkg/medical` (`ADTEvent`,
  `PatientRecord`). Verified importable: `go list -deps` shows their only bodhyanga transitive
  deps are `pkg/llm` + `pkg/medical`, both already reachable — **no go.mod change needed**.
- `microsoft/agent-framework-go` pinned `v0.0.0-20260710122455-75d0a647f739` (workflow only —
  the translation node is a plain `workflow.NewExecutor`, same primitive as
  `pkg/agents/intake.go`; no new MAF surface).
- No new third-party dependency.

**Storage**: none new. Audit uses specs/001's `audit.Recorder` via the existing bridge; no
schema, no checkpoint change (the front-door runs before the review gate).

**Testing**: stdlib `go test`; fixtures from `bodhyanga/pkg/hl7v2/testdata` (`adt_a01_admit.hl7`,
`adt_a08_update.hl7`, `adt_a03_discharge.hl7`, `adt_malformed.hl7`) plus new FHIR bundle
fixtures under `pkg/intake/testdata/`. No network, no live EHR/FHIR server (spec FR-010).

**Target Platform**: single-process binary; input is a file/byte payload, not a listener.

**Project Type**: Go library package (`pkg/intake`) + a translation executor + CLI flags.

**Performance Goals**: translation is one in-memory parse + map per case (<1ms typical); it
adds one admitted+audited hop before intake, matching specs/001's per-hop cost profile.

**Constraints** (inherited from specs/001, non-negotiable): fail-closed on the translation
admission decision; **fail-closed on parse ambiguity** (reject, never guess); zero PHI in
audit events — rejection reasons are a fixed category enum, never interpolated from message
content; the case fingerprint is `agents.HashCaseID` of the derived CaseID.

**Scale/Scope**: 1 new package (`pkg/intake`), 1 new executor node prepended to the graph via
an opt-in Config field, ~1 FHIR bundle decoder, 2 source→PatientCase mappers, ~12–15 tests.

## Constitution Check

*GATE: evaluated against `.specify/memory/constitution.md` (bodhyanga constitution, inherited).*

| Principle | Status | Notes |
|---|---|---|
| I. MARA pipeline | PASS | Same 4-stage pipeline; the front-door is a *pre-intake* translation hop feeding the unchanged intake executor — no graph edit, no new agent stage |
| II. LLM-agnostic provider abstraction | N/A | No LLM in this feature; translation is deterministic parsing/mapping |
| III. HIPAA (non-negotiable) | PASS | Rejection reasons are a PHI-free category enum; CaseID hashed everywhere observable; translation hop admitted + audited like every hop; raw MRN/narrative never enters the trail |
| IV. Test-first, coverage gates | PASS | Tests-first per story; deterministic fixtures (bodhyanga testdata + new FHIR bundles); ≥90% coverage target on `pkg/intake` |
| V. Package-first | PASS | `pkg/intake` library; the executor node lives in `pkg/agents` alongside intake; CLI stays thin |
| VI. Observability by default | DEFERRED | Same posture as specs/001 (structured logging only; OTel out of slice scope) — not a new violation |

**Post-design re-check**: no new violations; the one deliberate adaptation (see Complexity
Tracking) is the `medical.* → PatientCase` semantic mapping.

## Technical Approach

### Where the translation sits in the graph

The intake executor is fixed: it consumes `agents.PatientCase` and, after de-identification,
emits `DiagnosticQuery{Case, Round:0}` (`pkg/agents/intake.go:15-21`). Two placement options
were considered:

1. **Prepend a translation executor as the new start node** (chosen). A new
   `agents.NewTranslator(id, guard)` executor consumes an *inbound-message* type
   (`intake.Inbound{Format, Payload}`), calls `pkg/intake.Translate`, and on success sends the
   resulting `PatientCase` on the edge to the existing intake executor; on failure it records
   a PHI-free rejection and terminates the run (no `PatientCase` emitted). `pkg/pipeline.New`
   gains an opt-in `Config.Translate bool` (default false ⇒ current behavior, graph unchanged;
   true ⇒ start node is the translator, `AddEdge(translator, intake)`). This keeps the
   translation hop *inside* the admission boundary (`guarded(...)` wraps its body, identical to
   every other executor — spec FR-004) and inside the audit bridge (its `ExecutorCompleted` /
   `ExecutorFailed` events flow to the trail with zero extra wiring — `pkg/pipeline/runner.go`
   `drain` already consumes them).

2. *Translate before the workflow starts* (rejected): calling `pkg/intake.Translate` in
   `cmd/slice`/the Runner before `RunStreaming`. Rejected because it moves the translation hop
   *outside* the admission boundary and the audit bridge — violating FR-004 and losing the
   "translation is admitted and audited like every hop" property the spec requires. The graph
   node keeps the boundary uniform.

### Package boundaries: import unchanged vs. adapt

**Import unchanged** (spec FR-009 — do not reimplement):
- `bodhyanga/pkg/hl7v2`: `ParseMessage(raw)` → `*Message`; `MessageToADTEvent(msg)` →
  `medical.ADTEvent` (already refuses non-ADT and unsupported triggers — `hl7v2.go:347,418`,
  giving us the rejection categories for free); the parser reads delimiters off MSH-1/2 and
  handles timestamp precision, so the front-door inherits all of that.
- `bodhyanga/pkg/fhir`: the resource *types* `Patient`, `Condition`, `Observation`, `Encounter`
  and the helpers `ConditionCode(c)` (`fhir.go:146`) and `ObservationToRPM(o)` (`fhir.go:99`).

**New code (bodhyanga has no equivalent)**:
- **FHIR bundle envelope decoder** — `bodhyanga/pkg/fhir` parses *individual* resources but has
  **no `Bundle` type** (verified: `fhir.go` + `bulk.go` define no Bundle/entry decoder). We
  add a minimal `Bundle{ resourceType, entry:[{resource: json.RawMessage}] }` decoder in
  `pkg/intake` that dispatches each entry's `resourceType` to the bodhyanga resource types. This is
  the one genuinely-new parser and must reject >1 Patient ("ambiguous subject") and 0 Patient
  ("missing required resource").
- **`medical.* → agents.PatientCase` mapper** — bodhyanga's parsers target `medical.ADTEvent`
  (`{EventID, PatientID, PrimaryDx, ...}`) and `medical.PatientRecord`, **not**
  `agents.PatientCase{CaseID, Symptoms[], History[]}`. This mapping is the feature's core new
  logic and its main clinical-fidelity judgment call (see Complexity Tracking):
  - HL7: `CaseID <- ADTEvent.PatientID`; `History <- [ADTEvent.PrimaryDx]` when present;
    `Symptoms <- []` (ADT carries a diagnosis, not a symptom list) ⇒ undifferentiated cases
    flow into forced progression (spec FR-008).
  - FHIR: `CaseID <- Patient.id`; each `Condition` → one `History` entry via `ConditionCode`;
    each recognized `Observation` → one `Symptoms`/`History` entry (code/text). No coded
    resource ⇒ undifferentiated.

### Preserving the two safety invariants

- **Admission-boundary-on-every-hop**: the translator is a normal executor wrapped by the same
  `agents.Guard` closure (`pkg/agents/guard.go:9`) as intake; when `Config.Boundary != nil`,
  `cfg.Boundary.Guard` is threaded in exactly as `pkg/pipeline.New` already does for the other
  four executors (`pipeline.go:47-63`). A deny-all or deny-agent policy on the translator
  fails closed — no case runs, one synchronous deny record (specs/001 FR-002/FR-003).
- **PHI-free audit**: the translator keys its `guarded(...)` call on
  `agents.HashCaseID(caseID)` when a CaseID is recoverable, else on the empty token; the
  audit event carries only agentID + msgType + fingerprint (never `Payload`). Rejection
  reasons are members of a fixed enum, never built from message text — the compiler-visible
  guarantee against interpolating an MRN into a reason string. Test SC-004 scans every emitted
  record for raw fixture MRNs/names to prove it.

### MAF-go integration points (real, not invented)

- Translation node: `workflow.NewExecutor(id, func(*workflow.Context, intake.Inbound) error)`
  + `.Extend(&workflow.Executor{ConfigureProtocol: ...})` declaring it *sends* `PatientCase` —
  the exact pattern `NewIntake` uses (`pkg/agents/intake.go:14-27`).
- Graph wiring: `workflow.NewBuilder(translator).AddEdge(translator, intake)...` — the same
  builder/edge API `pkg/pipeline.New` already calls (`pipeline.go:68-72`). No new MAF primitive.
- Rejection terminates the run without emitting `PatientCase`: the executor simply returns
  after recording — the pipeline produces no output, surfacing as no `CarePlan`/`CaseRejected`
  in `Outcome`, which the Runner already handles.

## Risks

| Risk | Likelihood | Mitigation |
|---|---|---|
| **PHI leakage into a rejection reason or audit event** (the cardinal sin) | Med | Reasons are a fixed enum, *never* `fmt`-interpolated from message content; a dedicated test (SC-004) scans every front-door record for the fixtures' literal MRNs/names; translator audits only fingerprint + category |
| **`medical.* → PatientCase` mapping is clinically thin** (ADT gives a diagnosis, the pipeline wants symptoms) | High | Explicitly scoped as demo-grade (repo is research-not-clinical); documented in spec Assumptions + Complexity Tracking; undifferentiated cases are a *designed* outcome (FR-008), not a bug |
| **bodhyanga parser gaps** (no FHIR bundle decoder; ADT subset only) | Known | Bundle decoder is the one new parser, scoped small; ADT is limited to the trigger set bodhyanga supports and everything else is a *category rejection*, not a crash |
| **MAF-go preview API churn** | Low | Pin unchanged (CLAUDE.md ground rule); this feature touches only `workflow.NewExecutor`/`NewBuilder`/`AddEdge`, the most stable surface, already exercised by specs/001 |
| **Graph regression** (breaking the specs/001 slice) | Low | `Config.Translate` defaults false ⇒ byte-identical existing graph; the translator is additive and gated; existing tests must stay green (added to the story's DoD) |
| **Bundle decoder accepting a multi-patient bundle and guessing** | Med | Decoder counts Patient resources and rejects ≠1 as "ambiguous subject"/"missing resource" (explicit edge case + acceptance scenario) |

## Scope & Sizing (≈1 week)

One vertical, independently-shippable increment. MVP is **US1 alone** (HL7 ADT → care plan):
a demoable front door on the most common hospital protocol, reusing bodhyanga's parser unchanged.
US2 (FHIR bundle) and US3 (fail-closed rejection taxonomy) layer on the same seam. Estimated
~1 new package, ~2 mappers, 1 bundle decoder, ~12–15 tests. Explicit non-goals (spec §Non-Goals):
no network listener, no multi-patient bundles, no ORU/ORM, no write-back, no new
`PatientCase` field, no graph edit.

## Project Structure

### Documentation (this feature)

```text
specs/002-fhir-hl7-intake/
├── spec.md
├── plan.md          # this file
└── tasks.md
```

### Source Code (repository root)

```text
pkg/intake/                  NEW — the translation front-door library
  inbound.go                 Inbound{Format, Payload}; Format enum (json|hl7|fhir)
  translate.go               Translate(Inbound) (agents.PatientCase, error); RejectReason enum
  hl7.go                     ADT message → PatientCase (wraps bodhyanga/pkg/hl7v2 + medical.ADTEvent)
  fhir.go                    Bundle decoder (NEW) + bundle → PatientCase (wraps bodhyanga/pkg/fhir)
  testdata/                  FHIR bundle fixtures (+ reuse bodhyanga/pkg/hl7v2/testdata for HL7)
pkg/agents/
  translator.go              NEW — NewTranslator(id, guard) *workflow.Executor (pre-intake node)
pkg/pipeline/
  pipeline.go                (edit) add Config.Translate bool; prepend translator when set
cmd/slice/
  main.go                    (edit) -format json|hl7|fhir; read raw payload; build Inbound
```

**Structure Decision**: a dedicated `pkg/intake` library holds all parsing/mapping (pure,
unit-testable without MAF); the thin `agents.NewTranslator` executor is the only MAF-aware
piece and lives beside the other executors (constitution V). The pipeline change is a single
opt-in Config field so the specs/001 graph is untouched by default.

## Complexity Tracking

| Deviation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| New `medical.* → agents.PatientCase` mapping (not a straight import) | bodhyanga's parsers target `medical.ADTEvent`/`PatientRecord`, not the slice's `PatientCase`; no bodhyanga code bridges the two | Adding a new field to `PatientCase` or reshaping intake to consume `medical.*` was rejected — it would ripple into every specs/001 package and change the frozen pipeline entry type (spec SC-006) |
| New FHIR bundle envelope decoder | `bodhyanga/pkg/fhir` has no `Bundle` type — it parses individual resources only (verified in fhir.go/bulk.go) | Reusing a bodhyanga decoder is impossible (none exists); pulling a full third-party FHIR library would drag a large dependency for a slim, well-scoped envelope decode |
| Translation as a graph node vs. pre-graph call | Keeps the translation hop inside the admission boundary + audit bridge (spec FR-004) | Pre-graph translation in the Runner was rejected — it would bypass admission/audit, breaking the "every hop is admitted" invariant specs/001 exists to prove |
