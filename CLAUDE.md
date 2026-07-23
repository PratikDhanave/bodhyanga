# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

Bodh is a **HIPAA-aware reference architecture** for medical AI orchestration, written in Go — a
**research and education codebase, not a medical device and not for clinical use** (see `NOTICE`).
Source-available under **PolyForm Noncommercial 1.0.0** (relicensed from Apache-2.0).
It implements the Multi-Agent Reference Architecture (MARA) for clinical decision *support*:
intake → test-planning → diagnostics → care-plan generation, with audit trails, human-in-the-loop
feedback, and LLM-agnostic provider abstraction.

> **Identity note (ADR-0006):** the running binary defaults to research-grade substrate — an
> in-process bus, in-memory audit, and no auth middleware wired in `cmd/care`. Treat "HIPAA-aware"
> as *architectural intent*, not certified compliance. Clinical-grade controls (tamper-evident
> audit, authenticated API, durable bus) are a documented roadmap, not the current default.

> **Also read `AGENTS.md`.** It is the dense, assistant-facing field guide and encodes three
> **load-bearing invariants that get changes reverted** — summarized under "Invariants" below.
> This file (`CLAUDE.md`) covers commands + architecture; `AGENTS.md` covers what must never break.

---

## Quick reference

```bash
make test          # run all Go tests
make build         # compile all binaries
make care          # start HTTP API + embedded React SPA on :8088
make demo          # sequential diagnosis demo (Ollama)
make evals         # multi-turn agent evaluation
make laminar-eval  # lmnr.evaluate() equivalent
make cds-hooks     # CDS Hooks service for EHR integration
make finetune      # fine-tuning pipeline (validate/split/upload)
make help          # full target listing with variable overrides
```

---

## Build commands

| Command | Purpose |
|---|---|
| `make test` | `go test ./...` (no build tags) |
| `make test-otel` | tests that require `//go:build otel` |
| `make race` | `go test -race ./...` |
| `make vet` | `go vet ./...` |
| `make lint` | vet + staticcheck |
| `make cover` | HTML coverage report |
| `make ci` | vet + test + govulncheck |
| `make tidy` | `go mod tidy` |
| `make fmt` | `go fmt ./...` |
| `make clean` | remove build artifacts |

### Build tags

Three optional tags gate heavyweight, proprietary, or alternate-transport code:

```bash
# OpenTelemetry + Laminar tracing
go build -tags otel ./cmd/care

# Kronk (proprietary) LLM / audio / rerank providers
go build -tags kronk ./cmd/care

# NATS bus transport (pkg/comm) — alternative to the in-process bus
go build -tags nats ./...

# Combine
go build -tags "otel kronk" ./cmd/care
```

Default (`go build ./...`) compiles without any tag. Every tagged file has a
`_stub.go` counterpart (e.g. `laminar_stub.go`, `kronk_stub.go`) so the project always
builds cleanly without those tags.

**Tagged code is invisible to default CI, so it silently rots** (this happened to the
`nats` bus — see ADR-0011). Run `make build-tags` to compile-check the default + `otel`
build combinations before relying on them.

---

## Module

```
module  github.com/PratikDhanave/bodh
go      1.26
```

Key direct dependencies: `jackc/pgx/v5`, `google/uuid`, `testcontainers-go`,
`go.opentelemetry.io/otel` (otel tag only).

---

## Repository layout

```
cmd/                  CLI binaries, one subdirectory per binary (~36; run `ls cmd/`)
  bench/              SD-Bench regression runner
  bodhadmin/          admin CLI (migrations, config)
  bodhctl/            control-plane CLI
  care/               HTTP API + embedded React 18 + TS SPA (web/)
  caredemo/           CDM/TCM/Panel demo
  demo/               sequential diagnosis walkthrough
  evals/              multi-turn evaluation runner
  laminar-eval/       Go equivalent of Python lmnr.evaluate()
  intro/, toolcalling/, agentloop/, ...   lesson binaries (lesson-01 … lesson-13)
  ... plus eval/demo/integration binaries (a2aserver, cdshooks, mcpserver,
      shadow, drift-monitor, redteameval, scribe, ...)

pkg/                  library packages (~46; run `ls pkg/` for the current set)
  agent/              agent orchestration core
  audio/              audio processing (+ kronk/ subpkg)
  audit/              HIPAA append-only audit log
  auth/               OPA Rego + JWT auth
  comm/               NATS bus transport (nats tag; at-least-once + DLQ)
  evals/
    laminar/          lmnr.evaluate() API client
    multiturn/        multi-turn benchmark harness
  eval/               single-turn evaluation
  gateway/            admission-control pipeline (decoupling substrate, see below)
  governance/         composable policy stack + kill switches (see below)
  graduation/         agent-autonomy lifecycle ladder (see below)
  hitl/               human-in-the-loop feedback
  hl7v2/              HL7 v2 ADT parser
  knowledge/          RAG corpus + retrieval (+ rerank/)
  llm/                LLM abstraction layer (+ kronk/, pool/)
  mcp/                Model Context Protocol client
  observability/      OTLP tracing, Laminar exporter
  orchestration/      clinical message bus
  persistence/        data layer (+ postgres/)
  phi/                PHI de-identification
  runtime/            per-agent message loop (decoupling substrate, see below)
  safety/             safety checks
  web/                HTTP handlers + routing
  ... (see pkg/ for the full list)

agents/               agent implementations, all Go (~146 .go files, no YAML): the
                      physician panel + care-services agents, each with LLM variants
                      (diagnostician_{llm,debate,reflexion,cascade,vision,tools}.go, etc.)
data/                 knowledge corpus, clinical case data
db/                   SQL migrations
docs/                 markdown documentation (+ docs/adr/ decision records)
zarf/                 Docker Compose + Kubernetes manifests
```

