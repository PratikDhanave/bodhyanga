# bodh-maf — Bodh on the Microsoft Agent Framework for Go

A **vertical-slice reimplementation** of [bodh](../bodh)'s MARA clinical pipeline on
[`microsoft/agent-framework-go`](https://github.com/microsoft/agent-framework-go) (MAF-go),
built to answer one question with evidence instead of argument:

> Should bodh's hand-rolled orchestration substrate (in-process bus, admission pipeline,
> HITL plumbing, per-agent runtime) be replaced by MAF-go?

Like bodh, this is a **research and education codebase — not a medical device, not for
clinical use**.

## Status

**Slice complete — all five exit criteria proven by tests (2026-07-12).**
`go test ./...` runs the proof (`pkg/pipeline/exitcriteria_test.go` + the
Postgres kill/resume test in `pkg/ckptpg`). Design record: [`architecture.md`](architecture.md);
spec chain: `specs/001-slice-safety-boundary/`; verdict: `../bodh/session.md`.

## Scope of the slice

The 4-agent MARA pipeline — intake → test planner → diagnostician → care plan — as a MAF
workflow graph, with:

- the diagnostician **loop-back** to the test planner as a conditional edge
- bodh's **fail-closed governance / fail-open audit** boundary enforced at every hop
- **audit trail** fed from the workflow event stream (never message content / PHI)
- a **physician-review HITL gate** (`RequestPort`) with pause/resume across process restarts
- a **Postgres checkpoint store** behind MAF's 3-method `checkpoint.Store` interface
- **rule-based and LLM diagnosticians** swappable without graph changes
  (Anthropic native; Ollama via the OpenAI-compatible provider)
- an **eval record on the terminal hop** — closing the eval-mirror gap documented in
  bodh's `pkg/runtime/doc.go`

Bodh's compliance layer is **imported, not rewritten**: `pkg/audit`, `pkg/phi`,
`pkg/governance` (via a local `replace` directive during development).

## Explicit non-goals

Care API + SPA, FHIR/HL7 ingestion, RAG/knowledge, the full eval harness, the remaining
~20 agents, and any distribution story (MAF-go is in-process; A2A comes later if ever).

## Exit criteria — all proven ✅

| # | Criterion | Proof |
|---|---|---|
| 1 | **Admission fidelity** — denial blocks the handler, audited, fail-closed | `TestExitCriterion1_AdmissionFidelity`, `pkg/admit` suite |
| 2 | **Audit parity** — PHI-free trail reconstructs the case lifecycle | `TestExitCriterion2_AuditParity`, `trail_test.go` (per-hop parity; BSP ordering note in contracts/audit-trail.md) |
| 3 | **Durable HITL** — paused review survives process death, same request republished | `pkg/ckptpg.TestKillResume` (Postgres/testcontainers) + `TestExitCriterion3_DurableReview` (file store) |
| 4 | **Provider swap** — rule ↔ LLM is a flag, not a graph change; malformed output never becomes a diagnosis | `TestExitCriterion4_EngineSwap`, `engine_swap_test.go` |
| 5 | **Eval mirror closed** — exactly one `eval.InteractionRecord` per terminal case | `TestExitCriterion5_EvalMirrorClosed` |
