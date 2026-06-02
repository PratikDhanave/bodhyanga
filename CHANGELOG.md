# Changelog

All notable changes to Bodh are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

---

## [Unreleased]

### Added
- Ambient clinical scribe — NLP pipeline converting voice transcripts to structured PatientPresentation (`pkg/medical/scribe.go`)
- Shadow mode deployment — parallel AI evaluation with concordance capture (`pkg/shadow/`)
- Cost-based model routing — budget-aware 3-tier cascade with hard caps (`pkg/llm/budget.go`)
- Multi-channel notification service — Slack, Teams, PagerDuty, email (`pkg/notifications/`)
- Federated evaluation — cross-site accuracy benchmarks with differential privacy (`pkg/eval/federated/`)
- Patient portal i18n — English, Spanish, French, Mandarin Chinese (`cmd/care/web/src/i18n/`)
- OSS standard files — LICENSE (Apache 2.0), SECURITY.md, CODE_OF_CONDUCT.md, CHANGELOG.md, issue/PR templates

---

## [0.9.0] - 2026-06-01

### Added
- **AES-256-GCM column-level encryption** for PHI at rest (`pkg/persistence/crypto.go`)
- **SMART on FHIR** RS256 JWT validation with JWKS caching (`pkg/auth/smart.go`)
- **Immutable audit sink** with Merkle SHA-256 chain-hash (`pkg/audit/immutable.go`)
- **FHIR Bulk Data `$export`** — async job → poll → NDJSON download (`pkg/fhir/bulk.go`)
- **HL7 v2 ORM^O01** outbound lab orders — encode/parse round-trip (`pkg/hl7v2/orm.go`)
- **4 specialist agents**: Neurologist (NIHSS, stroke, seizure), Nephrologist (CKD-EPI, KDIGO, AKI), Pulmonologist (PFT, COPD GOLD, asthma), Ophthalmologist (retinopathy ETDRS, glaucoma, DME)
- **Helm chart** — distroless image, NetworkPolicy, HPA, PDB, ExternalSecrets (`zarf/helm/bodh/`)
- **Performance benchmarks** — bus throughput, orchestrator latency (`pkg/bench/`)
- **Chaos tests** — agent panic recovery, PHI-leak assertion, race detector (`pkg/chaos/`)
- **E2E Playwright tests** — dashboard, auth, reviews, case runner, audit log
- `cmd/audit-verify` CLI for Merkle chain integrity verification

---

## [0.8.0] - 2026-06-01

### Added
- **WebSocket real-time HITL** — live push notifications for clinician reviews (`pkg/hitl/hub.go`)
- **CDS Hooks server** — patient-view, order-sign, medication-prescribe, encounter-discharge (`pkg/cdshooks/`)
- **JWT authentication** — login endpoint, auth middleware, token validation (`pkg/auth/login.go`)
- **LLM cost accounting** — per-call spend tracking, `/api/analytics/cost` (`pkg/analytics/cost.go`)
- **Webhook dispatcher** — HMAC-SHA256 signed event delivery with retry (`pkg/webhooks/`)
- **NEWS2 early warning score** — 10-component national early warning (`pkg/medical/news2.go`)
- **NATS JetStream bus** adapter (build tag `nats`) (`pkg/comm/nats.go`)
- **Fine-tuning pipeline** — validate/split/estimate/upload (`pkg/finetune/`)
- **OncologyAgent** — chemo protocols (NSCLC, breast, colon, lymphoma), BSA dosing
- **ClinicalTrialScreener** — eligibility matching + ClinicalTrials.gov API tool
- **Drug dosing tools** — weight-based, renal-adjusted (Cockcroft-Gault), pediatric
- Frontend: Login page, Cost dashboard, dark mode, notification bell, toast alerts

---

## [0.7.0] - 2026-05-31

### Added
- **PE diagnostic rule** — closes last rule-path specialty gap (bench ~90%+)
- **PharmacistAgent** — top-10 drug-drug interaction checker
- **CardiologistAgent** — ECG interpretation, STEMI detection, TIMI score, CHF class
- **RadiologistAgent** — critical finding detection from imaging reports
- **InfectiousDiseaseAgent** — antibiotic stewardship (Sanford Guide-style)
- **PHQ-9 depression** panel rule + stepped care plan
- Post-MI + post-stroke care plan templates
- Pediatric well-child care plan (age-based vaccines, weight-based dosing)
- **HL7 v2 ORU^R01** lab results parser (OBR/OBX segments)
- **42 CFR Part 2** consent policy (SUD record protection)
- **BreakGlassPolicy** — audited emergency override with mandatory audit trail
- **RedactNarrativePolicy** — strip HPI/plan from log-bound messages
- **RateLimitPolicy** — token bucket per-tenant + per-agent
- Audit pagination — cursor-based `GET /api/audit`
- Equity audit pipeline — stratified accuracy by demographics (AIGP)
- Drift monitor — per-condition accuracy alerts
- Lessons 10-13: Computer Use, Long-horizon Planning, Multi-Agent Debate, Self-Correction
- Frontend: Clinician Dashboard, Patient Portal, Audit Log viewer

