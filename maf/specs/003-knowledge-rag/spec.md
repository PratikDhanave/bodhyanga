# Feature Specification: Knowledge / RAG Port

**Feature Branch**: `feat/knowledge-rag`

**Created**: 2026-07-16

**Status**: Draft

**Input**: User description: "Give the LLM diagnostician retrieval-augmented grounding — before
it forms a diagnosis, retrieve the top-k most relevant clinical-evidence snippets from a swappable
knowledge corpus, inject them as an Evidence block into the model prompt, and surface the matching
citations on the diagnosis output. RAG is bodh's differentiator that MAF-go has no equivalent for
(RAG is .NET-only in the framework). Port bodh's hybrid retrieval store (vector cosine + BM25 +
reciprocal-rank fusion) and its provider-agnostic embedder as-is via the existing `replace ../bodh`,
and slot retrieval into bodh-maf's existing LLM-diagnostician node without changing the graph.
Retrieval must degrade safely: an unavailable embedder, empty corpus, or zero results never breaks a
case — the diagnostician just proceeds ungrounded. Success = the LLM diagnostician returns citations
when a corpus is configured, the corpus is a configuration choice, and every failure mode leaves the
pipeline producing a diagnosis. All automated tests run offline with a deterministic embedder."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Diagnostician grounds its verdict in retrieved evidence (Priority: P1)

As a clinical-platform quality lead, I need the AI diagnostician — when a knowledge corpus is
configured — to retrieve the most relevant clinical-evidence snippets for the case before it
answers, feed them to the model as an explicit Evidence block, and return the citations it relied
on, so that a reviewing physician can see *what the diagnosis was grounded in* rather than trusting
an ungrounded model guess.

**Why this priority**: Retrieval-with-citations is the single differentiating capability this feature
exists to prove on MAF-go; without it there is no story. Every other story hardens or configures this
one. It slots into the one node (the LLM diagnostician) where grounding is clinically meaningful.

**Independent Test**: Configure the LLM diagnostician (fake, deterministic provider) with a small
corpus containing a document that matches the case. Run one case: the diagnosis result carries a
non-empty set of citations, each with a source/title/snippet, and the prompt the model received
contained an Evidence block listing those snippets. Run the same case with no corpus configured: the
diagnosis is produced with zero citations and no Evidence block.

**Acceptance Scenarios**:

1. **Given** a corpus with at least one document relevant to the case and an LLM diagnostician,
   **When** the diagnostician runs, **Then** it retrieves up to *k* citations, prepends an Evidence
   block listing them to the model prompt, and the resulting diagnosis carries those citations.
2. **Given** the model returns its own subset of citations, **When** the result is assembled,
   **Then** the model's chosen citations are kept; **Given** the model returns none, **Then** the
   retrieved citations are propagated so provenance is never lost.
