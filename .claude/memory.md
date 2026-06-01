# Bodh — Session Memory
_Last updated: 2026-06-01_

---

## Project identity

- **Repo:** `github.com/PratikDhanave/bodh`
- **Local path:** `/Users/genesis/Developer/goworkspace/src/github.com/PratikDhanave/bodh`
- **Module:** `github.com/PratikDhanave/bodh` · Go 1.26
- **Description:** HIPAA-grade medical AI orchestration platform (MARA architecture)
- **Current branch:** `feat/production-hardening` (active workflow running)
- **Test status:** 56 packages, 0 failures on `main`

---

## What was built this session (PRs #36–#42 + commits)

### PR #36 — OPA Rego policy adapter
- `pkg/governance/opa_policy.go` — OPA-backed governance using Rego rules
- `pkg/governance/rego/medical.rego` — medical domain Rego policies

### PR #37 — Lesson 01: Intro to Agents
- `pkg/agent/intro/` + `cmd/intro/` — simplest LLM call, no tools, no loop

### PR #38 — Lesson 02: Tool Calling
- `pkg/agent/toolcalling/` + `cmd/toolcalling/` — advertise tools, detect model selection

### PR #39 — Laminar OTLP exporter
- `pkg/observability/laminar.go` — OTLP spans with `lmnr.span.type=LLM` attributes
- `pkg/observability/llm_client.go` — `TracingClient` transparent wrapper

### PR #40 — All agentic AI skills (lessons 03–09 + infrastructure)
Lessons:
- Lesson 03 Agent Loop — `pkg/agent/agentloop/` + `cmd/agentloop/`
- Lesson 04 Single-Turn Evals — `pkg/agent/singleturneval/` + `cmd/singleturneval/`
- Lesson 05 Multi-Turn Evals — `pkg/agent/multiturneval/` + `cmd/multiturneval/`
- Lesson 06 File System Tools — `pkg/tools/filesystem.go` + `cmd/filesystemtools/`
- Lesson 07 Web Search — `pkg/tools/websearch.go` + `cmd/websearchtools/`
- Lesson 08 Streaming — `pkg/llm/stream.go` + adapters + `cmd/streaming/`
- Lesson 09 HITL — `cmd/hitl/`

Infrastructure:
- `pkg/llm/stream.go` — `Streamer` interface + `AsStreamer()` helper
- `pkg/memory/redis.go` — RESP2 KeyValueStore (stdlib only, no deps)
- `pkg/a2a/` + `cmd/a2aserver/` — A2A protocol server (Agent Card + tasks)
- `pkg/mcp/server/` + `cmd/mcpserver/` — MCP server exposing tools registry
- `pkg/hitl/checkpoint.go` — flush/restore HITL queue to JSON (pod restarts)
- `pkg/eval/redteam/` + `cmd/redteameval/` — adversarial red-team suite
- **Ollama compatibility:** GBNF → `options.grammar`, full JSON Schema in `format`, streaming

### PR #41 — Full platform extension
Clinical agents:
- PE diagnostic rule (closes rule-path gap; bench ~90%+)
- `agents/pharmacist.go` — drug-drug interaction checker (top 10 pairs)
- `agents/cardiologist.go` — ECG, STEMI, TIMI, CHF class
- `agents/radiologist.go` — critical finding detection from imaging
- `agents/infectious_disease.go` — antibiotic stewardship (Sanford Guide-style)
- PHQ-9 depression panel rule + care plan
- Post-MI + post-stroke care plans
- Pediatric well-child care plan (age-based vaccines, weight-based dosing)

Healthcare interop:
- `pkg/hl7v2/oru.go` — HL7 v2 ORU^R01 lab results ingest (OBR/OBX)
- `cmd/hl7oru/` — CLI parser

Compliance policies:
- `pkg/governance/part2_policy.go` — 42 CFR Part 2 SUD protection
- `pkg/governance/break_glass_policy.go` — audited emergency override
- `pkg/governance/redact_narrative_policy.go` — strip PHI from log-bound copies
- `pkg/governance/rate_limit_policy.go` — token bucket per-tenant + per-agent

Infrastructure:
- `pkg/audit/pagination.go` — cursor-based `/api/audit`
- `pkg/eval/equity/` + `cmd/equity-eval/` — stratified accuracy by demographics
- `pkg/eval/drift/` + `cmd/drift-monitor/` — accuracy drift monitoring

