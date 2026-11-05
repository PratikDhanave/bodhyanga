# Bodhyanga — Roadmap

Where Bodhyanga has been, where it is, and where it's going.

> Pairs with [CONTRIBUTING.md](CONTRIBUTING.md) for how to take work on, and the [main README](README.md) for the current implemented surface.

---

## Status legend

- 🟢 **Done** — shipped on `main`, tested, documented
- 🟡 **In progress** — branch or open PR exists
- ⚪ **Planned** — committed direction, no branch yet
- 🔵 **Considering** — open question; needs design discussion

---

## Phase 1 — MARA platform + diagnostic panel 🟢

The foundation: orchestrator pattern, agent contract, governance, bus, registry.

- 🟢 `pkg/protocol`, `pkg/agent`, `pkg/registry`, `pkg/comm`, `pkg/orchestration`
- 🟢 `pkg/governance` with `MaxContentLength`, `RequireCaseID`, `MaxDiagnosticCost`, `ResearchDisclaimer` policies
- 🟢 Eight diagnostic agents (`intake`, `rpm_monitor`, `diagnostic_supervisor`, `questioner`, `test_planner`, `cost_guardian`, `diagnostician`, `reasoning_verifier`)
- 🟢 `cmd/demo` end-to-end CHF + CAP cases
- 🟢 `pkg/eval` with `CaseMetrics` and `ScoreCase`
- 🟢 Governance test suite + medical domain tests

---

## Phase 2 — Care services 🟢

Value-based care workflows on the same bus.