3. **Given** no corpus is configured, **When** the same LLM diagnostician runs, **Then** it produces
   a diagnosis with no Evidence block and no citations (behavior identical to today's node).
4. **Given** the rule diagnostician (non-LLM), **When** a case runs, **Then** retrieval is never
   invoked and no citations are produced (RAG is an LLM-path concern only).

---

### User Story 2 - The corpus is a configuration choice (Priority: P2)

As a clinical-platform operator, I choose per deployment which knowledge corpus (or none) the
diagnostician grounds against, and which embedding backend computes relevance — by configuration
only, with no change to the pipeline graph or the diagnostician's output contract.

**Why this priority**: Swappability is the platform's standing provider-abstraction guarantee applied
to knowledge; it only matters once retrieval (P1) exists, and it is what lets the corpus be curated,
versioned, or disabled without a code change. It is lower risk than P1 but essential for the feature
to be operationally real.

**Independent Test**: Load a corpus from a file, run a case, and observe citations from that corpus;
point the configuration at a second corpus file with different documents and observe citations from
the second; disable the corpus and observe no citations — all three via configuration only, with the
graph and the diagnosis output structure unchanged.

**Acceptance Scenarios**:

1. **Given** a corpus file path, **When** the diagnostician is constructed, **Then** the documents are
   loaded and indexed once at startup and reused across cases.
2. **Given** two different corpus files, **When** each is configured in turn, **Then** the same case
   yields citations drawn from whichever corpus is active, with no graph or output-shape change.
3. **Given** a selected embedding backend (deterministic test embedder, or a local model server for
   manual runs), **When** retrieval runs, **Then** relevance is computed by that backend and the
   choice is a configuration value, not a code edit.
4. **Given** an empty corpus (zero documents), **When** a case runs, **Then** the diagnostician
   produces a diagnosis with zero citations and no error.

---

### User Story 3 - Retrieval degrades safely and never breaks a case (Priority: P3)

As a clinical-platform safety steward, I need retrieval to be strictly *additive*: if the embedder is
unreachable or errors, if the query is empty, or if the corpus returns nothing, the case must still
produce a diagnosis — ungrounded — never a failure. Grounding is an enhancement to a decision that
must be able to happen without it.

**Why this priority**: A retrieval dependency that can fail a clinical case would be worse than no
retrieval at all. This story is what makes P1 safe to ship. It depends on P1 existing to have a
degrade path to protect, and on the admission boundary already guarding the hop.

**Independent Test**: With the diagnostician RAG-enabled, inject an embedder that errors on every
call, run a case, and confirm a diagnosis is still produced with zero citations and no case failure;
repeat with an empty corpus and with a query that yields zero matches — each still completes.

**Acceptance Scenarios**:

1. **Given** an embedder that errors on the retrieval call, **When** the diagnostician runs, **Then**
   the case still produces a diagnosis with zero citations, and the retrieval failure is surfaced to
   operators (logged) but never propagated as a case error.
2. **Given** a corpus that returns zero matches for the case, **When** the diagnostician runs, **Then**
   no Evidence block is added and the diagnosis is produced normally.
3. **Given** retrieval is skipped or fails, **When** the trail for the case is read, **Then** it
   contains no raw case identifier, no query text, and no snippet content — retrieval's success or
   failure is expressed only as PHI-free signals.
4. **Given** the model call itself fails (unrelated to retrieval), **When** the diagnostician runs,
   **Then** the existing failure semantics are unchanged (recorded failure, no fabricated diagnosis),
   and retrieval does not mask or alter that outcome.

---

### Edge Cases

- **Empty corpus** → retrieval returns zero citations; no Evidence block; diagnosis produced. No error.
- **Embedder unreachable / erroring** → retrieval failure is logged loudly, swallowed for the case;
  diagnostician proceeds ungrounded. Fail-open, mirroring the audit invariant.
- **Zero results above the corpus's ranking** → treated exactly like an empty corpus for that case.
- **Query is empty** (a case with no usable retrieval text) → retrieval is skipped entirely; no
  embedder call is made; diagnosis produced ungrounded.
- **Citation with a very low fused score** → still surfaced (the ranking already ordered it into the
  top-k); scoring is informational and never a hard filter in this slice. A minimum-score cutoff is a
  documented non-goal.
- **Model fabricates a citation not in the Evidence block** → the port keeps the model's citations as
  returned (matching bodh's reconciliation), because the model is instructed to cite only supplied
  evidence; enforcing "citation ∈ retrieved set" is a documented non-goal for this slice.
- **Retrieval query would leak PHI** → the query is built only from de-identified case content (PHI is
  stripped at intake, upstream of the diagnostician), and neither the query nor any snippet is ever
  written to the audit trail.
- **Large document** → the corpus loader/store is responsible for chunking/snippeting; the
  diagnostician surfaces bounded snippets, not full documents.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: When a corpus is configured, the LLM diagnostician MUST retrieve up to a
  configuration-set *k* most-relevant evidence snippets for the case *before* invoking the model.
- **FR-002**: Retrieved snippets MUST be injected into the model prompt as an explicit, labeled
  Evidence block, and the model MUST be instructed to ground its rationale in that evidence and cite
  only from it.
- **FR-003**: The diagnosis result MUST carry the citations relied upon: the model's own subset when
  it returns one, otherwise the retrieved set (provenance is never dropped).
- **FR-004**: Retrieval MUST be strictly additive — an embedder error, empty corpus, empty query, or
  zero results MUST still yield a diagnosis (ungrounded, zero citations), never a case failure.
- **FR-005**: A retrieval failure MUST be surfaced to operators (logged) but MUST NOT be recorded as a
  case/agent error nor alter the existing model-failure semantics (recorded failure, no fabricated
  diagnosis).
- **FR-006**: The corpus MUST be selectable by configuration (a corpus source, or none) with zero
  changes to the pipeline graph or the diagnosis output contract.
- **FR-007**: The embedding backend MUST be selectable by configuration among the provider-agnostic
  options, defaulting the automated suite to a deterministic offline embedder.
- **FR-008**: The rule (non-LLM) diagnostician MUST NOT invoke retrieval and MUST produce no citations.
- **FR-009**: The retrieval query MUST be built only from de-identified case content; neither the
  query text nor any retrieved snippet may appear in the audit trail, checkpoints, logs at PHI level,
  or test assertions of the trail. Retrieval is observable only as PHI-free signals.
- **FR-010**: Automated tests MUST NOT call a real embedding service; retrieval is exercised with a
  deterministic in-process embedder, and any live-embedder run is opt-in via an explicit environment
  flag.
- **FR-011**: The corpus is loaded and indexed once at diagnostician construction and reused across
  cases within a process; re-indexing is not required per case.
- **FR-012**: Bodh's retrieval store and embedder abstractions MUST be consumed as libraries (via the
  existing local module reference), not reimplemented; their retrieval semantics are the authority.

### Key Entities

- **Corpus**: the configured set of clinical-evidence documents the diagnostician grounds against;
  swappable or absent. PHI-free by construction (guideline/textbook prose, not patient data).
- **Document**: one indexable unit of the corpus — an identifier, a source/citation pointer, a title,
  body content, and optional metadata.
- **Citation**: a retrieved snippet with its provenance (source, title, snippet) and a relevance
  score; the atom surfaced on a diagnosis.
- **Evidence block**: the labeled section prepended to the model prompt listing the retrieved
  citations the model is asked to ground in and cite from.
- **Embedder**: the provider-agnostic backend that turns text into vectors for relevance ranking;
  selectable by configuration, deterministic in tests.
- **Retrieval query**: the single de-identified string composed from the case's salient fields, sent
  to the store to rank documents.
- **Diagnosis result**: the diagnostician's structured output — unchanged in shape except that it now
  carries the citations relied upon (empty when RAG is off or degraded).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: With a matching corpus configured, 100% of LLM-diagnostician runs return at least one
  citation and the model prompt contains an Evidence block; with no corpus, 100% return zero citations
  and no Evidence block.
- **SC-002**: Switching the active corpus is a configuration-only change (zero graph edits, zero
  output-shape changes), verified by running the same case against two different corpus files and
  observing citations drawn from whichever is active.
- **SC-003**: 100% of retrieval-failure modes (embedder error, empty corpus, empty query, zero
  results) still produce a diagnosis with zero citations and no case failure.
- **SC-004**: An automated scan of the audit trail for a RAG-enabled case finds zero raw identifiers,
  zero query text, and zero snippet content.
- **SC-005**: The entire automated suite for this feature runs offline (no network embedding call),
  deterministically, on every run — no opt-in flags required for the proof.
- **SC-006**: The rule-diagnostician path produces identical output (zero citations) whether or not a
  corpus is configured — retrieval never touches the non-LLM path.

## Assumptions

- The parent platform's knowledge-retrieval store (hybrid vector + lexical fusion) and its
  provider-agnostic embedder are consumable as libraries from this codebase via the existing local
  module reference, and their retrieval semantics — not reimplementations — are the authority.
- PHI is already stripped at intake (the existing pipeline's de-identification seam runs upstream of
  the diagnostician), so the de-identified case content the query is built from is safe to embed.
- A synthetic, PHI-free demo corpus exists in the parent platform's data directory and is suitable for
  tests and demos; no real clinical guideline data is required.
- The LLM diagnostician node already exists and is the only node where grounding is inserted; the
  three other agents and the graph shape are unchanged by this feature.
- A deterministic in-process embedder is sufficient to prove retrieval wiring and every degrade path;
  a real embedding backend is available for manual/live verification but never required by the suite.
- This remains a single-process slice; distribution and a knowledge-service split are out of scope.
