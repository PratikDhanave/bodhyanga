# Bodh — Architecture & Flow Diagrams

Fourteen interactive [Archify](https://github.com/tt-a1i/archify) diagrams covering the
whole system, generated from and validated against the codebase. Each **SVG renders inline
on GitHub**; each links to a **self-contained interactive HTML** (pan, zoom, light/dark
theme, guided views) — open it locally or serve it. Source specs live in
[`diagrams/archify/`](diagrams/archify/).

> Complements the two shipped PNGs ([`Architecture.png`](../Architecture.png),
> [`architectureflow.png`](../architectureflow.png)) and the 25 Mermaid views in
> [`diagrams.md`](diagrams.md).

---

## Platform & core flows

### 1 · Platform architecture (MARA)
One Go binary: clients → orchestrator → in-process bus → agents, gated by governance, HITL and an append-only audit log.

<p align="center"><a href="diagrams/platform.architecture.html"><img src="diagrams/platform.architecture.svg" alt="Platform architecture (MARA)" width="900"/></a></p>

<sub>▶ <a href="diagrams/platform.architecture.html">Interactive</a></sub>

### 2 · Sequential diagnosis — virtual physician panel
The MAI-DxO / SD-Bench loop: presentation → history → cost-guarded tests → diagnosis → verify → HITL gate → final diagnosis.

<p align="center"><a href="diagrams/diagnosis.sequence.html"><img src="diagrams/diagnosis.sequence.svg" alt="Sequential diagnosis panel" width="900"/></a></p>

<sub>▶ <a href="diagrams/diagnosis.sequence.html">Interactive</a></sub>

### 3 · Human-in-the-loop gate
Fail-closed oversight: every consequential action is wrapped as a `review_request`, queued, and either approved (resumed) or rejected (halted).

<p align="center"><a href="diagrams/hitl.workflow.html"><img src="diagrams/hitl.workflow.svg" alt="Human-in-the-loop gate" width="760"/></a></p>

<sub>▶ <a href="diagrams/hitl.workflow.html">Interactive</a></sub>

### 4 · Care-services workflows
Chronic-disease (CDM), transitions (TCM) and panel pathways run on the same bus and fan into patient engagement.

<p align="center"><a href="diagrams/care-services.dataflow.html"><img src="diagrams/care-services.dataflow.svg" alt="Care-services fan-in" width="900"/></a></p>

<sub>▶ <a href="diagrams/care-services.dataflow.html">Interactive</a></sub>

### 5 · LLM → rule fallback
Every LLM-backed agent degrades to its deterministic rule sibling on refusal, timeout or failed grounding — so the case always finalizes.

<p align="center"><a href="diagrams/fallback.lifecycle.html"><img src="diagrams/fallback.lifecycle.svg" alt="LLM to rule fallback" width="900"/></a></p>

<sub>▶ <a href="diagrams/fallback.lifecycle.html">Interactive</a></sub>

### 6 · Audit &amp; observability
Every message forks into a PHI-minimized, append-only audit log (Merkle-chained) and hashed-`case_id` spans — raw MRNs never leave the process.

<p align="center"><a href="diagrams/audit.dataflow.html"><img src="diagrams/audit.dataflow.svg" alt="Audit and observability" width="900"/></a></p>

<sub>▶ <a href="diagrams/audit.dataflow.html">Interactive</a></sub>

---

## Subsystems

### 7 · Authentication &amp; session
JWT login (HS256, bcrypt) → Bearer-gated `/api/*` → a 401 clears the token and redirects to login.

<p align="center"><a href="diagrams/auth.sequence.html"><img src="diagrams/auth.sequence.svg" alt="Authentication and session" width="900"/></a></p>

<sub>▶ <a href="diagrams/auth.sequence.html">Interactive</a></sub>

### 8 · Governance policy chain
Every message passes a fail-closed policy chain (structural → authz → clinical) before it reaches an agent; deny is audited.

<p align="center"><a href="diagrams/governance.workflow.html"><img src="diagrams/governance.workflow.svg" alt="Governance policy chain" width="820"/></a></p>

<sub>▶ <a href="diagrams/governance.workflow.html">Interactive</a></sub>

### 9 · Retrieval-Augmented Generation
Hybrid vector + BM25 retrieval, reciprocal-rank fusion and rerank produce cited clinical context for the diagnostician.

<p align="center"><a href="diagrams/rag.dataflow.html"><img src="diagrams/rag.dataflow.svg" alt="RAG hybrid retrieval" width="900"/></a></p>

<sub>▶ <a href="diagrams/rag.dataflow.html">Interactive</a></sub>

### 10 · LLM provider abstraction
One `llm.Client` interface, a budget-aware cost router, and swappable cloud / local / in-process providers.

<p align="center"><a href="diagrams/llm-providers.architecture.html"><img src="diagrams/llm-providers.architecture.svg" alt="LLM provider abstraction" width="900"/></a></p>

<sub>▶ <a href="diagrams/llm-providers.architecture.html">Interactive</a></sub>

### 11 · Clinical case lifecycle
A case's status from intake to finalized, pausing at the HITL gate; over-budget or timeout is terminal.

<p align="center"><a href="diagrams/case.lifecycle.html"><img src="diagrams/case.lifecycle.svg" alt="Clinical case lifecycle" width="900"/></a></p>

<sub>▶ <a href="diagrams/case.lifecycle.html">Interactive</a></sub>

---

## Interop, evaluation &amp; operations

### 12 · Healthcare interop ingest
HL7 v2 and FHIR R4 sources are parsed, normalized to a `PatientPresentation` and published to the bus.

<p align="center"><a href="diagrams/interop.dataflow.html"><img src="diagrams/interop.dataflow.svg" alt="Healthcare interop ingest" width="900"/></a></p>

<sub>▶ <a href="diagrams/interop.dataflow.html">Interactive</a></sub>

### 13 · SD-Bench evaluation
Case fixtures run through the pipeline; accuracy and cost feed a Pareto view gated on a saved baseline.

<p align="center"><a href="diagrams/sdbench.dataflow.html"><img src="diagrams/sdbench.dataflow.svg" alt="SD-Bench evaluation" width="900"/></a></p>

<sub>▶ <a href="diagrams/sdbench.dataflow.html">Interactive</a></sub>

### 14 · Deployment topology (Kubernetes)
Distroless care pod behind Ingress, guarded by NetworkPolicy, scaled by HPA, with Postgres, ExternalSecrets and OTLP.

<p align="center"><a href="diagrams/deployment.architecture.html"><img src="diagrams/deployment.architecture.svg" alt="Deployment topology" width="900"/></a></p>

<sub>▶ <a href="diagrams/deployment.architecture.html">Interactive</a></sub>

---

<sub>Diagrams generated with [Archify](https://tt-a1i.github.io/archify/). Every diagram passed
Archify's `showcase` validation (9/9 artifact checks, 0 warnings). To regenerate one, edit its
spec under `diagrams/archify/` and re-run `archify deliver <type> <spec>.json <out>.html`.</sub>
