# Bodhyanga Constitution

## Core Principles

### I. MARA Pipeline Architecture
Bodhyanga implements the Multi-Agent Reference Architecture (MARA) for clinical decision support.
Every clinical workflow flows through the pipeline: Intake Agent -> Test Planner -> Diagnostician -> Care Plan Generator.
Agents communicate via typed messages through `pkg/orchestration` (in-process message bus).
The Diagnostician slot is provider-pluggable: Rule | LLM-Anthropic | LLM-OpenAI | LLM-Ollama.

### II. LLM-Agnostic Provider Abstraction
`pkg/llm.Client` is the single interface for all LLM calls; providers are selected at startup.
New LLM providers must implement `llm.Client` and register via the provider pattern.
Unit tests MUST mock `llm.Client` -- never call real LLM APIs in tests.
Build tags (`otel`, `kronk`) gate optional features; every tagged file has a `_stub.go` counterpart.

### III. HIPAA Compliance (NON-NEGOTIABLE)
`pkg/audit` writes an append-only audit log for every clinical action.
`pkg/phi` de-identifies Protected Health Information before any external transmission.
`bodhyanga.case_id` is always `sha256(caseID)[:8]` hex on spans -- raw MRNs must NEVER appear in collectors, logs, or test assertions.
API keys go in `.env` only -- never in tracked files or code.
`pkg/hitl` captures human feedback for every AI decision (human-in-the-loop).

### IV. Test-First with Coverage Gates
Go tests live alongside source (`pkg/foo/foo_test.go`).
CI enforces >= 90% Go coverage and >= 80% frontend coverage.
Database-dependent tests use `pkg/dbtest.TestDB()` with testcontainers.
Frontend: Vitest + jsdom for unit tests, Playwright for E2E (`cmd/care/web/`).
Build-tagged code has matching test stubs (e.g., `otlp_test.go` + `otlp_stub.go`).
Mock data must be deterministic -- no flaky randomness.

### V. Package-First Design
Every feature starts as a `pkg/` library with clear boundaries.
CLI binaries in `cmd/` are thin wrappers that wire packages together.
Agent definitions live in `agents/` (96 YAML/Go files) with `_test.go` counterparts.
No circular dependencies between packages.

### VI. Observability by Default
OpenTelemetry tracing gated behind `//go:build otel` tag.
Prometheus metrics exposed at `/metrics`.
Structured JSON logging for all services.
Grafana dashboards and alert rules in `zarf/compose/`.

## Technology Stack

| Layer | Technology |
|-------|-----------|
| Backend | Go 1.26, module `github.com/PratikDhanave/bodhyanga` |
| Database | PostgreSQL (pgx/v5), tenant RLS |
| Frontend | React 18 + TypeScript, Vite, Tailwind CSS |
| Testing (Go) | stdlib `testing` + testcontainers-go |
| Testing (FE) | Vitest + @testing-library/react + Playwright |
| CI/CD | GitHub Actions (6 jobs: build-test, frontend, e2e, security, vuln, docker) |
| Infrastructure | Docker (distroless multi-stage), Kubernetes (Kustomize), Docker Compose |
| Monitoring | Prometheus + Grafana + OpenTelemetry |
| i18n | 4 locales (en, es, fr, zh) via `useI18n()` hook + I18nProvider |

## Development Workflow

### Build Commands
- `make test` -- run all Go tests
- `make build` -- compile all binaries
- `make care` -- start HTTP API + React SPA on :8088
- `make vet` -- go vet ./...
- `make lint` -- vet + staticcheck
- `make ci` -- vet + test + govulncheck
- Frontend: `cd cmd/care/web && npm ci && npx vitest run`

### Build Tags
- Default: `go build ./...` (no tags, always compiles cleanly)
- `otel`: OpenTelemetry + Laminar tracing
- `kronk`: Proprietary LLM / audio / rerank providers
- Both: `go build -tags "otel kronk" ./cmd/care`

### Repository Layout
- `cmd/` -- CLI binaries (one subdirectory per binary, 36 total)
- `pkg/` -- library packages (65 directories)
- `agents/` -- 96 YAML/Go agent definitions
- `data/` -- knowledge corpus, clinical case data
- `db/` -- SQL migrations
- `docs/` -- markdown documentation (18 files)
- `zarf/` -- Docker Compose + Kubernetes manifests
- `cmd/care/web/` -- React SPA (19 pages, 6 components)

### Adding New Features
1. Create `pkg/<name>/` with `types.go`, implementation, and `*_test.go`
2. If CLI needed: create `cmd/<name>/main.go` with standard flags
3. Add Makefile target with configurable variables
4. Wire `AgentCallbacks` for observability hooks
5. Add i18n keys to all 4 locale files if UI-facing

## Security Rules

- API keys in `.env` only -- NEVER in tracked files or code
- PHI de-identification: `sha256(caseID)[:8]` hex -- raw MRNs never in logs
- Audit trail: append-only, every clinical action logged
- Human-in-the-loop: every AI decision gets human feedback capture
- JWT + OPA Rego for authentication/authorization

## Governance

This constitution supersedes all other development practices for Bodhyanga.
Amendments require documentation update, code review, and migration plan.
All PRs must verify compliance with HIPAA constraints and testing gates.
Clinical safety features (decision confirmation, confidence display) are highest priority.

**Version**: 1.0.0 | **Ratified**: 2026-06-10 | **Last Amended**: 2026-06-10
