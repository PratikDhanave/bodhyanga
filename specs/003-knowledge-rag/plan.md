# Implementation Plan: Knowledge / RAG Port

**Branch**: `feat/knowledge-rag` | **Date**: 2026-07-16 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/003-knowledge-rag/spec.md`

## Summary

Give the existing LLM diagnostician node retrieval-augmented grounding. Before the model call, a
configured knowledge store is queried with a de-identified summary of the case; the top-*k* citations
are prepended to the model prompt as an Evidence block and surfaced on the diagnosis output.
Retrieval is strictly additive — every failure mode (embedder error, empty corpus, empty query, zero
results) still produces a diagnosis with zero citations, mirroring the audit fail-open invariant.
Bodh's `pkg/knowledge` hybrid store (vector cosine + BM25 + reciprocal-rank fusion) and
`pkg/embeddings` provider-agnostic embedder are **imported unchanged** via the existing
`replace ../bodh`; the only new bodh-maf code is a thin retriever seam in `pkg/agents` plus the
per-hop prompt/citation wiring inside the LLM diagnostician. The graph is untouched.

## Technical Context

**Language/Version**: Go 1.26 (module `github.com/PratikDhanave/bodh-maf`)

**Primary Dependencies**:
- `microsoft/agent-framework-go` pinned `v0.0.0-20260710122455-75d0a647f739` — unchanged; the model
  call already uses `agent.RunText(...).Collect()` with `agent.WithStructuredOutput` in
  `pkg/agents/llm_diagnostician.go`.
- `github.com/PratikDhanave/bodh` via local `replace ../bodh` — **new imports**:
  `pkg/knowledge` (`Store` interface, `NewHybridStore`, `LoadCorpus`, `Document`, `Citation`) and
  `pkg/embeddings` (`Embedder` interface, `Mock`, `NewOllama`). Both packages depend only on each
  other (`knowledge.NewHybridStore` imports `pkg/embeddings` and nothing heavier), so they import
  cleanly. **`knowledge.NewInMemoryStore` is deliberately NOT used** — it pulls in
  `pkg/knowledge/rerank` and `pkg/observability`; `NewHybridStore` is the clean seam and is bodh's
  own default retrieval path.
- No new third-party direct dependency; no new database table; no new migration.

**Storage**: none new. The corpus is an in-memory index built at construction from a JSON file
(`knowledge.LoadCorpus` → `Store.Index`). Nothing about RAG is persisted or checkpointed.

**Testing**: stdlib `go test`, fully offline. Retrieval is exercised with `embeddings.Mock` (bodh's
deterministic test embedder: i-th input → vector `[float32(i),0,…]`, plus injectable `Err` and
`Resp`) so cosine/BM25/RRF ranking is deterministic and every degrade path is reachable without a
network. The model call continues to use the existing fake `agent.ProviderConfig{Run: …}` from
step-4. **No real embedding API call in any test** (constitution II); a live-embedder run
(`NewOllama`) is manual behind an env flag.

**Target Platform**: single-process binary (`cmd/slice`); no distribution.

**Project Type**: Go library packages + the existing thin CLI.

**Performance Goals**: one embedder call + one in-memory ranking per LLM-diagnostician invocation on
the retrieval path; the corpus is embedded/indexed once at construction, not per case (FR-011). The
mock-embedder test path adds no network and stays within the slice's <5s no-LLM budget.

**Constraints**: retrieval is fail-open (spec FR-004/FR-005) exactly like the audit invariant; **zero
PHI in the trail** — the query text and every snippet stay out of audit events, checkpoints, and trail
assertions (FR-009); the query is composed only from de-identified case content (PHI stripped at
intake, upstream); deterministic offline tests (FR-010).

**Scale/Scope**: 1 new package (`pkg/agents` retriever seam — likely one new file + edits to
`llm_diagnostician.go` and `types.go` for the citation field), no graph change, no new agent, ~6–8
new tests, 1 demo corpus reused from bodh, CLI flags on `cmd/slice`.

## Constitution Check

*GATE: evaluated against the bodh constitution inherited by bodh-maf (same file specs/001 used).*

| Principle | Status | Notes |
|---|---|---|
| I. MARA pipeline | PASS | Same 4-stage pipeline, same graph; RAG is an enrichment *inside* the diagnostician node, not a new stage or edge. |
| II. LLM-agnostic provider abstraction | PASS | The embedder is provider-agnostic (`embeddings.Embedder`); tests use the deterministic `Mock`, never a real API. Extends the same "provider seam, mocked in tests" discipline to the embedding provider. |
| III. HIPAA (non-negotiable) | PASS | Query built from de-identified case content only; query text and snippets never enter the trail, checkpoints, or logs at PHI level (FR-009); retrieval is fail-open like audit. Citations carry guideline provenance, not patient data. |
| IV. Test-first, coverage gates | PASS | TDD per user story; deterministic mock embedder; degrade paths each have a red-first test; coverage target ≥90% on the new retriever seam. |
| V. Package-first | PASS | Retrieval store/embedder are imported bodh libraries; the only new bodh-maf surface is a small retriever seam in `pkg/agents`; `cmd/slice` stays thin. |
| VI. Observability by default | DEFERRED | Same standing deferral as specs/001 — structured logging for retrieval hit/miss/failure (PHI-free counts only); no OTel wiring in slice scope. |

**Post-design re-check**: no new violations; no new external dependency; no new persistence surface.

## Technical Approach

### Where retrieval sits (relative to the LLM node and the admission boundary)

The admission boundary is unchanged and remains the outermost guard. In
`pkg/agents/llm_diagnostician.go` today, `NewLLMDiagnostician` wraps its body in `guarded(guard, …)`
(the hop admission) and then calls `assessWithModel(ctx, a, wu)`, which builds a prompt via
`buildPrompt(wu)` and runs `a.RunText(runCtx, prompt, agent.WithStructuredOutput(&assess))`.

Retrieval slots **inside the admitted hop, before the model call**:

```
guarded(guard, …) {                      // admission — UNCHANGED, outermost
    evidence, citations := retriever.Retrieve(ctx, wu)   // NEW; fail-open, may be empty
    prompt := buildPrompt(wu, evidence)                  // Evidence block prepended when non-empty
    assess := a.RunText(prompt, WithStructuredOutput)     // UNCHANGED call shape
    route DiagnosisResult{… , Citations: reconcile(assess.Citations, citations)}
}
```

This preserves the property that **the boundary is checked before any retrieval or model work
happens** (a denied hop does no embedding call), and keeps retrieval a private detail of the LLM node
— the rule diagnostician and the graph never see it (FR-008).

### bodh packages: import unchanged vs. adapt

| bodh symbol | Decision | Rationale |
|---|---|---|
| `knowledge.Store` (interface) | **import unchanged** | The retriever seam depends on this interface, not a concrete type — swappable stores. |
| `knowledge.NewHybridStore(embedder)` | **import unchanged** | bodh's default hybrid retrieval; depends only on `pkg/embeddings`. Clean import. |
| `knowledge.LoadCorpus(path)` + `knowledge.Document` | **import unchanged** | JSON corpus loader + doc type; no adaptation. |
| `knowledge.Citation` | **import unchanged** at the seam; **map to a slice-local type** at the output | See below. |
| `embeddings.Embedder`, `embeddings.Mock`, `embeddings.NewOllama` | **import unchanged** | Provider-agnostic interface + deterministic test double + one real backend. |
| `knowledge.NewInMemoryStore` | **do NOT use** | Pulls `rerank` + `observability`; unnecessary weight for the slice. |
| `medical.DiagnosisCitation` | **do NOT import; mirror as a slice-local `agents.Citation`** | Importing it drags the whole `pkg/medical` package (care-plan generators, ADT, NEWS2, registry) into bodh-maf's minimal `agents` types. bodh-maf already defines its own lean message structs (`DiagnosisResult`, `CarePlan`) rather than reusing `pkg/medical`; a slice-local `Citation{Source,Title,Snippet,Score}` (identical shape) keeps that boundary. The seam converts `knowledge.Citation` → `agents.Citation` — a 4-field copy. |

**Net**: retrieval logic (embedding, cosine, BM25, RRF, corpus loading) is 100% imported bodh code,
zero reimplementation (FR-012). The only new bodh-maf code is (a) a `Retriever` seam that owns the
store + query construction + fail-open behavior, and (b) the prompt/citation wiring inside the
existing LLM node.

### Reusing bodh's query + prompt pattern

bodh's `agents/diagnostician_llm.go` already established the exact pattern this port mirrors:
`buildRetrievalQuery(state)` composes one string from salient case fields;
`Store.Search(ctx, query, k)` returns citations; on error it *logs and continues without citations*;
an `Evidence:\n- [source] title — snippet` block is prepended to the user content; and
`if len(model.Citations)==0 && len(retrieved)>0 { use retrieved }` reconciles provenance. The
bodh-maf port reuses this shape adapted to the slice's `Workup`/`DiagnosisResult` types (bodh-maf has
no `medical.CaseState`; the query is built from `wu.Query.Case.Symptoms`, `.History`,
`wu.Query.Round`, and `wu.Results`).

### MAF-go integration points (real, verified)

- The model call is `*agent.Agent.RunText(ctx, prompt, agent.WithStructuredOutput(&assess)).Collect()`
  — already in `pkg/agents/llm_diagnostician.go`. RAG only changes the `prompt` string and the
  post-run citation assembly; the MAF call surface is untouched.
- No new MAF primitive is introduced. No workflow edge, executor, event, or checkpoint change. The
  diagnostician still emits `DiagnosisResult` / `DiagnosticQuery` via the same `ConfigureProtocol`.
- The admission `guard`/middleware wiring on the node is unchanged.

### Keeping the PHI-free-audit invariant

- The retrieval query is built from de-identified case content only (intake's de-id seam runs
  upstream; the diagnostician already sends that content to the model — RAG adds no new PHI exposure).
- Neither the query nor any snippet is ever written to an audit event or checkpoint. The audit bridge
  already records only PHI-free hop signals (agent ID, hashed case token, message type); RAG adds no
  new audit event carrying content. If a retrieval signal is logged at all, it is a **count** (e.g.
  "retrieved N citations" / "retrieval failed") with the hashed case token, never query or snippet
  text. This is asserted by a trail-scan test (SC-004).

### Keeping tests offline

- `embeddings.Mock` gives deterministic vectors and injectable `Err`/`Resp`, so hybrid ranking is
  reproducible and the embedder-error degrade path is directly triggerable — no network.
- The model remains the step-4 fake `agent.ProviderConfig{Run: …}`, scripted to return diagnoses with
  and without a `citations` array to exercise reconciliation.
- The corpus is a small fixture (a copy of / minimal subset modeled on bodh's synthetic
  `data/knowledge/corpus.json`) checked into `pkg/agents/testdata/`.
- Live embedding (`NewOllama`) is reachable only from `cmd/slice` behind an env flag; no test depends
  on it.

## Risks

| Risk | Mitigation |
|---|---|
| **Preview churn** in MAF-go changing the `RunText`/`WithStructuredOutput` surface | RAG does not touch that surface — it only changes the prompt string and post-run assembly. The pinned commit is unchanged; no MAF upgrade in this feature. |
| **Embedder cost / credentials** for a real backend | The automated suite never calls a real embedder (mock only, FR-010); real backends are opt-in via env flag on `cmd/slice`. No credential is required to build, test, or demo (Ollama is local/free; even that is optional). |
| **PHI leaking into the retrieval query or trail** | Query built only from already-de-identified case content; query/snippet never enter the trail or checkpoints; a trail-scan test enforces zero query/snippet content (SC-004, FR-009). |
| **Bodh package pulling unexpected heavy deps** | Verified: `knowledge.NewHybridStore` + `LoadCorpus` depend only on `pkg/embeddings`; the heavy path (`NewInMemoryStore` → rerank/observability) is deliberately avoided. Confirmed by reading `pkg/knowledge/hybrid.go`, `corpus.go`, `store.go` and `pkg/embeddings/embedder.go`, `mock.go`. |
| **Retrieval failure silently degrading quality** (fails open but nobody notices) | Fail-open is intentional and required (FR-004); the failure is logged loudly (FR-005). A future observable metric is the standing Principle-VI deferral, not a slice blocker. |
| **`replace ../bodh`** unversioned | Same standing risk as specs/001; a bodh version tag is required before bodh-maf is ever published. Out of this feature's scope. |

## Scope

**In scope (~1 week, single vertical increment)**: retriever seam in `pkg/agents`; RAG wiring inside
the existing LLM diagnostician; slice-local `Citation` on `DiagnosisResult`; corpus + embedder
configuration on `cmd/slice`; a test corpus fixture; the offline test suite proving SC-001…SC-006.

**Explicit non-goals**:
- Re-ranking (`pkg/knowledge/rerank`) — the hybrid RRF ranking is the slice's retrieval.
- A minimum-score citation cutoff / relevance thresholding.
- Enforcing "model citations ⊆ retrieved set" — provenance reconciliation matches bodh's behavior; no
  hard membership check.
- Retrieval on any node other than the LLM diagnostician (no RAG for rule/intake/planner/care-plan).
- Per-case re-indexing, corpus hot-reload, or a persisted / distributed knowledge service.
- Surfacing citations in the care plan output or the physician-review payload (diagnosis-only here).
- OTel metrics/spans for retrieval (Principle-VI deferral).
- Chunking strategy work — the corpus loader/store owns snippeting; no new chunker.

## Project Structure

### Documentation (this feature)

```text
specs/003-knowledge-rag/
├── spec.md      # this feature's spec
├── plan.md      # this file
└── tasks.md     # dependency-ordered TDD tasks
```

### Source Code (repository root)

```text
pkg/agents/
  retriever.go              NEW — Retriever seam: owns knowledge.Store + query build + fail-open Retrieve
  retriever_test.go         NEW — mock-embedder tests: hit, miss, empty corpus, embedder error, empty query
  llm_diagnostician.go      EDIT — Retrieve before model call; Evidence block in buildPrompt; reconcile citations
  llm_diagnostician_test.go NEW/EDIT — with-corpus → citations; no-corpus → none; model-subset vs retrieved
  types.go                  EDIT — add slice-local Citation type + Citations field on DiagnosisResult
  testdata/corpus.json      NEW — small synthetic PHI-free fixture (modeled on bodh's demo corpus)
cmd/slice/
  providers.go              EDIT — -corpus <path>, -embedder mock|ollama flags; build Retriever, inject into LLM node
pkg/pipeline/
  (unchanged graph)         trail-scan test may live here or in pkg/agents — asserts no query/snippet in trail
```

**Structure Decision**: the entire feature lands inside `pkg/agents` (the one node that grounds) plus
CLI wiring — no new package, no graph edit, mirroring specs/001's "each change maps 1:1 to a story"
discipline while staying a strictly smaller increment.
