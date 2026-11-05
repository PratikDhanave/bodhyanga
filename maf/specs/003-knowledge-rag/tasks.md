# Tasks: Knowledge / RAG Port

**Input**: Design documents from `specs/003-knowledge-rag/`

**Prerequisites**: plan.md, spec.md

**Tests**: INCLUDED — TDD, tests-first per user story. Each story lands independently testable and
green before the next starts. All tests run offline with the deterministic `embeddings.Mock` (spec
FR-010); no real embedding API is ever called.

**Baseline**: specs/001 is complete and green — the graph, admission boundary, audit bridge, LLM
diagnostician node (`pkg/agents/llm_diagnostician.go` with `agent.RunText` + `WithStructuredOutput`),
and the fake `agent.ProviderConfig{Run:…}` all already exist. This feature adds retrieval *inside* the
existing LLM node; it changes no graph edge and no other agent.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable (different files, no dependency on an incomplete task)
- **[Story]**: US1–US3 per spec.md

## Phase 1: Setup

- [ ] T001 Confirm the bodhyanga knowledge/embeddings packages resolve under the existing
  `replace ../bodhyanga`: add nothing to go.mod beyond what the new imports pull; run `go build ./...` and
  `go mod tidy` and verify `github.com/PratikDhanave/bodhyanga/pkg/knowledge` and `.../pkg/embeddings`
  compile in from source (no `NewInMemoryStore` import — hybrid + LoadCorpus only), in
  /Users/genesis/Developer/goworkspace/src/github.com/PratikDhanave/bodhyanga-maf/go.mod

## Phase 2: Foundational (blocking prerequisites)

- [ ] T002 [P] Add the corpus fixture pkg/agents/testdata/corpus.json: a small synthetic, PHI-free
  set of documents (modeled on ../bodhyanga/data/knowledge/corpus.json, each with id/source/title/content/
  metadata) including at least one document that matches the canonical test case's symptoms and one
  that does not — so ranking is observable. Add a `_notice` disclaimer field.
- [ ] T003 [P] Extend the diagnosis output contract in pkg/agents/types.go: add slice-local
  `Citation struct { Source, Title, Snippet string; Score float64 }` (mirror of knowledge.Citation /
  medical.DiagnosisCitation — NOT imported) and `Citations []Citation` on `DiagnosisResult`
  (omitempty). No behavior change yet; existing tests must still pass (field is additive).

**Checkpoint**: fixture + output field exist — story phases can begin.

## Phase 3: User Story 1 — Diagnostician grounds its verdict in retrieved evidence (P1) 🎯 MVP

**Goal**: when a corpus is configured, the LLM diagnostician retrieves top-k citations, injects an
Evidence block into the prompt, and surfaces citations on the result; no corpus ⇒ no citations.

**Independent test**: fake provider + matching corpus ⇒ result carries citations and the prompt seen
by the model contains an Evidence block; no corpus ⇒ zero citations, no Evidence block (SC-001).

- [ ] T004 [P] [US1] Retriever contract tests (write first, red) in pkg/agents/retriever_test.go:
  using `knowledge.NewHybridStore(&embeddings.Mock{...})` indexed from the fixture — Retrieve on a
  matching case returns ≥1 `agents.Citation` (Source/Title/Snippet populated); Retrieve builds the
  query only from de-identified `Workup` fields; nil store ⇒ zero citations, no embedder call.