Lessons 10–13:
- Lesson 10 Computer Use — `pkg/tools/computer.go` + `cmd/computeruse/`
- Lesson 11 Long-horizon Planning — `pkg/agent/planning/` + `cmd/planning/`
- Lesson 12 Multi-Agent Debate — `pkg/agent/debate/` + `cmd/debate/`
- Lesson 13 Self-Correction — `pkg/agent/selfcorrect/` + `cmd/selfcorrect/`

Frontend (React/TypeScript SPA):
- `ClinicianDashboard.tsx` — summary cards, HITL queue, case table
- `PatientPortal.tsx` — care plan, panel gaps, RPM trends
- `AuditLog.tsx` — paginated audit events with filters

### PR #42 — WebSocket HITL, CDS Hooks, JWT, cost dashboard, NEWS2, oncology, fine-tuning
Backend:
- `pkg/hitl/hub.go` — WebSocket broadcast hub (real-time HITL push)
- `pkg/cdshooks/` + `cmd/cdshooks/` — CDS Hooks server (:8096) — patient-view, order-sign, medication-prescribe, encounter-discharge
- `pkg/auth/login.go` — JWT credential store, bcrypt, HS256 token
- `pkg/analytics/cost.go` — LLM spend ledger, `/api/analytics/cost`
- `pkg/webhooks/` — HMAC-SHA256 signed webhook delivery with retry
- `pkg/medical/news2.go` — NEWS2 early warning score (10 components)
- `pkg/comm/nats.go` + `nats_stub.go` — NATS JetStream bus adapter (`-tags nats`)
- `pkg/finetune/` + `cmd/finetune/` — validate/split/estimate/upload pipeline

New agents:
- `agents/oncologist.go` — chemo protocols (NSCLC, breast, colon, lymphoma), BSA dosing
- `agents/trial_screener.go` — clinical trial eligibility + ClinicalTrials.gov tool
- `pkg/tools/dosing.go` — weight-based, renal-adjusted (Cockcroft-Gault), pediatric
- `pkg/tools/ctgov.go` — ClinicalTrials.gov search tool

Frontend additions:
- `Login.tsx` — JWT login, auth guard on all routes
- `Cost.tsx` — spend dashboard, SVG sparklines, Pareto chart
- `NotificationBell.tsx` — real-time HITL alerts via WebSocket
- `Toast.tsx` — slide-in toast notifications
- Dark mode — tailwind `darkMode: class`, toggle in Layout

### Commit 99afd42 — Claude Code plugins
Three plugins installed in `.claude/`:
- `commit-commands` → `/commit`, `/commit-push-pr`, `/clean_gone`
- `code-review` → `/code-review [--comment]` (5-agent Opus review)
- `security-guidance` → hooks on PostToolUse/Stop scanning 25+ vuln classes

---

## Active work

**Branch:** `feat/production-hardening`
**Workflow running:** `wf_de496243-24e` (12 agents)

What's being built right now:
- Column-level AES-256-GCM encryption (`pkg/persistence/crypto.go`)
- SMART on FHIR auth (`pkg/auth/smart.go` + `SMARTScopePolicy`)
- Immutable audit sink with Merkle chain-hash (`pkg/audit/immutable.go`)
- FHIR Bulk Data `$export` (`pkg/fhir/bulk.go`)
- HL7 v2 ORM outbound orders (`pkg/hl7v2/orm.go`)
- NeurologyAgent (stroke NIH, tPA window, seizure classification)
- NephrologyAgent (CKD-EPI eGFR, KDIGO staging, AKI)
- PulmonologyAgent (PFT, COPD GOLD, asthma severity)
- OphthalmologyAgent (diabetic retinopathy ETDRS, glaucoma, DME)
- Helm chart (`zarf/helm/bodh/`) — distroless, NetworkPolicy, HPA, PDB
- Performance benchmarks (`pkg/bench/`)
- E2E Playwright tests (dashboard, auth, reviews, case runner)
- Chaos tests (panic recovery, PHI-leak assertion, concurrent load)

---

## Key technical facts

### Ports
| Port | Service |
|---|---|
| 8088 | Bodh care API + SPA |
| 8096 | CDS Hooks server |
| 8097 | MCP server |
| 8099 | A2A server |
| 5667 | Laminar web UI (NOT the API) |
| 8000 | Laminar REST API (`LMNR_BASE_URL`) |
| 8001 | Laminar OTLP/gRPC |

### Build tags
- `otel` — OpenTelemetry SDK (OTLP export, Laminar)
- `kronk` — proprietary in-process llama.cpp provider
- `nats` — NATS JetStream bus adapter

