# Bodhyanga — Session Memory
_Last updated: 2026-06-02_

---

## Project identity

- **Repo:** `github.com/PratikDhanave/bodhyanga`
- **Local path:** `/Users/genesis/Developer/goworkspace/src/github.com/PratikDhanave/bodhyanga`
- **Module:** `github.com/PratikDhanave/bodhyanga` · Go 1.26
- **Current branch:** `feat/final-extensions` (ready to commit + PR)
- **Stats:** 61 test packages · 37 agent files · 35 cmd binaries · 42 pkg packages · 71 commits on main

---

## Merged PRs (this session series)

| PR | Title | What it added |
|---|---|---|
| #36 | OPA Rego policy adapter | `pkg/governance/opa_policy.go`, Rego rules |
| #37 | Lesson 01: Intro to Agents | `pkg/agent/intro/`, `cmd/intro/` |
| #38 | Lesson 02: Tool Calling | `pkg/agent/toolcalling/`, `cmd/toolcalling/` |
| #39 | Laminar OTLP exporter | `pkg/observability/laminar.go`, `TracingClient` |
| #40 | All agentic AI skills (03-09) | Lessons 03-09, streaming, A2A, MCP server, Redis KV, HITL checkpoint, red-team eval, Ollama GBNF/streaming fix |
| #41 | Full platform extension | PE rule, Pharmacist, Cardiologist, Radiologist, ID agent, PHQ-9, post-MI/stroke, pediatric, HL7 ORU, Part2/BreakGlass/Redact/RateLimit policies, equity/drift eval, lessons 10-13, frontend pages |
| #42 | WebSocket HITL, CDS Hooks, JWT, cost, NEWS2, oncology, fine-tuning | Hub, CDS Hooks server, JWT auth, cost ledger, webhooks, NEWS2 score, NATS bus, fine-tune pipeline, oncology agent, trial screener, dosing tools, Login/Cost/dark mode/notifications UI |
| #43 | Production hardening | AES-256-GCM encryption, SMART on FHIR, immutable audit (Merkle chain), FHIR Bulk $export, HL7 ORM, neurologist/nephrologist/pulmonologist/ophthalmologist, Helm chart, perf benchmarks, E2E Playwright, chaos tests |
| (pending) | Final extensions | Ambient scribe, shadow mode, cost-based routing, notifications service, federated eval, i18n (en/es/fr/zh) |

---

## Current work on feat/final-extensions (uncommitted)

Applied from workflow worktrees, builds clean, tests pass (61 packages):
- `pkg/medical/scribe.go` — ambient scribe NLP (vitals/meds/allergies regex + LLM extraction)
- `cmd/scribe/` — CLI for transcript → structured PatientPresentation
- `pkg/shadow/` + `cmd/shadow/` — shadow mode (async/sync concordance capture)
- `pkg/llm/budget.go` — cost-based model routing (3-tier cascade with hard caps)
- `pkg/notifications/` — multi-channel dispatcher (Slack/Teams/PagerDuty/email)
- `pkg/eval/federated/` + `cmd/federated-eval/` — cross-site eval with differential privacy
- `cmd/care/web/src/i18n/` — i18n (en/es/fr/zh) with React context + localStorage

Also in progress (OSS audit):
- `LICENSE` — Apache 2.0 (written)
- `SECURITY.md` — vulnerability reporting + PHI security overview (written)
- Still need: CODE_OF_CONDUCT.md, CHANGELOG.md, issue templates, PR template, CODEOWNERS, .editorconfig

---

## Key technical facts

### Architecture
- MARA pipeline: Intake → Test Planner → Diagnostician → Care Plan Generator
- Bus: InMemoryBus (default) or NATS JetStream (`-tags nats`)
- Governance: CompositePolicy (deny-first) gates every message before agents
- Audit: append-only, PHI-free, Merkle chain-hash, S3 export stub

### Clinical agents (37 implementations)
Diagnostic: intake, rpm_monitor, supervisor, questioner, test_planner, cost_guardian, diagnostician (rule + 6 LLM variants), reasoning_verifier, human_review
Specialists: cardiologist, radiologist, pharmacist, infectious_disease, neurologist, nephrologist, pulmonologist, ophthalmologist, oncologist, trial_screener
Care services: cdm_planner, tcm_coordinator, panel_manager, pre_visit_prep, inbox_triage, refill_manager, prior_auth, engagement, readmission_tracker
Support: audio_transcriber

### LLM providers
Anthropic (claude-sonnet-4-6), OpenAI (gpt-4o), Ollama (qwen2.5:7b/llama3.2), Kronk (in-process llama.cpp)
Streaming: all 3 via `Streamer` interface
GBNF grammar: Ollama (`options.grammar`) + Kronk (native)
Budget routing: 3-tier cascade (Ollama→small-cloud→large-cloud)

### Build tags
`otel` — OpenTelemetry SDK, Laminar exporter
`kronk` — proprietary llama.cpp provider
`nats` — NATS JetStream bus adapter

### Ports
8088=care API, 8096=CDS Hooks, 8097=MCP server, 8099=A2A server, 5667=Laminar UI, 8000=Laminar API

### PHI security invariants
- `bodhyanga.case_id` on spans = `sha256(caseID)[:8]` hex
- Audit events: reason field redacted, never diagnosis/HPI/labs
- `BODHYANGA_ENCRYPTION_KEY` (base64, 32 bytes) → AES-256-GCM on PHI columns
- SMART on FHIR: RS256 JWT + JWKS caching for EHR integration

### Frontend (React/TypeScript SPA)
18 pages: Dashboard, ClinicianDashboard, PatientPortal, AuditLog, Login, Cost, CaseRunner, RunTrace, Reviews, ReviewDetail, Tasks, Patients, PanelReport, Agents, RPM, Bench, LLMTraces, FineTune, About
Features: JWT auth guard, dark mode, notification bell (WebSocket), toast alerts, Pareto charts, RPM sparklines, i18n (en/es/fr/zh)

### Claude Code setup
- `.claude/settings.json` — 7 permissions + security-guidance hooks
- `.claude/settings.local.json` — 140+ permission entries (gitignored)
- `.claude/commands/` — /commit, /commit-push-pr, /clean_gone, /code-review
- `.claude/plugins/security-guidance/` — PreToolUse/PostToolUse/Stop hooks (25+ vuln classes)

---

## What's remaining (post-final-extensions)

### Needs real external systems (can't stub)
- Epic/Cerner FHIR connector (OAuth2 client registration)
- Surescripts NCPDP SCRIPT (credentials)
- X12 EDI 270/271/837/835 (clearinghouse)
- AWS/GCP KMS (cloud account)
- Twilio/SendGrid (API key + phone)

### Needs legal/policy decisions
- GDPR DPIA template (Article 35)
- Right-to-delete (HIPAA 6yr retention vs GDPR erasure)
- 21 CFR Part 11 e-signatures (FDA pathway)
- ONC Certification (accredited lab)
- SOC 2 Type II (auditor + 6-month window)

### OSS documentation (in progress this session)
- [x] LICENSE (Apache 2.0)
- [x] SECURITY.md
- [ ] CODE_OF_CONDUCT.md
- [ ] CHANGELOG.md
- [ ] .github/ISSUE_TEMPLATE/bug_report.md
- [ ] .github/ISSUE_TEMPLATE/feature_request.md
- [ ] .github/PULL_REQUEST_TEMPLATE.md
- [ ] .github/CODEOWNERS
- [ ] .editorconfig
