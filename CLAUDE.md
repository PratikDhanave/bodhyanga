# Bodh — CLAUDE.md

Bodh is a **HIPAA-grade medical AI orchestration platform** written in Go. It implements the
Multi-Agent Reference Architecture (MARA) for clinical decision support: intake → test-planning
→ diagnostics → care-plan generation, with full audit trails, human-in-the-loop feedback, and
LLM-agnostic provider abstraction.

---

## Quick reference

```bash
make test          # run all Go tests
make build         # compile all binaries
make care          # start HTTP API + Vue SPA on :8088
make demo          # sequential diagnosis demo (Ollama)
make evals         # multi-turn agent evaluation
make laminar-eval  # lmnr.evaluate() equivalent
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

Two optional tags gate heavyweight or proprietary features:

```bash
# OpenTelemetry + Laminar tracing
go build -tags otel ./cmd/care

# Kronk (proprietary) LLM / audio / rerank providers
go build -tags kronk ./cmd/care

# Both
go build -tags "otel kronk" ./cmd/care
```

Default (`go build ./...`) compiles without either tag. Every tagged file has a
`_stub.go` counterpart (e.g. `laminar_stub.go`, `kronk_stub.go`) so the project always
builds cleanly without those tags.

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
cmd/                  CLI binaries (one subdirectory per binary)
  bench/              SD-Bench regression runner
  bodhadmin/          admin CLI (migrations, config)
  bodhctl/            control-plane CLI
  care/               HTTP API + embedded Vue 3 SPA
  caredemo/           CDM/TCM/Panel demo
  demo/               sequential diagnosis walkthrough
  evals/              multi-turn evaluation runner
  export-hitl-feedback/
  laminar-eval/       Go equivalent of Python lmnr.evaluate()

pkg/                  library packages (36 total)
  agent/              agent orchestration core
  audio/              audio processing (+ kronk/ subpkg)
  audit/              HIPAA append-only audit log
  auth/               OPA Rego + JWT auth
  evals/
    laminar/          lmnr.evaluate() API client
    multiturn/        multi-turn benchmark harness
  eval/               single-turn evaluation
  hitl/               human-in-the-loop feedback
  hl7v2/              HL7 v2 ADT parser
  knowledge/          RAG corpus + retrieval (+ rerank/)
  llm/                LLM abstraction layer (+ kronk/, pool/)
  mcp/                Model Context Protocol client
  observability/      OTLP tracing, Laminar exporter
  orchestration/      clinical message bus
  persistence/        data layer (+ postgres/)
  phi/                PHI de-identification
  safety/             safety checks
  web/                HTTP handlers + routing
  ... (see pkg/ for the full list)

agents/               96 YAML/Go agent definitions
data/                 knowledge corpus, clinical case data
db/                   SQL migrations
docs/                 18 markdown documentation files
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
- **Build tags** (`otel`, `kronk`) keep the default binary small; stubs satisfy all interfaces

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

Follow the pattern established by lesson-01 (`cmd/intro`) and lesson-02 (`cmd/toolcalling`):

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