---

## Environment variables

Copy `.env.example` → `.env` (gitignored) and source it before running:

```bash
set -a; . ./.env; set +a
```

Key variables:

| Variable | Purpose |
|---|---|
| `ANTHROPIC_API_KEY` | Required for `-diagnostician=llm-anthropic` |
| `OPENAI_API_KEY` | Required for `-diagnostician=llm-openai` |
| `OLLAMA_HOST` | Ollama base URL (default `http://localhost:11434`) |
| `BODH_ANTHROPIC_MODEL` | Override Anthropic model (default `claude-sonnet-4-6`) |
| `BODH_OPENAI_MODEL` | Override OpenAI model (default `gpt-4o`) |
| `BODH_OLLAMA_MODEL` | Override Ollama model (default `llama3.2`) |
| `LMNR_PROJECT_API_KEY` | Laminar API key (cloud or self-hosted) |
| `LMNR_BASE_URL` | Laminar server URL (default `https://api.lmnr.ai`; self-hosted: `http://localhost:8000`) |
| `BODH_SERVICE_NAME` | OTel service.name (default `bodh`) |
| `DATABASE_URL` | PostgreSQL connection string |

**Security rules:**
- API keys go in `.env` only — never in tracked files or code
- PHI: `bodh.case_id` is always `sha256(caseID)[:8]` hex on spans — raw MRNs must never appear in collectors or logs

---

## Architecture

Bodh implements the **MARA pipeline**:

```
Intake Agent → Test Planner → Diagnostician → Care Plan Generator
                                    ↑
                           Rule | LLM-Anthropic | LLM-OpenAI | LLM-Ollama
```

Key architectural decisions:
- **`pkg/llm.Client`** is the single interface for all LLM calls; providers are selected at startup
- **`pkg/orchestration`** is an in-process message bus; agents communicate via typed messages
- **`pkg/audit`** writes an append-only audit log for every clinical action (HIPAA requirement)
- **`pkg/hitl`** captures human feedback for every AI decision
- **Build tags** (`otel`, `kronk`, `nats`) keep the default binary small; stubs satisfy all interfaces

#### Invariants (violating any of these gets a change reverted — see `AGENTS.md`)

1. **Never bypass the HITL gate for clinically consequential actions.** Diagnosis conclusions,
   treatment recommendations, discharge plans, prior-auth/refill approvals, and outreach to patients
   on 42 CFR Part 2 conditions must flow through `agents/human_review.go` → `pkg/hitl.Queue` →
   clinician decision. Without the gate, Bodh becomes a regulated medical device.
2. **Never widen the PHI surface of the audit log.** `pkg/audit` records IDs, kinds, timestamps,
   agent IDs, verdicts, and *redacted* reasons — never chief complaints, HPI, lab values, med
   names/doses, or free text. `cmd/care/explain.go` inherits this; its tests assert forbidden JSON
   keys are absent (`TestExplainHandler_PHIFreeFieldSelection`).
3. **Every LLM-backed agent has a rule-based fallback, and the fallback is the contract** (detailed
   below). Bench fixtures in `data/cases/` are graded on the rule path, so regressions show up in CI.
- **LLM agents fall back to rule-based twins.** Every `*_llm.go` agent wraps an `llm.Client` and
  degrades to its deterministic rule-based equivalent on *any* per-call failure (timeout, JSON parse
  error, transport error, confidence below `MinConfidence`). The case always finalizes — an LLM
  outage degrades quality, never breaks the pipeline. The LLM variant emits the *same*
  `DiagnosisProposal` JSON shape as the rule agent, so cost guardian / reasoning verifier / HITL are
  unchanged. Preserve this contract when touching any `agents/*_llm.go` file.

### Agent-decoupling substrate (Phase 1)

These four packages form the per-message safety boundary that must survive splitting the
monolith into per-agent processes. Understand them together before touching agent admission:

- **`pkg/governance`** — a composable policy stack (`medical_policy`, `break_glass_policy`,
  `part2_policy`, `rate_limit_policy`, `smart_scope_policy`, `opa_policy`, …) plus
  `agentmesh_gate.go`, which provides global + per-agent kill switches for operator demotion.