---

## [0.6.0] - 2026-05-29

### Added
- **Lessons 03-09**: Agent Loop, Single-Turn Evals, Multi-Turn Evals, File System Tools, Web Search, Streaming, HITL
- **Ollama GBNF grammar** wired to `options.grammar` (sampler-level constraints)
- **Full JSON Schema** in Ollama `format` field (Ollama 0.5+)
- **Streaming** — `Streamer` interface + Ollama/Anthropic/OpenAI SSE adapters
- **Redis KV** memory store — RESP2 over stdlib TCP (`pkg/memory/redis.go`)
- **A2A server** — Agent Card, task submission, SSE stream (`pkg/a2a/`)
- **MCP server** — JSON-RPC 2.0 HTTP+SSE exposing tool registry (`pkg/mcp/server/`)
- **HITL checkpointing** — flush/restore queue to JSON for pod restarts
- **Red-team evaluation** suite — adversarial safety tests (`pkg/eval/redteam/`)
- Claude Code plugins: `/commit`, `/commit-push-pr`, `/code-review`, security-guidance hooks

---

## [0.5.0] - 2026-05-29

### Added
- OPA Rego governance policy adapter (`pkg/governance/opa_policy.go`)
- Lesson 01: Intro to Agents (`cmd/intro/`)
- Lesson 02: Tool Calling (`cmd/toolcalling/`)
- Laminar OTLP exporter + TracingClient middleware (`pkg/observability/laminar.go`)
- `lmnr.evaluate()` Go equivalent (`pkg/evals/laminar/`, `cmd/laminar-eval/`)

---

## [0.4.0] - 2026-05-28

### Added
- Unified Makefile — single entry point for all workflows
- Multi-turn agent evaluation framework with LLM-as-judge
- AgentMesh kill-switch + ring + prompt-injection gate
- Prometheus `/metrics` + OTel HTTP span tracing
- `/healthz` liveness + `/readyz` readiness probes for K8s
- K8s Kustomize manifests, docker-compose stack, multistage Dockerfiles

---

## [0.3.0] - 2026-05-27

### Added
- Inline code comments across orchestration, governance, safety, bus, and agent layers
- AGENTS.md for AI coding assistants
- LinkedIn article series (11 articles)
- Blog posts published to PratikDhanave.github.io
- Model cards for LLM-backed agents
- AIIA template + incident playbook
- MCP client (JSON-RPC 2.0 over HTTP+SSE)
- `pkg/llm/kronk` provider (in-process llama.cpp)
- `pkg/llm/pool` multi-model pool

---

## [0.2.0] - 2026-05-26

### Added
- Right-to-explanation HTTP handler
- Comprehensive documentation rewrite (18 docs)
- SD-Bench regression runner with Pareto frontier
- PostgreSQL persistence with tenant RLS
- HL7 v2 ADT parser + `POST /hl7/adt`
- OpenTelemetry OTLP export for LLMTrace
- Asthma care plan template + panel rules
- Specialty diagnostic rules (ACS, sepsis, DKA, CHF)
- Audit/eval wiring into orchestrator

---

## [0.1.0] - 2026-05-24

### Added
- MARA platform: orchestrator, bus, registry, governance
- 8 diagnostic agents (intake → reasoning_verifier)
- 9 care-services agents (CDM, TCM, panel, inbox, refill, prior auth, engagement, readmission)
- `pkg/llm` provider abstraction (Anthropic, OpenAI, Ollama)
- `pkg/knowledge` hybrid RAG (BM25 + vector)
- `pkg/safety` guardrails (refusal, hallucination, PHI)
- `pkg/audit` append-only HIPAA audit log
- `pkg/hitl` human-in-the-loop review queue
- `cmd/care` HTTP API + Vue 3 SPA
- `cmd/demo` sequential diagnosis walkthrough