- [ ] T005 [US1] Retriever seam in pkg/agents/retriever.go: `Retriever` holding a
  `knowledge.Store` + `K int`; `NewRetriever(store, k)` (k<=0 ⇒ default 4); `Retrieve(ctx, Workup)
  ([]Citation, error)` that builds the query from `wu.Query.Case.Symptoms/History`, `wu.Query.Round`,
  `wu.Results` (port of bodhyanga's `buildRetrievalQuery`), calls `store.Search(ctx, query, k)`, and maps
  `knowledge.Citation` → `agents.Citation`. Empty query ⇒ return (nil,nil) without calling the store.
- [ ] T006 [US1] Evidence-block prompt wiring in pkg/agents/llm_diagnostician.go: change
  `buildPrompt` to accept the retrieved citations and prepend an `Evidence:\n- [source] title —
  snippet` block when non-empty (port bodhyanga's format); update the system-prompt guidance so the model
  is instructed to ground in and cite only from supplied evidence.
- [ ] T007 [US1] Wire retrieval into the node in pkg/agents/llm_diagnostician.go: `NewLLMDiagnostician`
  takes an optional `*Retriever`; inside the guarded hop, before `assessWithModel`, call
  `Retrieve`; pass citations into the prompt; after the model returns, set
  `DiagnosisResult.Citations` = reconcile(model citations, retrieved) — keep model's subset when
  present, else the retrieved set (port bodhyanga's reconciliation). Retrieval sits *inside* the admitted
  hop, before the model call (plan §Where retrieval sits).
- [ ] T008 [US1] Node-level tests in pkg/agents/llm_diagnostician_test.go: with-corpus + fake
  provider ⇒ `DiagnosisResult.Citations` non-empty AND the prompt captured by the fake `Run` contains
  "Evidence:"; no-corpus (nil Retriever) ⇒ zero citations, no Evidence block, output otherwise
  identical; model-returns-own-citations ⇒ those are kept; model-returns-none ⇒ retrieved propagated
  (SC-001, FR-002, FR-003).

**Checkpoint**: US1 functional — grounded diagnosis with citations, the MVP capability.

## Phase 4: User Story 2 — The corpus is a configuration choice (P2)

**Goal**: corpus (or none) and embedder are configuration; same case, different corpus ⇒ citations
from whichever is active; zero graph/output-shape change.

**Independent test**: load corpus A ⇒ A's citations; load corpus B ⇒ B's citations; disable ⇒ none —
config-only (SC-002).

- [ ] T009 [P] [US2] Corpus-load helper tests (red) in pkg/agents/retriever_test.go: constructing a
  Retriever from a corpus file path via `knowledge.LoadCorpus` + `NewHybridStore` + `Index` indexes
  once and reuses across N `Retrieve` calls (index count stable — FR-011); a second corpus file yields
  citations drawn from the second (SC-002); empty corpus (zero docs) ⇒ zero citations, no error.
- [ ] T010 [US2] Config constructor in pkg/agents/retriever.go (or cmd/slice/providers.go):
  `LoadRetriever(corpusPath string, emb embeddings.Embedder, k int) (*Retriever, error)` that loads +
  indexes once; empty corpusPath ⇒ returns nil Retriever (RAG disabled), no error.
- [ ] T011 [US2] CLI wiring in cmd/slice/providers.go: `-corpus <path>` (empty ⇒ RAG off) and
  `-embedder mock|ollama` (default mock; ollama via `embeddings.NewOllama($OLLAMA_HOST, model)` behind
  the flag); build the Retriever and inject it into the LLM diagnostician binding only when
  `-diagnostician=llm-*`. Graph construction (pkg/pipeline) is untouched — only the diagnostician
  binding differs (FR-006).
- [ ] T012 [US2] Config-swap test in pkg/agents/retriever_test.go (or a small
  llm_diagnostician_test.go case): same `Workup` run against two Retrievers built from two fixture
  corpora ⇒ citations differ by corpus, `DiagnosisResult` shape identical; RAG-off run ⇒ zero
  citations — proving configuration-only (SC-002).

**Checkpoint**: corpus + embedder are operational configuration choices.

## Phase 5: User Story 3 — Retrieval degrades safely and never breaks a case (P3)

**Goal**: every retrieval failure mode still yields a diagnosis with zero citations; no query/snippet
in the trail.

**Independent test**: erroring embedder / empty corpus / empty query / zero results each still
complete with zero citations and no case failure; trail scan finds no query/snippet (SC-003, SC-004).

- [ ] T013 [P] [US3] Degrade-path tests (red) in pkg/agents/retriever_test.go: `embeddings.Mock{Err:
  …}` ⇒ `Retrieve` returns (nil citations, non-nil error) — the seam surfaces the error but does not
  panic; zero-match corpus ⇒ (nil, nil); empty query ⇒ (nil, nil) with no embedder call recorded
  (assert `Mock.Calls` empty).
- [ ] T014 [US3] Fail-open behavior at the node in pkg/agents/llm_diagnostician.go: a `Retrieve`
  error is logged (PHI-free: hashed case token + "retrieval failed", no query/snippet) and swallowed
  — the model call proceeds ungrounded, the diagnosis is still produced with zero citations
  (FR-004/FR-005). This must NOT alter existing model-failure semantics (an actual model error still
  returns the recorded agent_error, no fabricated diagnosis).
- [ ] T015 [US3] Node degrade tests in pkg/agents/llm_diagnostician_test.go: erroring embedder ⇒
  diagnosis produced, zero citations, no case error; zero-match corpus ⇒ same; and a separate case
  where the *model* errors ⇒ existing failure semantics unchanged regardless of retrieval (SC-003,
  FR-005 last clause).
- [ ] T016 [US3] Trail-PHI-scan test in pkg/pipeline/rag_trail_test.go (reuses the specs/001
  invariant checker / trail collector): run a RAG-enabled case end-to-end through the graph and assert
  the audit trail contains no raw case ID, no retrieval query text, and no snippet content — only
  PHI-free hop signals (SC-004, FR-009).

**Checkpoint**: retrieval is strictly additive and provably PHI-free in the trail.

## Phase 6: Polish & Cross-Cutting

- [ ] T017 [P] Coverage ≥90% on pkg/agents/retriever.go and the RAG additions to
  llm_diagnostician.go (`go test -cover ./pkg/agents/...`); `go vet ./...` + staticcheck clean.
- [ ] T018 [P] Manual live-embedder smoke (optional, env-gated): `cmd/slice -diagnostician=llm-ollama
  -corpus pkg/agents/testdata/corpus.json -embedder ollama` against a local Ollama with an embed model
  (e.g. nomic-embed-text); confirm citations appear. Not part of the automated suite.
- [ ] T019 Update README.md (scope note: RAG now present on the LLM diagnostician; move it out of the
  non-goals list with a one-line "added in specs/003" pointer), architecture.md (a short §
  documenting retrieval inside the LLM node + the import-vs-adapt decision), and CLAUDE.md if the
  `-corpus`/`-embedder` flags change the demo command.

## Dependencies

- Setup (T001) → Foundational (T002–T003) → US1 (T004–T008)
- US2 (T009–T012) requires US1's Retriever + node wiring
- US3 (T013–T016) requires US1 (a retrieval path to degrade) and US2's config constructor; T016 also
  reuses specs/001's trail collector/invariant checker
- Polish (T017–T019) last

Story order: **US1 → US2 → US3**. US2 and US3 both build on US1's node wiring; US3's node degrade
tests (T014–T015) can proceed in parallel with US2's CLI wiring (T011) since they touch different
concerns of the same node.

## Parallel example (within Foundational + US1)

```
T002 (corpus fixture) ∥ T003 (Citation type on DiagnosisResult)
then T004 (retriever tests, red) → T005 (retriever seam) → T006 (prompt) → T007 (node wiring) → T008 (node tests)
```

## Implementation strategy

MVP = Phase 1–3 (US1): a grounded diagnosis with citations from a configured corpus, demoable via
`cmd/slice -diagnostician=llm-ollama -corpus …`. It already proves the differentiator MAF-go lacks.
Then land US2 (make it a real configuration choice) and US3 (make it safe) one story per commit/PR;
each checkpoint leaves `go test ./...` green and the CLI usable, and no test touches a real embedding
service.