- **`pkg/gateway`** — `Pipeline.Admit` extracts Steps 1–2 of `orchestration.Orchestrator.Start`
  (governance evaluate/deny + audit record) into a **reusable admission-control pipeline**, so a
  decoupled agent binary enforces the *identical* boundary the monolith does. **Fail-closed on
  governance, fail-open on audit; never copy `msg.Content` (PHI) into audit events.** Start at
  `pkg/gateway/doc.go`.
- **`pkg/graduation`** — the agent-autonomy lifecycle ladder
  `development → shadow → copilot → autopilot`. Promotion is **evidence-gated, one rung at a time**,
  cleared only when the **Wilson-score lower bound** of the success rate beats a threshold over a
  minimum observation count (default 0.95 / ≥100 obs). The **autopilot fence**
  (`Criteria.AllowAutopilot`, default `false`) is checked *before* the statistical gate: promoting
  *into* autopilot requires an explicit authorized override, because unsupervised clinical
  decisioning is a regulated SaMD change, not a statistics threshold (ADR-0012). Currently a
  library with no production caller yet — start at `pkg/graduation/stage.go`.
- **`pkg/runtime`** — the per-agent message loop: reassembles the orchestrator's per-message
  closure (admit via `gateway.Pipeline` → `agent.HandleMessage` → audit handler errors →
  republish outputs) around ONE agent so it can run as its own binary, plus `/healthz` +
  `/readyz` probes matching `cmd/care`'s contract. **Known fidelity gap:** the monolith's
  eval-mirror (step 4 of `orchestration.Orchestrator.Start`) is NOT reproduced — decoupling
  `diagnostic_supervisor` before closing it breaks `GET /explain/{case_id}` case-status
  reporting. Start at `pkg/runtime/doc.go`, which documents the gap and both closure options.

### Architecture decision records

`docs/adr/` holds the design rationale. `0001`–`0005` are foundational (LLM abstraction, audit
chain-hash, build tags, MARA, PHI de-id). `0006`–`0012` are a 2026 principal-engineer review;
**read ADR-0006 first — the unresolved "research platform vs clinical product" identity question
gates how 0007–0012 resolve.** Key open threads: audit is fail-open at every seam and the
tamper-evident `ImmutableRecorder` is wired nowhere in `cmd/care` (0008); the decoupling plan's
24 per-agent NATS services may be premature distribution vs. a trust-tier split (0007).

---

## Testing conventions

- Tests live alongside source (`pkg/foo/foo_test.go`)
- Database-dependent tests use `pkg/dbtest.TestDB()` which spins up a Postgres container via testcontainers
- Build-tagged code has matching test stubs:
  - `otlp_test.go` (`//go:build otel`) + `otlp_stub.go` (`//go:build !otel`)
  - `kronk_real_test.go` (`//go:build kronk`) + `kronk_test.go` (`//go:build !kronk`)
- Test data fixtures live in `pkg/<name>/testdata/`
- Frontend: Vitest for unit tests, Playwright for e2e (`cmd/care/web/`)

Run just one package:

```bash
go test ./pkg/observability/...
go test -tags otel ./pkg/observability/...
```

---

## Adding a new lesson / command

Lessons `lesson-01` … `lesson-13` exist (see the Makefile). Follow the pattern established by
lesson-01 (`cmd/intro`), lesson-02 (`cmd/toolcalling`), and lesson-03 (`cmd/agentloop`):

1. Create `pkg/agent/<name>/` with `types.go`, `run.go`, and any tool files
2. Create `cmd/<name>/main.go` with `-provider`, `-model`, `-message`, `-verbose` flags
3. Add Makefile variables (`<NAME>_PROVIDER`, `<NAME>_MODEL`, `<NAME>_MESSAGE`) and a `lesson-NN:` target
4. Wire `AgentCallbacks` for observability hooks

---

## Self-hosted Laminar ports

| Port | Service |
|---|---|
| 5667 | Next.js web UI (not the API) |
| 8000 | REST API + OTLP/HTTP (use this for `LMNR_BASE_URL`) |
| 8001 | OTLP/gRPC |

A cloud API key does **not** authenticate against a self-hosted instance. Generate a local
project API key from `localhost:5667` → Settings → API Keys.

---

## Useful one-liners

```bash
# Run lesson-02 tool-calling with Ollama
make lesson-02

# Run with Anthropic
make lesson-02 TOOLCALLING_PROVIDER=anthropic

# Run the Laminar eval against local server
make laminar-eval LMNR_BASE_URL=http://localhost:8000

# Start local stack (Postgres + Laminar + Bodh)
make compose-up

# Apply DB migrations
make migrate

# Generate coverage report
make cover
```

<!-- SPECKIT START -->
For additional context about technologies to be used, project structure,
shell commands, and other important information, read the current plan.
The active branch `feat/agent-decoupling-phase0` tracks
specs/002-agent-decoupling/plan.md (per-agent binary / 3-tier split);
the earlier specs/001-remaining-plan-items/plan.md covers prior platform work.
<!-- SPECKIT END -->
