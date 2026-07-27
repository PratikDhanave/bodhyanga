# Implementation Plan: Slice Safety Boundary

**Branch**: `feat/slice-safety-boundary` | **Date**: 2026-07-12 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/001-slice-safety-boundary/spec.md`

## Summary

Harden the step-1 MARA pipeline with bodh's safety boundary, implemented on MAF-go
primitives: one `pkg/admit` decorator gives every hop fail-closed governance
(`bodh/pkg/governance.Policy`) and fail-open audit (`bodh/pkg/audit.Recorder`);
`pkg/auditbridge` maps the workflow event stream onto the append-only trail;
a physician-review `RequestPort` plus a Postgres `checkpoint.Store[json.RawMessage]`
(`pkg/ckptpg`) makes paused cases survive process death; the diagnostician becomes a
flag-selected binding (rule executor | Anthropic | Ollama-via-OpenAI-provider) with
structured output; and a terminal-hop recorder writes one `eval.InteractionRecord`
per finished case. Every README exit criterion gets an automated test.

## Technical Context

**Language/Version**: Go 1.26 (module `github.com/PratikDhanave/bodh-maf`)

**Primary Dependencies**: `microsoft/agent-framework-go` pinned
`v0.0.0-20260710122455-75d0a647f739` (agent, workflow, checkpoint, inproc,
anthropicprovider, openaiprovider); `github.com/PratikDhanave/bodh` via local
`replace ../bodh` (pkg/governance, pkg/audit, pkg/eval, pkg/protocol, pkg/phi);
`jackc/pgx/v5` (checkpoint store); `testcontainers-go` (test-only)

**Storage**: PostgreSQL — `checkpoints` table (append-only, jsonb payload); audit uses
bodh's `Recorder` interface (InMemory in tests/demo; the production sink chain is
bodh's concern, not re-solved here)

**Testing**: stdlib `go test`; testcontainers Postgres for `pkg/ckptpg` and the
kill/resume integration test; LLM providers mocked via custom `agent.ProviderConfig`
RunFunc — **no real AI calls in tests** (constitution II); live-LLM verification manual
via env flags

**Target Platform**: single-process binary (darwin/linux); no distribution

**Project Type**: Go library packages + one thin CLI (`cmd/slice`)

**Performance Goals**: admission adds one in-process policy evaluation + one audit
write per hop (<1ms, no network on the allow path); slice runs a case end-to-end in
<5s without LLM, <60s with a local model

**Constraints**: fail-closed governance / fail-open audit (non-negotiable ordering:
deny-record write is synchronous with the denial); zero PHI in audit events,
checkpoints metadata, span attributes, or test assertions (raw IDs only inside the
clinical payload / checkpoint jsonb, which is access-controlled); deterministic tests

**Scale/Scope**: 1 workflow, 5 graph nodes, 4 new packages, ~1 migration, ~15 tests

## Constitution Check

*GATE: evaluated against `.specify/memory/constitution.md` (bodh constitution,
inherited by bodh-maf).*

| Principle | Status | Notes |
|---|---|---|
| I. MARA pipeline | PASS | Same 4-stage pipeline; typed messages via MAF edges instead of pkg/orchestration — the evaluated substitution, see Complexity Tracking |
| II. LLM-agnostic provider abstraction | PASS w/ deviation | Provider seam is MAF `agent.ProviderConfig`, not `pkg/llm.Client`; tests mock the provider (RunFunc), never call real APIs. Deviation justified below |
| III. HIPAA (non-negotiable) | PASS | bodh pkg/audit append-only trail; PHI-free events (correlation handles only); pkg/phi seam at intake; HITL gate for every AI decision; hashed case IDs everywhere observable |
| IV. Test-first, coverage gates | PASS | Tests per exit criterion; testcontainers per bodh convention; deterministic mocks; coverage target ≥90% on new pkgs |
| V. Package-first | PASS | pkg/admit, pkg/auditbridge, pkg/ckptpg, pkg/evalhook libraries; cmd/slice stays thin |
| VI. Observability by default | DEFERRED | MAF `WithTelemetry` exists but OTel wiring is out of slice scope; structured logging only. Recorded as a known gap, not a violation |

**Post-design re-check (Phase 1)**: no new violations introduced; deviation II
unchanged and documented.

## Project Structure

### Documentation (this feature)

```text
specs/001-slice-safety-boundary/
├── spec.md
├── plan.md              # this file
├── research.md          # Phase 0
├── data-model.md        # Phase 1
├── quickstart.md        # Phase 1
├── contracts/
│   ├── audit-trail.md
│   ├── checkpoint-postgres.md
│   └── admission-and-review.md
├── checklists/requirements.md
└── tasks.md             # Phase 2 (/speckit-tasks — not created here)
```

### Source Code (repository root)

```text
cmd/slice/               flags (-policy, -diagnostician, -resume, -db), run loop,
                         physician inbox (CLI), wiring only
pkg/agents/              (exists) + llm_diagnostician.go
pkg/pipeline/            (exists) + admit/review/eval wiring in New()
pkg/admit/               Wrap decorator + agent.Middleware variant + envelope mapping
pkg/auditbridge/         WatchStream consumer → audit.Recorder
pkg/ckptpg/              checkpoint.Store[json.RawMessage] over pgx
pkg/evalhook/            OutputEvent → eval.InteractionRecord
db/0001_checkpoints.sql  append-only checkpoints table
```

**Structure Decision**: flat `pkg/` libraries + thin `cmd/`, mirroring bodh
(constitution V); each new package maps 1:1 to a spec user story to keep tasks and
tests independently landable.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| Constitution II: provider seam is MAF `ProviderConfig`, not bodh `pkg/llm.Client` | Evaluating MAF as the platform substrate IS this repo's purpose (ADR-0006/0007 context); MAF agents must own the tool/streaming loop for structured output to work | Adapter layering `pkg/llm.Client` under a MAF RunFunc was considered (research.md R6) — rejected for the slice because it would mask exactly the provider-fidelity questions the slice must answer; revisit if bodh-maf graduates |
| New direct dep `jackc/pgx/v5` | checkpoint store needs Postgres | Reusing bodh/pkg/persistence would drag the full clinical schema into a store that needs one table |