### PHI security invariants
- `bodh.case_id` on spans = `sha256(caseID)[:8]` hex — never raw MRN
- Audit events: reason field is redacted, never contains diagnosis/HPI/lab values
- BODH_ENCRYPTION_KEY (base64, 32 bytes) → AES-256-GCM on all PHI columns

### Default providers + models
- Ollama default: `qwen2.5:7b` (lessons), `llama3.2` (care)
- Anthropic default: `claude-sonnet-4-6`
- OpenAI default: `gpt-4o`

### Environment key vars
```bash
ANTHROPIC_API_KEY       # llm-anthropic diagnostician
OPENAI_API_KEY          # llm-openai diagnostician
OLLAMA_HOST             # default http://localhost:11434
LMNR_PROJECT_API_KEY    # Laminar (use local key from localhost:5667)
LMNR_BASE_URL           # http://localhost:8000 for self-hosted
DATABASE_URL            # PostgreSQL DSN (optional, enables Postgres backend)
BODH_ENCRYPTION_KEY     # base64 AES-256 key for PHI column encryption
BODH_JWT_SECRET         # JWT signing secret (default: bodh-dev-secret-change-in-prod)
NATS_URL                # nats://localhost:4222 (only with -tags nats)
```

---

## Packages overview

```
agents/          28+ clinical agents (diagnostic + care-services + specialist)
cmd/             18+ CLI binaries
pkg/
  a2a/           A2A wire protocol server
  agent/         agent interface + lesson packages (intro, toolcalling, agentloop, ...)
  analytics/     cost ledger, cohort metrics
  audit/         append-only log, immutable sink, pagination
  auth/          OPA Rego + JWT + SMART on FHIR
  cdshooks/      CDS Hooks server (EHR integration)
  comm/          InMemoryBus + NATS adapter
  eval/          single-turn, equity, drift, redteam
  evals/         laminar + multiturn
  finetune/      SFT/DPO export + OpenAI upload
  fhir/          R4 types + Bulk $export
  governance/    CompositePolicy + 8 policies (OPA, Part2, BreakGlass, ...)
  hitl/          Queue + Hub (WebSocket) + Checkpoint
  hl7v2/         ADT + ORU + ORM parsers
  llm/           4 providers + streaming + GBNF grammar
  mcp/           client + server
  medical/       care plans (10+ templates), NEWS2, panel rules
  memory/        InMemoryKV + Redis adapter
  observability/ OTel + Laminar exporter
  orchestration/ MARA orchestrator + message bus
  persistence/   file + postgres (with encryption)
  phi/           PHI redaction
  safety/        refusal detection, hallucination, PHI patterns
  tools/         drug interaction, FHIR query, filesystem, websearch, computer, dosing, ctgov
  webhooks/      HMAC-signed event delivery
```

---

## Claude Code setup

### Settings files
- `.claude/settings.json` — tracked, 7 core permissions + security-guidance hooks
- `.claude/settings.local.json` — gitignored, 140 permissions (all go/git/make/npm/docker commands)

### Installed plugins
- `/commit` — stage + commit in one shot
- `/commit-push-pr` — commit, push, open PR in one shot
- `/clean_gone` — prune deleted remote branches
- `/code-review [--comment]` — 5-agent Opus PR review
- Security guidance — auto-runs on every file edit + commit + push

### Session isolation tip
**One terminal per project.** Run `/clear` before switching tasks.
Context is scoped to working directory — never switch dirs mid-session.

---

## What's still remaining (roadmap)

### High priority
- [ ] Workflow running: production-hardening (Helm, encryption, SMART, 4 specialist agents)
- [ ] FHIR Bulk $export (in workflow)
- [ ] HL7 ORM outbound orders (in workflow)

### Next after production-hardening merges
- [ ] Right-to-delete tooling (hard-delete patient_id across all stores)
- [ ] 21 CFR Part 11 e-signature
- [ ] GDPR DPIA template
- [ ] Federated eval (cross-site accuracy without data sharing)
- [ ] Shadow mode kit (silent prospective deployment)
- [ ] Ambient scribe (voice → PatientPresentation)
- [ ] Multi-language patient portal (Spanish, French)
- [ ] NCPDP SCRIPT e-prescribing
- [ ] X12 270/271 real-time eligibility

---

## LinkedIn article series

Repo: `github.com/PratikDhanave/https-pratikdhanave.github.io`
Local: `/Users/genesis/Developer/goworkspace/src/github.com/PratikDhanave/bodh-linkedin`
PR #1 merged: 11 technical articles (00–10) on Bodh's Go healthcare AI implementation
