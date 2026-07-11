# bodh-maf — Bodh on the Microsoft Agent Framework for Go

A **vertical-slice reimplementation** of [bodh](../bodh)'s MARA clinical pipeline on
[`microsoft/agent-framework-go`](https://github.com/microsoft/agent-framework-go) (MAF-go),
built to answer one question with evidence instead of argument:

> Should bodh's hand-rolled orchestration substrate (in-process bus, admission pipeline,
> HITL plumbing, per-agent runtime) be replaced by MAF-go?

Like bodh, this is a **research and education codebase — not a medical device, not for
clinical use**.

## Status

**Design phase.** See [`architecture.md`](architecture.md) — no production code until the
design is approved. Decision context lives in `../bodh/session.md` (2026-07-11 review of
MAF-go at commit `75d0a64`, which this module pins).

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

## Exit criteria

The slice succeeds if it demonstrates, with tests:

1. **Admission fidelity** — a governance denial blocks the handler and is audited,
   identically to bodh's `gateway.Pipeline.Admit` semantics.
2. **Audit parity** — one case produces an audit trail equivalent to the monolith's.
3. **Durable HITL** — a case paused at physician review survives a process restart and
   resumes from the Postgres checkpoint.
4. **Provider swap** — rule ↔ LLM diagnostician switch is a flag, not a graph change.
5. **Eval mirror closed** — the terminal hop writes an `eval.InteractionRecord`.

If any of these can't be met cleanly, that is itself the answer, and bodh's specs/002
resumes.