- 🟢 Nine care-services agents (`cdm_planner`, `tcm_coordinator`, `panel_manager`, `pre_visit_prep`, `inbox_triage`, `refill_manager`, `prior_auth`, `engagement`, `readmission_tracker`)
- 🟢 `pkg/tasks` nurse queue, `pkg/engagement` notifier, `pkg/analytics` cohort metrics
- 🟢 `pkg/fhir` R4 adapter (Patient / Observation / Encounter / Condition)
- 🟢 `pkg/persistence` file store
- 🟢 `cmd/care` HTTP API, `cmd/caredemo` workflow demos, `cmd/bodhyangactl` operator CLI
- 🟢 Care-plan templates for HTN / DM / CHF / CKD / COPD / **asthma** (PR #6)
- 🟢 Panel rules including post-discharge follow-up + asthma controller gap (PR #6)
- 🟢 Readmission risk score (transparent LACE-inspired)

---

## Phase 3 — AI infrastructure & agentic patterns 🟢

LLM integration with safety guarantees.

- 🟢 Provider-agnostic `pkg/llm` client (Anthropic / OpenAI / Ollama)
- 🟢 Embedding adapters (`pkg/embeddings`) for Voyage / OpenAI / Ollama
- 🟢 `pkg/knowledge.HybridStore` — vector + BM25 with reciprocal-rank fusion
- 🟢 RAG-backed diagnostician with citations
- 🟢 Vision diagnostician (Claude Sonnet, gpt-4o, llama3.2-vision)
- 🟢 `pkg/eval.LLMJudge` — structured rubric with 4 dimensions
- 🟢 `pkg/safety` guardrails — refusal detection, hallucination grounding, PHI patterns
- 🟢 Agentic patterns: `DebateDiagnostician`, `ReflexionDiagnostician`, `CascadeDiagnostician`, `DiagnosticianTools` (tool-use)
- 🟢 LLM call tracing — `LLMTrace` JSON line per call
- 🟢 Prompt caching on Anthropic + OpenAI
- 🟢 Phase 2 AI capability commit: self-consistency, few-shot HITL, calibration, reranker, differential ranker (commit `1f915b6`)

---

## Phase 4 — Compliance & production infra 🟢

HIPAA / GDPR / 21 CFR Part 11 alignment as code.

- 🟢 `pkg/phi.Redactor` + `LoggerWrapper` (default Safe Harbor patterns)
- 🟢 `pkg/audit.Recorder` — in-memory + file-backed append-only
- 🟢 `pkg/audit` orchestrator wiring (PR #12) — every message produces audit events; HIPAA §164.312(b) wired
- 🟢 `pkg/hitl.Queue` + `human_review` agent — 21 CFR §3060 CDS carve-out criterion 4 expressed in code
- 🟢 `RequireTenantPolicy` + `RequireScopePolicy` (SMART-on-FHIR scope check)
- 🟢 PostgreSQL persistence with tenant RLS (PR #10) — `SELECT+INSERT` only at DB grants for audit_events
- 🟢 OpenTelemetry OTLP export, build-tag isolated (PR #7) — sha256-hashed `case_id` in spans, never raw MRN
- 🟢 Provenance type for diagnosis proposals
- 🟢 Red-team eval catalog (`pkg/eval/redteam`)

---

## Phase 5 — Healthcare interop expansion 🟢

Hospital-grade ingest paths and bidirectional data sharing.

- 🟢 HL7 v2 ADT parser + `POST /hl7/adt` (PR #8) — MSH/EVN/PID/PV1/DG1; ADT^A01/A02/A03/A04/A08
- 🟢 SMART on FHIR auth — RS256 JWT + JWKS + SMARTScopePolicy (PR #43) — `RequireScopePolicy` shipped; auth server integration pending
- 🟢 HL7 v2 ORU (lab results) — OBR/OBX parser + critical flags (PR #41)
- 🟢 HL7 v2 ORM (orders) — outbound lab orders + encode/parse round-trip (PR #43)
- ⚪ HL7 v2 SIU (scheduling)
- 🟢 FHIR Bulk Data `$export` — async job → poll → NDJSON download (PR #43)
- ⚪ FHIR Bulk Data `$import` for outbound care-plan publish
- ⚪ DICOMweb client for `radiologist` agent
- ⚪ NCPDP SCRIPT — Surescripts integration for refills / NewRx
- ⚪ X12 EDI — 270/271 eligibility, 837 claims, 835 ERA, 278 prior auth
- ⚪ CDA / C-CDA discharge summary export
- ⚪ Direct Project S/MIME secure clinician messaging

---

## Phase 6 — Clinical breadth 🟢

More specialties, more workflows, more conditions.

- 🟢 5 specialty diagnosis rules (PR #11): ACS / sepsis / DKA / CHF / CAP. Bench at 85.7% from 42.9%.
- 🟢 Rule diagnostician for PE — bench ~90%+ (PR #41)
- 🟢 `RadiologistAgent` — critical finding detection from imaging reports (PR #41)
- 🟢 `CardiologistAgent` — ECG, STEMI, TIMI score, CHF class (PR #41)
- 🟢 `InfectiousDiseaseAgent` — Sanford Guide-style antibiotic selection (PR #41)
- 🟢 `PharmacistAgent` — top-10 drug interaction checker (PR #41)
- 🟢 Care-plan templates: depression (PHQ-9), post-MI, post-stroke, pediatric well-child (PR #41)
- 🟢 Panel rule: PHQ-9 overdue (>90 days for depression) (PR #41)
- 🟢 Pediatric: age-based vaccine schedule + weight-based dosing (PR #41)

---

## Phase 7 — Evaluation & research 🟢

Reproducible measurement and adversarial robustness.

- 🟢 `cmd/bench` manifest runner with Pareto column + baseline regression gating (PR #9)
- 🟢 Synthetic bench fixtures: ACS, septic UTI, DKA, elderly CAP (PR #9)
- 🟢 LLM-as-judge structured rubric
- 🟢 Red-team eval catalogue
- 🟡 URI verifier finalize fix — `demo_uri.json` currently times out at the reasoning_verifier reject path (PR #11 deferred)
- ⚪ NEJM SD-Bench case loader (license question first)
- ⚪ Pareto plot export (PNG / SVG)
- 🟢 Equity audit pipeline — stratified accuracy by demographics (PR #41)
- 🟢 Drift monitor — per-condition accuracy with warning/critical alerts (PR #41)
- 🟢 Shadow mode — async/sync concordance capture (PR #44)
- ⚪ IRB-eligible pragmatic trial protocol template
- ⚪ Bodhyanga-Bench: open red-team cases + scoring rubric for the community
- 🟢 Federated eval — cross-site accuracy with Laplace noise (PR #44)
- 🟢 **MAF-go substrate slice** — bodhyanga-maf vendored under [`maf/`](maf/): the MARA pipeline reimplemented on `microsoft/agent-framework-go`, importing bodhyanga's compliance packages unchanged, to test whether MAF-go can carry the safety boundary. All five exit criteria proven by tests (nested Go module, built/tested separately)

---

## Phase 8 — Production hardening 🟢

The "what would it take to actually deploy" tier.

- 🟢 PostgreSQL persistence (PR #10)
- 🟢 OpenTelemetry export (PR #7)
- 🟢 Audit pipeline wired (PR #12)
- 🟢 Column-level encryption — AES-256-GCM envelope on PHI columns (PR #43)
- 🟢 NATS JetStream bus — build-tag isolated adapter (PR #42)
- ⚪ Kafka bus
- 🟢 Immutable audit sink — Merkle SHA-256 chain-hash + S3 export stub (PR #43)
- 🟢 Merkle chain-hash — tamper-evident audit with VerifyChain() (PR #43)
- 🟢 Helm chart — distroless, NetworkPolicy, HPA, PDB, ExternalSecrets (PR #43)
- ⚪ Per-tenant cell architecture documentation + reference deployment
- 🟢 RateLimitPolicy — token bucket per-tenant + per-agent (PR #41)
- ⚪ Cost / latency SLO dashboards (Prometheus + Grafana)
- ⚪ Output-fanout audit hooks (PR #12 deferred — currently only inbound hop audited)
- 🟢 /audit cursor-based pagination endpoint (PR #41)
- ⚪ Per-request tenant routing on `/audit` (single-tenant today)
- 🟢 .gitignore for all build artifacts (PR #45)

---

## Phase 9 — Compliance maturity ⚪

Beyond §164.312(b) wired, the deeper regulatory work.

- 🟢 `RedactNarrativePolicy` — strip HPI/plan from log-bound copies (PR #41)
- 🟢 `Part2ConsentPolicy` — 42 CFR Part 2 SUD record protection (PR #41)
- 🟢 `BreakGlassPolicy` — audited emergency override with mandatory audit (PR #41)
- ⚪ `RequireMinimumNecessaryPolicy` — per-agent field allowlist
- ⚪ 21 CFR Part 11 e-signature support for trial deployments
- ⚪ Cryptographic timestamping (RFC 3161) on audit events
- ⚪ ONC Certification artifacts — USCDI conformance, SMART App Launch test suite, Bulk Data conformance
- ⚪ HITRUST CSF / SOC 2 Type II evidence pipeline — auditor-ready control mapping from code → policy → evidence
- ⚪ GDPR DPIA template — Article 35 required for AI-based CDS in EU
- ⚪ Right-to-delete tooling — hard-delete by `patient_id` across audit + persistence + backups (HIPAA vs GDPR tension to navigate)
- ⚪ Annual bias audit pipeline — stratified accuracy with published report

---

## Phase 10 — User experience ⚪

For the actual humans interacting with the system.

- ⚪ Clinician dashboard — case timeline, HITL review queue, panel report viewer
- ⚪ Patient portal — read-only care plan, gap list, RPM trends
- ⚪ Operator UI — audit log browser, policy decision dashboard
- ⚪ Ambient scribe integration — voice → `PatientPresentation`
- ⚪ Multimodal intake beyond chest X-ray — skin lesions, fundus photography, ECG strips

---

## Cross-cutting: testability and reproducibility

These aren't a phase — they're a continuous expectation.

- 🟢 Table-driven tests for governance, medical domain, tasks, engagement, fhir, analytics, persistence, eval
- 🟢 Postgres integration tests via testcontainers
- 🟢 Orchestrator audit + eval integration tests
- 🟢 HL7 v2 parser test fixtures
- 🟡 Per-agent table-driven `HandleMessage` tests — many but not yet all agents
- ⚪ E2E test for the full HTTP API surface
- ⚪ Performance benchmark — message throughput per process
- ⚪ Chaos test — random agent panics, bus drops, policy errors; assert no PHI leaks

---

## Decision log

| Decision | When | Why |
|---|---|---|
| Use MARA pattern (orchestrator + agents + registry + bus + governance) | Phase 1 | Auditability, swappability, single point of policy enforcement |
| Agents never call each other directly | Phase 1 | Routing in one place; trivial to add observability and audit |
| Stateless agents | Phase 1 | Horizontal scaling without coordination; testability |
| In-memory bus default, interface for production swaps | Phase 1 | Demos run with zero infra; production picks Kafka/NATS without agent changes |
| Append-only audit at DB GRANTS | Phase 4 / PR #10 | Defence in depth — even compromised app credentials can't tamper with audit |
| Sha256-hashed `case_id` in OTel spans | Phase 4 / PR #7 | Spans go to third-party collectors; raw IDs could be MRNs |
| LLM agents always have a rule-based fallback | Phase 3 | Case always finalizes; degrade gracefully, never break loudly |
| Optional setters for audit / eval on orchestrator (not new constructor) | PR #12 | Backwards-compat for `cmd/demo` / `cmd/caredemo` |
| Postgres backend opt-in via `BODHYANGA_DB_DSN` | PR #10 | Demos stay zero-infra; production a single env var away |
| OTel SDK isolated behind `//go:build otel` | PR #7 | Default `go.sum` stays minimal; SDK loaded only when needed |

---

## Reading this roadmap

The phases are roughly the order things were built, not a Gantt chart. Most phases overlap; some items get reordered when reality pushes back (asthma was Phase 6 in design but shipped in Phase 4 as a "prove the composition pattern" PR).

If you want to take on a ⚪ Planned item, open an issue describing your approach first — especially for anything that touches `pkg/protocol`, `pkg/agent`, `pkg/orchestration`, or `pkg/audit`. The smaller `pkg/medical` extensions and care-plan templates rarely need design discussion; just send a PR following the asthma template (PR #6).
